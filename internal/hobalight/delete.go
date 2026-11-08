package hobalight

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Delete only the session root and its derived references. GC handles file and
// blob objects later, retaining anything shared with another session.
func (s *Store) deleteSession(prefix string, output io.Writer) error {
	release, err := s.beginMaintenance("delete")
	if err != nil {
		return err
	}
	defer release()
	return s.deleteSessionLocked(prefix, output)
}

func (s *Store) deleteSessionLocked(prefix string, output io.Writer) error {
	id, err := s.resolve(prefix, 'S')
	if err != nil {
		return err
	}
	info, err := s.backend.Inspect(objectKey(id))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("session object is not a regular file")
	}
	references, err := s.referencesForDeletion(id)
	if err != nil {
		return err
	}
	if err := s.trashObject(id, "deleted"); err != nil {
		return err
	}
	s.sessionListings = nil
	var failures []error
	for _, name := range references {
		if err := s.backend.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, fmt.Errorf("remove reference %s: %w", name, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("session %s deleted, but reference cleanup failed: %w", id[:8], errors.Join(failures...))
	}
	fmt.Fprintf(output, "Deleted session %s\n", id[:8])
	return nil
}

func (s *Store) referencesForDeletion(id string) ([]string, error) {
	directory := "sessions"
	entries, err := storageEntries(s.backend, directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !isSessionReference(entry) {
			continue
		}
		name := path.Join(directory, entry.Name())
		data, err := s.backend.Read(name)
		if err != nil {
			return nil, err
		}
		// Only the session ID matters; newer reference fields may have other types.
		var ref struct {
			Session string `yaml:"session"`
		}
		err = yaml.Unmarshal(data, &ref)
		if (err == nil && ref.Session == id) || ((!validID(ref.Session, 'S') || err != nil) && strings.HasSuffix(entry.Name(), "-"+id[:8]+".yaml")) {
			names = append(names, name)
		}
	}
	return names, nil
}

// Replace a session root under the maintenance lock; shared file/blob objects
// remain untouched. Publish the replacement before retiring the original root.
func (s *Store) deletePath(prefix, selected string, output io.Writer) (string, error) {
	release, err := s.beginMaintenance("delete")
	if err != nil {
		return "", err
	}
	defer release()
	return s.deletePathLocked(prefix, selected, "", output)
}

func (s *Store) deletePathLocked(prefix, selected, fileID string, output io.Writer) (string, error) {
	selected, err := userRelative(selected)
	if err != nil {
		return "", err
	}
	id, err := s.resolve(prefix, 'S')
	if err != nil {
		return "", err
	}
	session, files, err := s.sessionFiles(id)
	if err != nil {
		return "", err
	}
	references, err := s.referencesForDeletion(id)
	if err != nil {
		return "", err
	}
	kept := make([]string, 0, len(files))
	removed := 0
	for _, file := range files {
		if (fileID != "" && file.id == fileID) || (fileID == "" && (selected == "" || file.relative == selected || strings.HasPrefix(file.relative, selected+"/"))) {
			removed++
		} else {
			kept = append(kept, file.id)
		}
	}
	if removed == 0 {
		return "", errors.New("path not found in session")
	}
	session.Files = kept
	// Remove diagnostics belonging to the removed path; retain unrelated scan errors.
	absolute := strings.TrimRight(session.Directory, "/") + "/" + selected
	var retainedErrors []string
	for _, message := range session.Errors {
		if selected != "" && !strings.HasPrefix(message, absolute+":") && !strings.HasPrefix(message, absolute+"/") {
			retainedErrors = append(retainedErrors, message)
		}
	}
	session.Errors = retainedErrors
	if session.Status != sessionFailed {
		session.Status = sessionCompleted
		if len(session.Errors) > 0 {
			session.Status = sessionCompletedWithErrors
		}
	}
	data, err := encode(session)
	if err != nil {
		return "", err
	}
	next := hash(data) + "S"
	ref, err := s.referenceFromSession(next, session)
	if err != nil {
		return "", err
	}
	refData, err := encode(ref)
	if err != nil {
		return "", err
	}
	if err := s.put(next, data); err != nil {
		return "", err
	}
	started, _ := time.Parse(time.RFC3339Nano, session.Started)
	name := path.Join("sessions", sessionReferenceName(started, next))
	if err := s.backend.EnsureNamespace(path.Dir(name), false); err != nil {
		return next, err
	}
	if err := writeStorageBytes(s.backend, name, refData, false); err != nil {
		return next, fmt.Errorf("replacement %s saved, reference publication failed: %w", next[:8], err)
	}
	s.sessionListings = nil
	if err := s.trashObject(id, "replaced"); err != nil {
		return next, fmt.Errorf("replacement %s published; original %s could not be retired: %w", next[:8], id[:8], err)
	}
	var failures []error
	for _, old := range references {
		if old == name {
			continue
		}
		if err := s.backend.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		return next, fmt.Errorf("session replaced by %s; reference cleanup failed: %w", next[:8], errors.Join(failures...))
	}
	fmt.Fprintf(output, "Updated session %s -> %s: removed %s\n", id[:8], next[:8], displayCount(removed))
	return next, nil
}
