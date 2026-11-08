package hobalight

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func applyBrowserFilter(u *browserUI, text string) {
	u.key("/")
	u.key("clear")
	u.key(text)
	u.key("enter")
}

func TestBrowserFilterSourcesAndSessionComments(t *testing.T) {
	refs := []Reference{
		{Session: strings.Repeat("a", 64) + "S", Directory: "/alpha", Started: "2026-11-08T12:00:00Z", Comment: "Before upgrade"},
		{Session: strings.Repeat("b", 64) + "S", Directory: "/beta", Started: "2026-11-09T12:00:00Z", Comment: "After upgrade"},
	}
	u := browserUI{sources: true, refs: refs}
	row := func() int {
		for i, line := range strings.Split(u.view(100, 24), "\n") {
			if strings.HasPrefix(line, " ─") {
				return i
			}
		}
		return -1
	}
	position := row()
	applyBrowserFilter(&u, "BETA")
	if u.count() != 1 || u.sourceNames()[0] != "/beta" {
		t.Fatal("wrong filtered source")
	}
	if row() != position {
		t.Fatal("filter moved panel")
	}
	u.key("right")
	if u.filter != nil || u.source != "/beta" || len(u.refs) != 1 || u.refs[0].Session != refs[1].Session {
		t.Fatal("filtered selection opened wrong source")
	}
	u.key("left")
	if !u.sources || u.cursor != 1 || u.count() != 2 {
		t.Fatal("back lost source selection")
	}
	u.sources = false
	u.source = "/alpha"
	u.refs = refs
	applyBrowserFilter(&u, "AFTER")
	if len(u.refs) != 1 || u.refs[0].Session != refs[1].Session {
		t.Fatal("comment did not filter sessions")
	}
	u.key("left")
	if u.filter != nil || u.cursor != 1 || len(u.refs) != 2 {
		t.Fatal("clearing filter lost selected session")
	}
	applyBrowserFilter(&u, "2026-11-08")
	if len(u.refs) != 1 || u.refs[0].Session != refs[0].Session {
		t.Fatal("session date did not filter")
	}
	u.key("/")
	u.key("cancel")
	if u.filter == nil || u.filter.text != "2026-11-08" {
		t.Fatal("cancel changed filter")
	}
	applyBrowserFilter(&u, "")
	if u.filter != nil || len(u.refs) != 2 {
		t.Fatal("empty filter did not reset list")
	}
}

func TestBrowserFilterDirectorySelectionAndEmptyActions(t *testing.T) {
	store := newStore(t, t.TempDir())
	ref := Reference{Session: strings.Repeat("c", 64) + "S", Directory: "/source"}
	files := []sessionFile{{relative: "alpha/file.txt"}, {relative: "beta/file.txt"}}
	u := browserUI{browser: browser{store: store}, ref: &ref, files: files}
	u.prepareTree()
	u.entries = u.tree[""]
	applyBrowserFilter(&u, "BETA")
	if len(u.entries) != 1 || u.entries[0].name != "beta" {
		t.Fatal("wrong filtered directory")
	}
	u.key("right")
	if u.current != "beta" || len(u.entries) != 1 || u.entries[0].name != "file.txt" {
		t.Fatal("opened wrong directory")
	}
	u.key("left")
	if u.cursor != 1 {
		t.Fatal("back lost selected directory")
	}
	applyBrowserFilter(&u, "not present")
	for _, key := range []string{"right", "r", "t", "x", "X", "o"} {
		u.key(key)
	}
	if u.filter == nil || u.pendingDelete != nil || u.restoreToInput != nil || u.current != "" {
		t.Fatal("empty filter acted on hidden row")
	}
	u.key("left")
	if len(u.entries) != 2 || u.cursor != 1 {
		t.Fatal("empty filter lost original selection")
	}
}

