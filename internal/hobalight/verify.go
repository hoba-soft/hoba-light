package hobalight

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

type verifiedBlob struct {
	size int64
	err  error
}
type verifier struct {
	recovery                                                              map[string]recoveryCandidate
	searched                                                              map[string]bool
	store                                                                 *Store
	out                                                                   io.Writer
	writeErr                                                              error
	checked                                                               map[string]bool
	blobs                                                                 map[string]verifiedBlob
	sessions, files, blobCount, damaged, incomplete, issues, backupErrors int
}

func (v *verifier) print(format string, args ...any) {
	if v.writeErr == nil {
		_, v.writeErr = fmt.Fprintf(v.out, format, args...)
	}
}
func (v *verifier) issue(context string, err error) {
	v.issues++
	v.print("Error %s: %s\n", browserText(context), browserText(err.Error()))
}
func (v *verifier) mark(id string) {
	if v.checked[id] {
		return
	}
	v.checked[id] = true
	switch id[64] {
	case 'S':
		v.sessions++
	case 'F':
		v.files++
	case 'B':
		v.blobCount++
	}
	v.store.reportErrors("Verify", len(v.checked), -1, id, v.issues)
}
func (v *verifier) regular(id string) error {
	if !validObjectID(id) {
		return fmt.Errorf("invalid object reference %q", id)
	}
	// Scoped verification must reject symlinks too, without relying on inventory.
	bucket := path.Dir(objectKey(id))
	info, err := v.store.backend.Inspect(bucket)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("object bucket is not a real directory")
	}
	info, err = v.store.backend.Inspect(objectKey(id))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("object is not a regular file")
	}
	return nil
}
func (v *verifier) blob(id string) verifiedBlob {
	if result, ok := v.blobs[id]; ok {
		return result
	}
	result := verifiedBlob{}
	if validID(id, 'B') {
		v.mark(id)
	}
	result.err = v.regular(id)
	if result.err == nil {
		r, err := v.store.diffBlob(context.Background(), File{Blob: id})
		result.err = err
		if err == nil {
			result.size, result.err = io.Copy(io.Discard, r)
			result.err = errors.Join(result.err, r.Close())
		}
	}
	if result.err != nil {
		v.findTrash(id, 'B', nil)
	}
	v.blobs[id] = result
	return result
}
func (v *verifier) file(id, scope string) (File, bool) {
	var f File
	if !validID(id, 'F') {
		v.issue(scope, fmt.Errorf("invalid file reference %q", id))
		return f, false
	}
	v.mark(id)
	label := scope + " file " + id
	if err := v.metadata(id, 'F', &f); err != nil {
		v.issue(label, err)
		return f, false
	}
	if f.Size < 0 {
		v.issue(label, errors.New("negative file size"))
	}
	if _, err := historyPath(f.Path); err != nil {
		v.issue(label, fmt.Errorf("invalid file path: %w", err))
	}
	if f.Modified != "" || f.Error == "" {
		if _, err := time.Parse(time.RFC3339Nano, f.Modified); err != nil {
			v.issue(label, fmt.Errorf("invalid modification time: %w", err))
		}
	}
	if f.Created != nil {
		if _, err := time.Parse(time.RFC3339Nano, *f.Created); err != nil {
			v.issue(label, fmt.Errorf("invalid creation time: %w", err))
		}
	}
	if f.Error != "" {
		v.backupErrors++
		v.print("Backup error %s %s: %s\n", browserText(label), browserText(f.Path), browserText(f.Error))
		return f, true
	}
	b := v.blob(f.Blob)
	if b.err != nil {
		v.issue(label+" blob "+f.Blob, b.err)
		v.findLocal(f)
	} else if b.size != f.Size {
		v.issue(label, fmt.Errorf("size mismatch: metadata %d bytes, blob %d bytes", f.Size, b.size))
	}
	return f, true
}
func (v *verifier) session(id string) {
	v.mark(id)
	startIssues, startBackup := v.issues, v.backupErrors
	var session Session
	err := v.metadata(id, 'S', &session)
	if err != nil {
		v.issue("session "+id, err)
	} else {
		if _, err = time.Parse(time.RFC3339Nano, session.Started); err != nil {
			v.issue("session "+id, fmt.Errorf("invalid start time: %w", err))
		}
		if _, err = historyPath(session.Directory); err != nil {
			v.issue("session "+id, fmt.Errorf("invalid source directory: %w", err))
		}
		for _, message := range session.Errors {
			v.backupErrors++
			v.print("Backup error session %s: %s\n", id, browserText(message))
		}
		seen := make(map[string]bool)
		for _, fid := range session.Files {
			if v.writeErr != nil {
				return
			}
			f, readable := v.file(fid, "session "+id)
			if !readable {
				continue
			}
			rel, e := relative(session.Directory, f.Path)
			if e != nil {
				v.issue("session "+id+" file "+fid, e)
				continue
			}
			if seen[rel] {
				v.issue("session "+id, fmt.Errorf("duplicate path %q", rel))
			}
			seen[rel] = true
		}
		for rel := range seen {
			for parent := path.Dir(rel); parent != "."; parent = path.Dir(parent) {
				if seen[parent] {
					v.issue("session "+id, fmt.Errorf("file/directory path conflict: %q and %q", parent, rel))
				}
			}
		}
	}
	status := "OK"
	if v.issues > startIssues {
		v.damaged++
		status = "DAMAGED"
	} else if session.completionStatus() == sessionFailed {
		if v.backupErrors == startBackup {
			v.backupErrors++
			v.print("Backup error session %s: session failed\n", id)
		}
		v.incomplete++
		status = "FAILED"
	} else if v.backupErrors > startBackup {
		v.incomplete++
		status = "INCOMPLETE"
	}
	v.print("Session %.8s %s  %s\n", id, status, browserText(session.Directory))
}

