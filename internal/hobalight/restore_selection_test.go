package hobalight

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type restoreReadCounter struct {
	storageBackend
	metadataReads int
}

func (s *restoreReadCounter) Read(key string) ([]byte, error) {
	if strings.HasPrefix(key, "objects/") && strings.HasSuffix(key, "F") {
		s.metadataReads++
	}
	return s.storageBackend.Read(key)
}

func TestBrowserSingleFileRestoreDoesNotScanOtherMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for i := 0; i < 130; i++ {
		writeFile(t, filepath.Join(source, "other", fmt.Sprintf("%03d.txt", i)), []byte("other"))
	}
	selected := "nested/photo данные.jpg"
	writeFile(t, filepath.Join(source, filepath.FromSlash(selected)), []byte("selected content"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.listingFiles(id)
	must(t, err)
	counter := &restoreReadCounter{storageBackend: s.backend}
	s.backend = counter
	for _, open := range []bool{false, true} {
		counter.metadataReads = 0
		opened := ""
		u := browserUI{
			browser: browser{store: s, open: func(name string) error { opened = name; return nil }},
			ref:     &Reference{Session: id}, current: "nested", entries: browserEntries(files, "nested"),
		}
		destination := filepath.Join(root, fmt.Sprintf("restore-%v", open))
		u.restoreSelectionTo(destination, open)
		target := filepath.Join(destination, filepath.FromSlash(selected))
		if !strings.HasPrefix(u.message, "Restored 1 file") && !strings.HasPrefix(u.message, "Opened ") {
			t.Fatal(u.message)
		}
		if string(readFile(t, target)) != "selected content" {
			t.Fatal("wrong restored content")
		}
		if counter.metadataReads != 1 {
			t.Fatalf("read %d file objects for one selection", counter.metadataReads)
		}
		if open && canonical(opened) != canonical(target) {
			t.Fatalf("opened %q, want %q", opened, target)
		}
	}
}

func TestRestoreSelectionRejectsWrongFileOrPath(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file.txt"), []byte("selected"))
	id, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(id)
	must(t, err)
	other := filepath.Join(root, "other")
	writeFile(t, filepath.Join(other, "file.txt"), []byte("unrelated"))
	otherID, err := s.backup(other, false)
	must(t, err)
	_, otherFiles, err := s.sessionFiles(otherID)
	must(t, err)
	for _, tc := range []struct{ path, fileID string }{
		{"file.txt", otherFiles[0].id},
		{"wrong.txt", files[0].id},
	} {
		destination := filepath.Join(root, "must-not-be-created")
		if _, err := s.restoreResolvedSelection(id, tc.path, destination, tc.fileID, io.Discard); err == nil {
			t.Fatal("accepted an invalid selection")
		}
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			t.Fatalf("invalid selection touched destination: %v", err)
		}
	}
}

func TestRestorePreparationReportsCheckedMetadata(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	for i := 0; i < 65; i++ {
		writeFile(t, filepath.Join(source, fmt.Sprintf("%03d.txt", i)), []byte("content"))
	}
	id, err := s.backup(source, false)
	must(t, err)
	checked := 0
	s.progress = func(p progressUpdate) {
		if p.operation == "Restore" && p.total < 0 {
			if strings.Contains(p.text(), "found") {
				t.Fatal(p.text())
			}
			checked = max(checked, p.done)
		}
	}
	_, err = s.restoreItems(id, "", "")
	must(t, err)
	if checked != 64 {
		t.Fatalf("preparation did not update: %d", checked)
	}
}