func TestBrowserFilterMergedAndHistory(t *testing.T) {
	root := &mergedNode{children: map[string]*mergedNode{"alpha": {name: "alpha", file: true}, "beta": {name: "beta", file: true}}}
	u := browserUI{merged: &browserMerged{current: root}}
	applyBrowserFilter(&u, "beta")
	if len(u.merged.entries()) != 1 || u.merged.entries()[0].node.name != "beta" {
		t.Fatal("merged filter failed")
	}
	u.key("left")
	if u.merged.filter != "" || u.merged.cursor != 1 {
		t.Fatal("merged selection lost")
	}
	u.merged = nil
	u.history = &browserHistory{versions: []fileVersion{{id: strings.Repeat("a", 64) + "F", earliest: time.Now(), sessions: []searchMatch{{ref: Reference{Session: strings.Repeat("c", 64) + "S", Started: "2026-11-08T12:00:00Z", Comment: "comment"}}}}}}
	applyBrowserFilter(&u, "missing")
	u.view(100, 24)
	u.key("right")
	u.key("left")
	if len(u.history.versions) != 1 {
		t.Fatal("history versions lost")
	}
	u.history.sessions = true
	applyBrowserFilter(&u, "missing")
	u.view(100, 24)
	if _, ok := u.history.selected(); ok {
		t.Fatal("empty session filter selected hidden session")
	}
	u.key("left")
	applyBrowserFilter(&u, "COMMENT")
	if len(u.history.versions[0].sessions) != 1 {
		t.Fatal("history comment filter failed")
	}
}

func TestBrowserSearchNamesDirectoriesAndGlobalScope(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "root-only-label")
	writeFile(t, filepath.Join(source, "PhotoAlbum", "nested", "one.txt"), []byte("one"))
	writeFile(t, filepath.Join(source, "PhotoAlbum", "two.txt"), []byte("two"))
	writeFile(t, filepath.Join(source, "photo.jpg"), []byte("picture"))
	id, err := s.backupWithComment(source, false, nil, "comment-only-label")
	must(t, err)
	second := filepath.Join(root, "other-source")
	writeFile(t, filepath.Join(second, "PHOTO.png"), []byte("second"))
	_, err = s.backup(second, false)
	must(t, err)
	for _, text := range []string{"root-only-label", "comment-only-label", "PhotoAlbum/one"} {
		matches, err := s.findBrowserNames(text)
		must(t, err)
		if len(matches) != 0 {
			t.Fatalf("non-leaf match for %q: %+v", text, matches)
		}
	}
	matches, err := s.findBrowserNames("PHOTO")
	must(t, err)
	if len(matches) != 3 {
		t.Fatalf("want directory and two files, got %+v", matches)
	}
	var directory searchMatch
	for _, m := range matches {
		if m.directory {
			directory = m
		}
	}
	if directory.file.relative != "PhotoAlbum" {
		t.Fatal("directory not identified")
	}
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: groupedReferences(refs), sources: true}
	u.selectSource(portable(second))
	u.key("n")
	u.key("PHOTO")
	u.key("enter")
	if len(u.search.matches) != 3 {
		t.Fatal("search confined to current source")
	}
	u.locateMatch(directory)
	if u.ref.Session != id || u.current != "PhotoAlbum" || len(u.entries) != 2 {
		t.Fatal("directory result did not open directory")
	}
	u.key("left")
	if u.current != "" || u.entries[u.cursor].name != "PhotoAlbum" {
		t.Fatal("directory search lost parent selection")
	}
	u.search = &browserSearch{matches: []searchMatch{directory}}
	destination := filepath.Join(root, "restore")
	u.restoreSelectionTo(destination, false)
	if string(readFile(t, filepath.Join(destination, "PhotoAlbum", "two.txt"))) != "two" {
		t.Fatal("directory restore failed")
	}
	// Search remains global under the selected local session-day filter.
	s.sessionDate = "1900-01-01"
	matches, err = s.findBrowserNames("photo")
	must(t, err)
	if len(matches) != 0 {
		t.Fatal("date filter ignored")
	}
}

func TestBrowserSearchFromHistoryAndHistoryFromSearch(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file.txt"), []byte("contents"))
	_, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: refs}
	u.key("n")
	u.key("file")
	u.key("enter")
	u.key("v")
	if u.history == nil || !strings.Contains(u.view(100, 24), "File history:") {
		t.Fatal("search result did not show history")
	}
	history := u.history
	applyBrowserFilter(&u, "missing")
	if len(u.history.versions) != 0 {
		t.Fatal("history filter affected underlying search instead")
	}
	u.key("left")
	u.key("n")
	u.key("file")
	u.key("enter")
	if u.history != nil || !strings.Contains(u.view(100, 24), "Find: file") {
		t.Fatal("search hidden behind history")
	}
	u.key("left")
	if u.history != history {
		t.Fatal("search back lost history")
	}
	u.key("left")
	if u.history != nil || u.search == nil {
		t.Fatal("history back lost original search")
	}
	// Filtering history still works when it overlays file metadata.
	u.locateSearchResult()
	u.key("right")
	u.key("v")
	if u.detail == nil || u.history == nil {
		t.Fatal("missing metadata history fixture")
	}
	applyBrowserFilter(&u, "missing")
	if len(u.history.versions) != 0 {
		t.Fatal("metadata prevented history filtering")
	}
	u.key("left")
	u.key("n")
	u.key("cancel")
	if u.history == nil || u.searchInput != nil {
		t.Fatal("cancel search lost history")
	}
}

