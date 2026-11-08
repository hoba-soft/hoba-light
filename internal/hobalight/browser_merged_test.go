package hobalight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergedBrowserVersionsAndMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	s := newStore(t, filepath.Join(root, "store"))
	file := filepath.Join(source, "nested", "file.txt")
	oldOnly := filepath.Join(source, "old-only")
	writeFile(t, file, []byte("older"))
	writeFile(t, oldOnly, []byte("deleted later"))
	_, err := s.backup(source, false)
	must(t, err)
	must(t, os.Remove(oldOnly))
	writeFile(t, file, []byte("newer contents"))
	_, err = s.backup(source, false)
	must(t, err)
	// Overlapping sources must merge the same absolute path, retaining its sessions.
	_, err = s.backup(filepath.Join(source, "nested"), false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{sources: true, browser: browser{store: s}, refs: groupedReferences(refs)}
	u.toggleMergedMode()
	if u.merged == nil {
		t.Fatal(u.message)
	}
	if _, err := u.merged.locateFile(portable(oldOnly)); err != nil {
		t.Fatal("old-only file missing", err)
	}
	node, err := u.merged.locateFile(portable(file))
	must(t, err)
	u.openMergedFile(node)
	if len(u.history.versions) != 2 {
		t.Fatalf("got %d versions", len(u.history.versions))
	}
	if len(u.history.versions[0].sessions) != 2 {
		t.Fatal("overlapping sources lost session references")
	}
	// Choose the older version: Enter must show metadata, never content or OS opener.
	u.history.cursor = 1
	u.key("right")
	if u.detail == nil || u.detail.metadata.Size != 5 || u.streamDiff != nil {
		t.Fatal("Enter did not show older-version metadata")
	}
	destination := filepath.Join(root, "restore")
	u.restoreSelectionTo(destination, false)
	if string(readFile(t, filepath.Join(destination, "nested", "file.txt"))) != "older" {
		t.Fatal("restored wrong version")
	}
	u.key("left")
	if u.detail != nil || u.history == nil {
		t.Fatal("metadata back did not return to versions")
	}
	u.key("left")
	if u.history != nil || u.merged == nil {
		t.Fatal("versions back did not return to tree")
	}
	u.applySessionDate("1900-01-01")
	if u.merged == nil || len(u.merged.entries()) != 0 {
		t.Fatal("merged tree ignored session date")
	}
	u.applySessionDate("")
	if _, err := u.merged.locateFile(portable(file)); err != nil {
		t.Fatal("clear date did not rebuild union", err)
	}
	u.toggleMergedMode()
	if u.merged != nil || !u.sources {
		t.Fatal("mode switch did not return to sources")
	}
}

func TestMergedBrowserFileDirectoryConflict(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	s := newStore(t, filepath.Join(root, "store"))
	conflict := filepath.Join(source, "changed-kind")
	writeFile(t, conflict, []byte("was a file"))
	_, err := s.backup(source, false)
	must(t, err)
	must(t, os.Remove(conflict))
	writeFile(t, filepath.Join(conflict, "child"), []byte("now a directory"))
	_, err = s.backup(source, false)
	must(t, err)
	m, err := s.mergedBrowser()
	must(t, err)
	if _, err := m.locateFile(portable(conflict)); err != nil {
		t.Fatal("older file hidden by directory", err)
	}
	if _, err := m.locateFile(portable(filepath.Join(conflict, "child"))); err != nil {
		t.Fatal("new directory hidden by older file", err)
	}
	for _, full := range []string{"/server/file", "//server/share/file", "C:/Users/me/file"} {
		parts, err := mergedPathParts(full)
		must(t, err)
		if len(parts) < 2 || strings.Join(parts, "") == "" {
			t.Fatal(parts)
		}
	}
}