// Objects are authoritative; references provide hints for missing session roots.
// The maintenance lock prevents backup/deletion/GC from changing the graph.
func (s *Store) verify(args []string, out io.Writer) error {
	repair := false
	var selected []string
	for _, a := range args {
		if a == "--repair" {
			repair = true
		} else if strings.HasPrefix(a, "-") {
			return errors.New("usage: verify [--repair] [session-hash]")
		} else {
			selected = append(selected, a)
		}
	}
	if len(selected) > 1 {
		return errors.New("usage: verify [--repair] [session-hash]")
	}
	release, err := s.beginMaintenance("verify")
	if err != nil {
		return err
	}
	defer release()
	v := newVerifier(s, out)
	err = v.run(selected)
	if !repair || v.writeErr != nil || len(v.recovery) == 0 {
		return err
	}
	var repairErrors []error
	for id, candidate := range v.recovery {
		if e := s.repairObject(id, candidate); e != nil {
			v.print("Repair failed %s: %s\n", id, browserText(e.Error()))
			repairErrors = append(repairErrors, e)
		} else {
			v.print("Repaired %s from %s\n", id, browserText(candidate.source))
		}
		if v.writeErr != nil {
			return v.writeErr
		}
	}
	for id := range v.recovery {
		if validID(id, 'S') {
			if e := s.repairReference(id); e != nil {
				repairErrors = append(repairErrors, e)
				v.print("Reference repair failed %s: %s\n", id, browserText(e.Error()))
			}
		}
	}
	v.print("Rechecking repaired storage\n")
	return errors.Join(append(repairErrors, newVerifier(s, out).run(selected))...)
}
func newVerifier(s *Store, out io.Writer) *verifier {
	return &verifier{store: s, out: out, checked: map[string]bool{}, blobs: map[string]verifiedBlob{}, recovery: map[string]recoveryCandidate{}, searched: map[string]bool{}}
}
func (v *verifier) run(args []string) error {
	s := v.store
	var err error
	if len(args) == 1 {
		id, err := v.resolveSession(args[0])
		if err != nil {
			return err
		}
		v.session(id)
	} else {
		err = s.walkObjects(func(id string, walkErr error) error {
			if v.writeErr != nil {
				return v.writeErr
			}
			if walkErr != nil {
				v.issue(id, walkErr)
				return nil
			}
			// Sessions are always visited so shared failures identify every affected root.
			if validID(id, 'S') {
				v.session(id)
			} else if !v.checked[id] {
				if validID(id, 'F') {
					v.file(id, "object")
				} else {
					b := v.blob(id)
					if b.err != nil {
						v.issue("blob "+id, b.err)
					}
				}
			}
			return v.writeErr
		})
		if err != nil {
			v.issue("inventory", err)
		}
		v.missingRoots()
	}
	s.reportErrors("Verify", len(v.checked), len(v.checked), "", v.issues)
	v.print("Verified: %d sessions, %d file objects, %d blobs\n", v.sessions, v.files, v.blobCount)
	v.print("Sessions: %d OK, %d damaged, %d incomplete\n", v.sessions-v.damaged-v.incomplete, v.damaged, v.incomplete)
	v.print("Result: %d integrity issues, %d backup errors\n", v.issues, v.backupErrors)
	v.print("Recoverable: %d objects (use --repair to restore)\n", len(v.recovery))
	if v.writeErr != nil {
		return v.writeErr
	}
	if v.issues > 0 || v.backupErrors > 0 {
		return fmt.Errorf("verification failed: %d integrity issues, %d backup errors", v.issues, v.backupErrors)
	}
	return nil
}