func TestBrowserGlobalSearchDeleteAcrossSources(t *testing.T) {
	for _, directory := range []bool{false, true} {
		for _, merged := range []bool{false, true} {
			t.Run(fmt.Sprintf("directory=%t/merged=%t", directory, merged), func(t *testing.T) {
				root := t.TempDir()
				s := newStore(t, filepath.Join(root, "store"))
				first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
				writeFile(t, filepath.Join(first, "keep.txt"), []byte("first"))
				firstID, err := s.backup(first, false)
				must(t, err)
				selected := "target.txt"
				if directory {
					selected = "target-folder"
					writeFile(t, filepath.Join(second, selected, "nested.txt"), []byte("remove"))
				} else {
					writeFile(t, filepath.Join(second, selected), []byte("remove"))
				}
				writeFile(t, filepath.Join(second, "keep.txt"), []byte("second"))
				_, err = s.backup(second, false)
				must(t, err)
				refs, err := s.sessionReferences()
				must(t, err)
				u := browserUI{sources: true, browser: browser{store: s}, refs: groupedReferences(refs)}
				u.selectSource(portable(first))
				if merged {
					u.toggleMergedMode()
				}
				u.key("n")
				u.key("target")
				u.key("enter")
				if len(u.search.matches) != 1 {
					t.Fatal("wrong search fixture")
				}
				u.key("x")
				if u.pendingDelete == nil || u.pendingDelete.Directory != portable(second) || u.pendingDeletePath != selected {
					t.Fatal("cross-source delete did not select result")
				}
				u.key("y")
				if u.merged != nil || u.ref == nil || u.ref.Directory != portable(second) || u.history != nil || u.search != nil {
					t.Fatalf("delete left wrong screen: %s", u.view(100, 24))
				}
				_, files, err := s.listingFiles(u.ref.Session)
				must(t, err)
				if len(files) != 1 || files[0].relative != "keep.txt" {
					t.Fatal("delete affected wrong files")
				}
				_, files, err = s.listingFiles(firstID)
				must(t, err)
				if len(files) != 1 || files[0].relative != "keep.txt" {
					t.Fatal("delete affected original source")
				}
			})
		}
	}
}

func TestBrowserMergedSearchHistoryOpensMetadata(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file.txt"), []byte("contents"))
	_, err := s.backup(source, false)
	must(t, err)
	u := browserUI{browser: browser{store: s}}
	u.toggleMergedMode()
	u.key("n")
	u.key("file")
	u.key("enter")
	u.key("v")
	u.key("right")
	if u.detail == nil || !strings.Contains(u.view(100, 24), "File version metadata") {
		t.Fatal("merged search history hid metadata")
	}
	u.key("left")
	if u.detail != nil || u.history == nil {
		t.Fatal("metadata back lost history")
	}
	u.key("left")
	if u.history != nil || u.search == nil {
		t.Fatal("history back lost search")
	}
}

func TestBrowserFilterSearchDirectoryAndHistoryComments(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	file := filepath.Join(source, "folder", "file.txt")
	writeFile(t, file, []byte("contents"))
	_, err := s.backupWithComment(source, false, nil, "Before upgrade")
	must(t, err)
	u := browserUI{browser: browser{store: s}}
	u.key("n")
	u.key("folder")
	u.key("enter")
	applyBrowserFilter(&u, "FOLDER/")
	if len(u.search.matches) != 1 || !u.search.matches[0].directory {
		t.Fatal("displayed directory suffix did not filter")
	}
	u.clearFilter()
	versions, err := s.historyVersions(portable(file))
	must(t, err)
	u.search = nil
	u.history = &browserHistory{versions: versions, path: portable(file), sessions: true}
	if !strings.Contains(u.view(100, 24), "Before upgrade") {
		t.Fatal("history omitted real session comment")
	}
	applyBrowserFilter(&u, "UPGRADE")
	if len(u.history.versions[0].sessions) != 1 {
		t.Fatal("real session comment did not filter history")
	}
}
