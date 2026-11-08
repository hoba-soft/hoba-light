package hobalight

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserSourceLevels(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, source := range []string{first, second} {
		writeFile(t, filepath.Join(source, "file"), []byte("contents"))
		for i := 0; i < 3; i++ {
			_, err := s.backup(source, false)
			must(t, err)
		}
	}
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{sources: true, browser: browser{store: s}, refs: groupedReferences(refs), stats: s.referenceStats(refs)}
	if u.count() != 2 {
		t.Fatal("root should contain two sources")
	}
	view := u.view(120, 24)
	if strings.Contains(view, refs[0].Session[:8]) {
		t.Fatal("root lists individual sessions")
	}
	u.cursor = 1
	u.enter()
	if u.sources || u.count() != 3 || u.source != portable(second) {
		t.Fatalf("wrong source: %s", u.source)
	}
	query := "file"
	u.searchInput = &query
	u.searchInputKey("enter")
	if len(u.search.matches) != 6 {
		t.Fatal("search must include every source")
	}
	u.searchKey("left")
	u.cursor = 2
	u.enter()
	if u.ref == nil {
		t.Fatal("session not opened")
	}
	u.back()
	if u.ref != nil || u.cursor != 2 || u.count() != 3 {
		t.Fatal("back did not return to selected session")
	}
	u.back()
	if !u.sources || u.cursor != 1 || u.count() != 2 {
		t.Fatal("back did not return to selected source")
	}
	u.enter()
	u.requestDelete()
	if u.pendingDelete == nil {
		t.Fatal("session not selected for deletion")
	}
	u.confirmDelete()
	if u.count() != 2 {
		t.Fatal("deleted session remained in scoped list")
	}
	u.back()
	if u.count() != 2 {
		t.Fatal("source disappeared")
	}
	u.enter()
	if u.count() != 2 {
		t.Fatal("deleted session reappeared")
	}
	must(t, s.verify(nil, io.Discard))
	for u.count() > 0 {
		u.requestDelete()
		u.confirmDelete()
	}
	u.back()
	if !u.sources || u.count() != 1 {
		t.Fatal("deleted source reappeared after removing its last session")
	}
	u.enter()
	for u.count() > 0 {
		u.requestDelete()
		u.confirmDelete()
	}
	u.back()
	if !u.sources || u.count() != 0 {
		t.Fatal("empty storage retained deleted sources")
	}
}
