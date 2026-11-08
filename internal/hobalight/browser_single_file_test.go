package hobalight

import (
	"path/filepath"
	"testing"
)

func TestBrowserSingleFileUsesNormalListAndBackFlow(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	filename := filepath.Join(root, "source", "one.txt")
	writeFile(t, filename, []byte("one\ntwo\nthree\n"))
	id, err := s.backup(filename, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: refs, sources: true}
	u.enter()
	u.enter()
	if u.ref == nil || u.ref.Session != id || u.detail != nil || len(u.entries) != 1 || u.entries[0].name != "one.txt" {
		t.Fatal("single-file backup did not open its one-file list")
	}
	u.enter()
	if u.detail == nil {
		t.Fatal("Enter on file did not open metadata")
	}
	u.back()
	if u.detail != nil || u.ref == nil || len(u.entries) != 1 {
		t.Fatal("metadata Back skipped file list")
	}
	u.back()
	if u.ref != nil || u.sources {
		t.Fatal("file-list Back did not return to sessions")
	}
	_, files, err := s.sessionFiles(id)
	must(t, err)
	u.locateMatch(searchMatch{ref: refs[0], file: files[0]})
	if u.detail != nil || len(u.entries) != 1 {
		t.Fatal("locate skipped single-file list")
	}
	u.enter()
	u.back()
	if u.ref == nil || u.detail != nil {
		t.Fatal("located file Back skipped its list")
	}
}
