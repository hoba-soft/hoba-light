package hobalight

import (
	"context"
	"io"
	"path/filepath"
	"testing"
)

func TestFileAndDirectoryBackupsHaveSeparateBaselines(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	file := filepath.Join(source, "one")
	other := filepath.Join(source, "two")
	writeFile(t, file, []byte("one"))
	writeFile(t, other, []byte("two"))
	s := newStore(t, filepath.Join(root, "store"))
	var summary backupSummary
	first, err := s.backupWithSummary(file, false, &summary)
	must(t, err)
	second, err := s.backupWithSummary(other, false, &summary)
	must(t, err)
	if summary.baseline != "" {
		t.Fatal("another file used as baseline")
	}
	directory, err := s.backupWithSummary(source, false, &summary)
	must(t, err)
	if summary.baseline != "" {
		t.Fatal("file backup used as directory baseline")
	}
	_, err = s.backupWithSummary(file, false, &summary)
	must(t, err)
	if summary.baseline != first {
		t.Fatalf("got baseline %s, want %s", summary.baseline, first)
	}
	_, err = s.backupWithSummary(other, false, &summary)
	must(t, err)
	if summary.baseline != second {
		t.Fatal("wrong second-file baseline")
	}
	_, err = s.backupWithSummary(source, false, &summary)
	must(t, err)
	if summary.baseline != directory {
		t.Fatal("wrong directory baseline")
	}
	header, err := s.scanDiffSession(context.Background(), first, func(string) error { return nil })
	must(t, err)
	if header.Source != portable(file) {
		t.Fatal("streamed metadata lost file source")
	}
	must(t, s.verify(nil, io.Discard))
}

func TestSingleFileBackupBrowserOpen(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "source", "photo.txt")
	writeFile(t, file, []byte("saved photo"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(file, false)
	must(t, err)
	_, files, err := s.listingFiles(id)
	must(t, err)
	opened := ""
	u := browserUI{
		browser: browser{store: s, open: func(name string) error { opened = name; return nil }},
		ref:     &Reference{Session: id}, entries: browserEntries(files, ""),
	}
	dest := filepath.Join(root, "restore")
	u.restoreSelectionTo(dest, true)
	target := filepath.Join(dest, "photo.txt")
	if canonical(opened) != canonical(target) {
		t.Fatalf("%s: opened %q, want %q", u.message, opened, target)
	}
	if string(readFile(t, target)) != "saved photo" {
		t.Fatal("wrong restored contents")
	}
}
