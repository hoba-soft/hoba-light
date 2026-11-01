package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteCommandAndGC(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("data"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	if out := c.ok("delete", "--force", id[:8]); !strings.Contains(out, "Deleted session "+id[:8]) {
		t.Fatal(out)
	}
	if len(c.refs()) != 0 {
		t.Fatal("reference remains")
	}
	if _, err := os.Stat(c.object(id)); !os.IsNotExist(err) {
		t.Fatal("session remains")
	}
	if len(c.ids('F')) != 1 || len(c.ids('B')) != 1 {
		t.Fatal("delete collected data")
	}
	if out := c.ok("refresh"); !strings.Contains(out, "0 refreshed") {
		t.Fatal(out)
	}
	if out := c.ok("gc"); !strings.Contains(out, "0 retained (") || !strings.Contains(out, "2 moved to trash") {
		t.Fatal(out)
	}
	c.fails("usage: delete", "delete")
	c.fails("usage: delete", "delete", id, "path", "extra")
	c.fails("object not found", "delete", id)
}

func TestBrowserDeleteCancellationAndConfirmation(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("data"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	for _, input := range []string{"xnq", "x\rq", "x", "xq"} {
		out := c.interact(testBinary, input, nil)
		if !strings.Contains(out, "Delete session?") {
			t.Fatal(out)
		}
		if _, err := os.Stat(c.object(id)); err != nil {
			t.Fatal("deleted without confirmation")
		}
	}
	out := c.interact(testBinary, "\x1b[3~yq", nil)
	if !strings.Contains(out, "Deleted session "+id[:8]) || !strings.Contains(out, "No backup sessions.") {
		t.Fatal(out)
	}
	if len(c.refs()) != 0 {
		t.Fatal("reference remains")
	}
}

func TestDeletePathsReplacesSession(t *testing.T) {
	c := newCLI(t)
	contents := map[string][]byte{"private/a.txt": []byte("secret"), "private/deep/b.txt": []byte("more secret"), "private-other/keep.txt": []byte("keep"), "top.txt": []byte("top")}
	for name, data := range contents {
		writeFile(t, filepath.Join(c.source, filepath.FromSlash(name)), data)
	}
	c.ok("backup", c.source)
	original := latestRef(c.refs()).Session
	old := c.session(original)
	// Another session must retain shared file objects through gc.
	c.ok("backup", c.source)
	shared := latestRef(c.refs()).Session
	beforeObjects := len(c.ids('S'))
	for _, selected := range []string{"../escape", "/absolute", "absent"} {
		if out, err := c.command(nil, "delete", original, selected); err == nil {
			t.Fatal(out)
		}
	}
	if len(c.ids('S')) != beforeObjects {
		t.Fatal("invalid deletion created a replacement")
	}
	requireOutput(t, c.ok("delete", "--force", original, "private/"), "Updated session", "removed 2 files")
	var replacement string
	for _, ref := range c.refs() {
		if ref.Session != shared {
			replacement = ref.Session
		}
	}
	if replacement == "" || replacement == original {
		t.Fatal("replacement not published")
	}
	next := c.session(replacement)
	if next.Started != old.Started || next.Directory != old.Directory || next.Version != old.Version || next.Platform != old.Platform || len(next.Files) != 2 {
		t.Fatalf("%+v", next)
	}
	if _, err := os.Stat(c.object(original)); !os.IsNotExist(err) {
		t.Fatal("old root still active")
	}
	trash, err := filepath.Glob(filepath.Join(c.storage, "trash", original+"-*.replaced"))
	must(t, err)
	if len(trash) != 1 {
		t.Fatal("old session not archived")
	}
	c.ok("gc")
	for _, id := range old.Files {
		if _, err := os.Stat(c.object(id)); err != nil {
			t.Fatal("shared file lost", err)
		}
	}
	dest := filepath.Join(c.root, "restored")
	c.ok("restore-to", replacement, ".", dest)
	assertTree(t, dest, map[string][]byte{"private-other/keep.txt": []byte("keep"), "top.txt": []byte("top")})
	c.ok("delete", "--force", shared)
	c.ok("gc")
	if len(c.ids('F')) != 2 {
		t.Fatal("unused file objects remain")
	}
	c.ok("refresh")
	if len(c.refs()) != 1 || c.refs()[0].Session != replacement {
		t.Fatal("refresh resurrected old session")
	}
	c.ok("delete", "--force", replacement, "top.txt")
	replacement = c.refs()[0].Session
	if len(c.session(replacement).Files) != 1 {
		t.Fatal("file deletion failed")
	}
	c.ok("delete", "--force", replacement, ".")
	replacement = c.refs()[0].Session
	if len(c.session(replacement).Files) != 0 {
		t.Fatal("clear session failed")
	}
	c.ok("gc")
	if len(c.ids('F')) != 0 || len(c.ids('B')) != 0 {
		t.Fatal("unreferenced data remains")
	}
}

func TestDeleteFailedFileRemovesDiagnostic(t *testing.T) {
	c, _, failed, _, obstruction := failedSnapshot(t)
	c.ok("delete", "--force", failed, "nested/bad.txt")
	// Find the replacement by preserved timestamp and absence of the failed path.
	for _, ref := range c.refs() {
		session := c.session(ref.Session)
		if len(session.Files) == 2 {
			if len(session.Errors) != 0 {
				t.Fatal("obsolete failure diagnostic remains")
			}
			requireOutput(t, c.ok("list", ref.Session, "nested"), "1 file")
			must(t, os.RemoveAll(obstruction))
			return
		}
	}
	t.Fatal("replacement missing")
}

func TestBrowserDeletePaths(t *testing.T) {
	for _, tc := range []struct {
		name, navigate, path string
		remaining            int
	}{
		{"directory", "\r", "nested", 1},
		{"file", "\r\r", "nested/file.txt", 1},
		{"file details", "\r\r\r", "nested/file.txt", 1},
		{"search result", "/file\r", "nested/file.txt", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCLI(t)
			writeFile(t, filepath.Join(c.source, "nested", "file.txt"), []byte("data"))
			writeFile(t, filepath.Join(c.source, "top.txt"), []byte("top"))
			c.ok("backup", c.source)
			original := c.refs()[0].Session
			for _, cancel := range []string{"nq", "\rq", "q", ""} {
				out := c.interact(testBinary, tc.navigate+"x"+cancel, nil)
				requireOutput(t, out, "Delete from session?", tc.path)
				if c.refs()[0].Session != original {
					t.Fatal("deleted without confirmation")
				}
			}
			out := c.interact(testBinary, tc.navigate+"\x1b[3~yq", nil)
			requireOutput(t, out, "Updated session", "removed 1 file")
			updated := c.refs()[0].Session
			if updated == original || len(c.session(updated).Files) != tc.remaining {
				t.Fatal("wrong replacement")
			}
			c.ok("restore-to", updated, ".", filepath.Join(c.root, "restored"))
			assertTree(t, filepath.Join(c.root, "restored"), map[string][]byte{"top.txt": []byte("top")})
		})
	}
}
