package hobalight

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestoreRejectsMetadataSizeMismatch(t *testing.T) {
	for _, size := range []int64{-1, 3, 30} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/existing=%v", size, existing), func(t *testing.T) {
				root := t.TempDir()
				source := filepath.Join(root, "source")
				writeFile(t, filepath.Join(source, "file.txt"), []byte("original content"))
				s := newStore(t, filepath.Join(root, "store"))
				id, err := s.backup(source, false)
				must(t, err)
				_, files, err := s.sessionFiles(id)
				must(t, err)
				f := files[0].metadata
				f.Size = size
				identity, err := encode(f.Identity)
				must(t, err)
				fileID := hash(identity) + "F"
				data, err := encode(f)
				must(t, err)
				must(t, s.put(fileID, data))
				destination := filepath.Join(root, "restored")
				target := filepath.Join(destination, "file.txt")
				if existing {
					writeFile(t, target, []byte("original content"))
				}
				if _, err := s.restoreResolved(fileID, "", destination, io.Discard); err == nil {
					t.Fatal("restore accepted metadata size that differs from content")
				}
				if existing {
					if string(readFile(t, target)) != "original content" {
						t.Fatal("existing file was changed")
					}
				} else if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatalf("failed restore left a file: %v", err)
				}
			})
		}
	}
}

func TestRestoreResetsModificationTimeOfIdenticalFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file.txt"), []byte("original content"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(id)
	must(t, err)
	destination := filepath.Join(root, "restored")
	_, err = s.restoreResolved(id, "", destination, io.Discard)
	must(t, err)
	target := filepath.Join(destination, "file.txt")
	want, err := time.Parse(time.RFC3339Nano, files[0].metadata.Modified)
	must(t, err)
	changed := want.Add(time.Hour)
	must(t, os.Chtimes(target, changed, changed))
	_, err = s.restoreResolved(id, "", destination, io.Discard)
	must(t, err)
	info, err := os.Stat(target)
	must(t, err)
	if !info.ModTime().Equal(want) {
		t.Fatalf("got %s, want %s", info.ModTime(), want)
	}
}
