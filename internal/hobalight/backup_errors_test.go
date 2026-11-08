package hobalight

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failingBackupStorage struct {
	storageBackend
	fail string
}

func (b failingBackupStorage) Write(key string, r io.Reader, safe bool) error {
	if (b.fail == "reference" && strings.HasPrefix(key, "sessions/")) ||
		(b.fail != "reference" && strings.HasSuffix(key, b.fail)) {
		return errors.New("injected storage write failure")
	}
	return b.storageBackend.Write(key, r, safe)
}

func TestBackupSourceErrorsAreFullyRecorded(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "some", true: "all"}[all], func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			missing := filepath.Join(source, "unreadable")
			writeFile(t, missing, []byte("not saved"))
			if !all {
				writeFile(t, filepath.Join(source, "good"), []byte("saved"))
			}
			s := newStore(t, filepath.Join(root, "store"))
			removed := false
			s.progress = func(p progressUpdate) {
				if p.total >= 0 && !removed {
					removed = true
					must(t, os.Remove(missing))
				}
			}
			id, err := s.backup(source, false)
			if ExitCode(err) != 2 {
				t.Fatalf("expected completed with errors: %v", err)
			}
			var session Session
			must(t, s.read(id, 'S', &session))
			if session.Status != sessionCompletedWithErrors || len(session.Errors) != 1 {
				t.Fatalf("%+v", session)
			}
			failures := 0
			for _, fid := range session.Files {
				var f File
				must(t, s.read(fid, 'F', &f))
				if f.Error != "" {
					failures++
					if f.Path != portable(missing) || f.Blob != "" {
						t.Fatalf("%+v", f)
					}
				}
			}
			if failures != 1 {
				t.Fatal("source failure missing from session")
			}
		})
	}
}

func TestBackupStorageFailuresNeverComplete(t *testing.T) {
	for _, stage := range []string{"B", "F", "S", "reference"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeFile(t, filepath.Join(source, "file"), []byte("contents"))
			s := newStore(t, filepath.Join(root, "store"))
			s.backend = failingBackupStorage{s.backend, stage}
			id, err := s.backup(source, false)
			if ExitCode(err) != 1 {
				t.Fatalf("storage error classified as %d: %v", ExitCode(err), err)
			}
			if id != "" {
				var session Session
				must(t, s.read(id, 'S', &session))
				if session.Status != sessionFailed {
					t.Fatalf("%+v", session)
				}
			}
			must(t, s.walkObjects(func(id string, err error) error {
				if err != nil {
					return err
				}
				if !validID(id, 'S') {
					return nil
				}
				var session Session
				if err := s.read(id, 'S', &session); err != nil {
					return err
				}
				if session.Status != sessionFailed {
					t.Fatalf("storage failure left completed session: %+v", session)
				}
				return nil
			}))
		})
	}
}

func TestFailureMetadataWriteFailureIsFatal(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	file := filepath.Join(source, "file")
	writeFile(t, file, []byte("contents"))
	s := newStore(t, filepath.Join(root, "store"))
	s.backend = failingBackupStorage{s.backend, "F"}
	removed := false
	s.progress = func(p progressUpdate) {
		if p.total >= 0 && !removed {
			removed = true
			must(t, os.Remove(file))
		}
	}
	id, err := s.backup(source, false)
	if ExitCode(err) != 1 {
		t.Fatalf("%v", err)
	}
	var session Session
	must(t, s.read(id, 'S', &session))
	if session.Status != sessionFailed || len(session.Errors) == 0 {
		t.Fatalf("%+v", session)
	}
}

type closedBackupStage struct{ *filesystemStorage }

func (b closedBackupStage) NewStage(pattern string) (*os.File, error) {
	f, err := b.filesystemStorage.NewStage(pattern)
	if err == nil {
		err = f.Close()
	}
	return f, err
}
func TestBackupStageWriteFailureIsFatal(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("contents"))
	s := newStore(t, filepath.Join(root, "store"))
	s.backend = closedBackupStage{s.backend.(*filesystemStorage)}
	id, err := s.backup(source, false)
	if ExitCode(err) != 1 {
		t.Fatalf("%v", err)
	}
	var session Session
	must(t, s.read(id, 'S', &session))
	if session.Status != sessionFailed {
		t.Fatalf("%+v", session)
	}
}

func TestUnreadableDirectoryIsReportedInSession(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	denied := filepath.Join(source, "denied")
	writeFile(t, filepath.Join(denied, "unknown-file"), []byte("secret"))
	must(t, os.Chmod(denied, 0))
	t.Cleanup(func() { os.Chmod(denied, 0700) })
	if _, err := os.ReadDir(denied); err == nil {
		t.Skip("account can read the directory despite mode bits")
	}
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	if ExitCode(err) != 2 {
		t.Fatalf("%v", err)
	}
	var session Session
	must(t, s.read(id, 'S', &session))
	if session.Status != sessionCompletedWithErrors || len(session.Errors) != 1 || !strings.Contains(session.Errors[0], portable(denied)) {
		t.Fatalf("%+v", session)
	}
}

func TestFailedSessionCannotVerifyAsSuccessful(t *testing.T) {
	s := newStore(t, t.TempDir())
	session := Session{Directory: portable(t.TempDir()), Started: timestamp(time.Now()), Status: sessionFailed, Files: []string{}}
	data, err := encode(session)
	must(t, err)
	id := hash(data) + "S"
	must(t, s.put(id, data))
	if err := s.verify([]string{id}, io.Discard); err == nil {
		t.Fatal("failed session passed verification")
	}
}
