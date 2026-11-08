package hobalight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupDoesNotReuseNonRegularBlob(t *testing.T) {
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeFile(t, filepath.Join(source, "file"), []byte("source content"))
			s := newStore(t, filepath.Join(root, "store"))
			id, err := s.backup(source, false)
			must(t, err)
			_, files, err := s.sessionFiles(id)
			must(t, err)
			blob := s.object(files[0].metadata.Blob)
			original := readFile(t, blob)
			must(t, os.Remove(blob))
			if kind == "directory" {
				writeFile(t, filepath.Join(blob, "keep"), []byte("do not remove"))
			} else {
				outside := filepath.Join(root, "outside.zip")
				writeFile(t, outside, original)
				if err := os.Symlink(outside, blob); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			next, err := s.backup(source, false)
			if err == nil {
				t.Fatal("backup reported success with a non-regular blob")
			}
			var session Session
			must(t, s.read(next, 'S', &session))
			var f File
			must(t, s.read(session.Files[0], 'F', &f))
			if f.Error == "" || f.Blob != "" {
				t.Fatalf("failure was not recorded: %+v", f)
			}
			if kind == "directory" && string(readFile(t, filepath.Join(blob, "keep"))) != "do not remove" {
				t.Fatal("obstruction was changed")
			}
		})
	}
}

func TestBackupRejectsSourceChangedAfterScanning(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "source")
	writeFile(t, name, []byte("before"))
	info, err := os.Stat(name)
	must(t, err)
	writeFile(t, name, []byte("longer replacement after scanning"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backupFile(name, info, false)
	if err == nil {
		t.Fatal("saved changed contents with the old file size")
	}
	var f File
	must(t, s.read(id, 'F', &f))
	if !strings.Contains(f.Error, "changed") || f.Blob != "" {
		t.Fatalf("wrong failure: %+v", f)
	}
}

type sourceChangingStage struct {
	*filesystemStorage
	change func()
}

func (s *sourceChangingStage) NewStage(pattern string) (*os.File, error) {
	f, err := s.filesystemStorage.NewStage(pattern)
	if err == nil {
		s.change()
	}
	return f, err
}

func TestBackupRejectsSourceChangedDuringReading(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "source")
	writeFile(t, name, []byte("before"))
	info, err := os.Stat(name)
	must(t, err)
	s := newStore(t, filepath.Join(root, "store"))
	s.backend = &sourceChangingStage{filesystemStorage: s.backend.(*filesystemStorage), change: func() {
		writeFile(t, name, []byte("longer contents after opening"))
	}}
	id, err := s.backupFile(name, info, false)
	if err == nil {
		t.Fatal("saved contents changed after opening with old metadata")
	}
	var f File
	must(t, s.read(id, 'F', &f))
	if !strings.Contains(f.Error, "changed while reading") || f.Blob != "" {
		t.Fatalf("wrong failure: %+v", f)
	}
}
