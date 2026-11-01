package hobalight

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (s *Store) buildReference(id string) (Reference, error) {
	var session Session
	if err := s.read(id, 'S', &session); err != nil {
		return Reference{}, err
	}
	return s.referenceFromSession(id, session)
}

func (s *Store) referenceFromSession(id string, session Session) (Reference, error) {
	if _, err := time.Parse(time.RFC3339Nano, session.Started); err != nil {
		return Reference{}, fmt.Errorf("invalid session start time: %w", err)
	}
	listing := make([]ReferenceFile, 0, len(session.Files))
	seen := make(map[string]bool)
	for _, fileID := range session.Files {
		var file File
		if err := s.read(fileID, 'F', &file); err != nil {
			return Reference{}, err
		}
		rel, err := relative(session.Directory, file.Path)
		if err != nil {
			return Reference{}, err
		}
		if file.Size < 0 || seen[rel] {
			return Reference{}, errors.New("invalid or duplicate file in session")
		}
		seen[rel] = true
		listing = append(listing, ReferenceFile{ID: fileID, Path: rel, Size: file.Size, Failed: file.Error != ""})
	}
	return Reference{Format: referenceFormat, Directory: session.Directory, Started: session.Started, Session: id, Files: &listing}, nil
}

// Refresh regenerates derived references only; immutable objects are never written.
func (s *Store) refresh(output io.Writer) error {
	release, err := s.beginWrite("refresh")
	if err != nil {
		return err
	}
	defer release()

	directory := "sessions"
	if err := s.backend.EnsureNamespace(directory, false); err != nil {
		return err
	}
	entries, err := storageEntries(s.backend, directory)
	if err != nil {
		return err
	}
	type existingReference struct {
		name string
		data []byte
		ref  Reference
	}
	bySession := make(map[string][]existingReference)
	bySuffix := make(map[string][]existingReference)
	for _, entry := range entries {
		if !isSessionReference(entry) {
			continue
		}
		data, err := s.backend.Read(path.Join(directory, entry.Name()))
		if err != nil {
			return err
		}
		var ref Reference
		valid := yaml.Unmarshal(data, &ref) == nil && validID(ref.Session, 'S')
		candidate := existingReference{name: entry.Name(), data: data, ref: ref}
		if valid {
			bySession[ref.Session] = append(bySession[ref.Session], candidate)
		} else {
			stem := strings.TrimSuffix(entry.Name(), ".yaml")
			if at := strings.LastIndex(stem, "-"); at >= 0 {
				bySuffix[stem[at+1:]] = append(bySuffix[stem[at+1:]], candidate)
			}
		}
	}
	refreshed, unchanged, failed := 0, 0, 0
	var failures []error
	recordError := func(id string, err error) {
		failed++
		failures = append(failures, fmt.Errorf("session %s: %w", id, err))
		fmt.Fprintf(output, "Failed %s: %s\n", id, browserText(err.Error()))
	}
	err = s.walkObjects(func(id string, err error) error {
		if err != nil {
			recordError(id, err)
			return nil
		}
		if !validID(id, 'S') {
			return nil
		}
		ref, err := s.buildReference(id)
		if err != nil {
			recordError(id, err)
			return nil
		}
		data, err := encode(ref)
		if err != nil {
			recordError(id, err)
			return nil
		}
		targets := append([]existingReference(nil), bySession[id]...)
		targets = append(targets, bySuffix[id[:8]]...)
		if len(targets) == 0 {
			started, _ := time.Parse(time.RFC3339Nano, ref.Started)
			targets = append(targets, existingReference{name: sessionReferenceName(started, id)})
		}
		for _, target := range targets {
			if target.ref.Format > referenceFormat {
				recordError(id, fmt.Errorf("unsupported reference format %d in %s", target.ref.Format, target.name))
				return nil
			}
		}
		changed := false
		for _, target := range targets {
			if bytes.Equal(target.data, data) {
				continue
			}
			destination := path.Join(directory, target.name)
			// Never replace an unrelated file, including a symlink or a short-hash collision.
			if target.data == nil {
				if _, err := s.backend.Inspect(destination); !errors.Is(err, fs.ErrNotExist) {
					recordError(id, fmt.Errorf("reference destination already exists: %s", target.name))
					return nil
				}
			}
			if err := writeStorageBytes(s.backend, destination, data, false); err != nil {
				recordError(id, err)
				return nil
			}
			changed = true
		}
		if changed {
			refreshed++
			fmt.Fprintf(output, "Refreshed session %s  %s\n", id[:8], browserText(ref.Directory))
		} else {
			unchanged++
		}
		return nil
	})
	if err != nil {
		failures = append(failures, err)
	}
	s.sessionListings = nil
	fmt.Fprintf(output, "Sessions: %d refreshed, %d unchanged, %d failed\n", refreshed, unchanged, failed)
	return errors.Join(failures...)
}
