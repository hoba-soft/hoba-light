package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Force a real publication failure on every OS, including privileged CI accounts.
// The failed object and session are produced by the executable, not fabricated.
func failedSnapshot(t *testing.T) (*cliTest, string, string, string, string) {
	t.Helper()
	c := newCLI(t)
	for name, contents := range map[string]string{"nested/bad.txt": "bad content", "nested/good.txt": "good content", "top.txt": "top"} {
		writeFile(t, filepath.Join(c.source, filepath.FromSlash(name)), []byte(contents))
	}
	c.ok("backup", c.source)
	before := latestRef(c.refs()).Session
	var blob string
	for _, id := range c.session(before).Files {
		var f File
		decodeFile(t, c.object(id), &f)
		if strings.HasSuffix(f.Path, "/bad.txt") {
			blob = c.object(f.Blob)
		}
	}
	if blob == "" {
		t.Fatal("missing fixture file")
	}
	must(t, os.Remove(blob))
	makeDir(t, blob)
	writeFile(t, filepath.Join(blob, "obstruction"), []byte("keep"))
	out, err := c.command(nil, "backup", "--force", c.source)
	if err == nil {
		t.Fatal("backup unexpectedly succeeded")
	}
	for _, want := range []string{"session failed", "3/3 (100%)", "1 error", "Backup failed:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	failed := latestRef(c.refs()).Session
	var fid string
	for _, id := range c.session(failed).Files {
		var f File
		decodeFile(t, c.object(id), &f)
		if f.Error != "" {
			fid = id
			if f.Blob != "" || f.Size != 11 || f.Modified == "" || !strings.Contains(f.Error, "publish blob") {
				t.Fatalf("%+v", f)
			}
		}
	}
	if fid == "" {
		t.Fatal("no failed file in session")
	}
	return c, before, failed, fid, blob
}

func requireOutput(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(out, part) {
			t.Fatalf("missing %q:\n%s", part, out)
		}
	}
}

func TestFailedSnapshotCommands(t *testing.T) {
	c, before, failed, fid, blob := failedSnapshot(t)
	requireOutput(t, strings.Join(strings.Fields(c.ok("list")), " "), "3 1", "Files Errors", failed[:8])
	requireOutput(t, strings.Join(strings.Fields(c.ok("list", failed)), " "), "2 1", "nested/")
	requireOutput(t, c.ok("list", failed, "nested"), "[error] bad.txt", "11 B", "good.txt")
	requireOutput(t, c.ok("list", failed, "nested/bad.txt"), "Error:", "publish blob", fid[:8])
	for _, args := range [][]string{{"find", "bad"}, {"find", failed, "bad"}, {"find", failed, "nested", "bad"}} {
		requireOutput(t, c.ok(args...), "[error] nested/bad.txt", "11 B")
	}
	requireOutput(t, c.ok("diff", before, failed), "~ nested/bad.txt", "error", "publish blob")
	c.fails("file was not backed up", "restore", fid)
	c.fails("file was not backed up", "restore-to", fid, filepath.Join(c.root, "failed-file"))
	c.fails("file was not backed up", "restore", failed)
	assertTree(t, filepath.Join(c.root, "hobalight_restore", failed[:8]), map[string][]byte{"nested/good.txt": []byte("good content"), "top.txt": []byte("top")})
	destination := filepath.Join(c.root, "partial")
	c.fails("file was not backed up", "restore-to", failed, "nested", destination)
	assertTree(t, destination, map[string][]byte{"nested/good.txt": []byte("good content")})
	// Remove the intentional invalid object-directory before maintenance.
	must(t, os.RemoveAll(blob))
	// Reconstruct the display index solely from session/file objects.
	must(t, os.RemoveAll(filepath.Join(c.storage, "sessions")))
	c.ok("refresh")
	requireOutput(t, c.ok("list", failed, "nested"), "[error] bad.txt", "2 files, 1 errors")
	// Run a normal backup, with no force or special retry flag.
	c.ok("backup", c.source)
	healthy := latestRef(c.refs()).Session
	if out := c.ok("list", healthy, "nested"); strings.Contains(out, "[error]") {
		t.Fatal(out)
	}
	requireOutput(t, c.ok("diff", failed, healthy), "~ nested/bad.txt", "error")
	c.ok("gc")
	if _, err := os.Stat(c.object(fid)); err != nil {
		t.Fatal("GC removed reachable failed object:", err)
	}
	c.ok("delete", "--force", failed)
	c.ok("gc")
	if _, err := os.Stat(c.object(fid)); !os.IsNotExist(err) {
		t.Fatal("GC kept orphaned failed object:", err)
	}
	c.ok("refresh")
	for _, ref := range c.refs() {
		if ref.Session == failed {
			t.Fatal("refresh resurrected deleted session")
		}
	}
	c.ok("restore-to", healthy, ".", filepath.Join(c.root, "healthy"))
	assertTree(t, filepath.Join(c.root, "healthy"), map[string][]byte{"nested/bad.txt": []byte("bad content"), "nested/good.txt": []byte("good content"), "top.txt": []byte("top")})
}

func TestFailedSnapshotBrowser(t *testing.T) {
	c, _, failed, _, _ := failedSnapshot(t)
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"details", "\r\r\rq", []string{"[error] bad.txt", "Size: 11 bytes", "Error:"}},
		{"search and locate", "\rnbad\r\r\rq", []string{"Find: bad", "[error]", "nested/bad.txt", "Located nested/bad.txt", "Error:"}},
		{"failed restore", "\r\rrq", []string{"file was not backed up"}},
		{"failed restore-to", "\r\rtfailed-destination\rq", []string{"file was not backed up"}},
		{"session partial restore", "rq", []string{"restore incomplete"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := *c
			local.t = t
			requireOutput(t, local.interact(testBinary, tc.input, nil), tc.want...)
		})
	}
	assertTree(t, filepath.Join(c.root, "hobalight_restore", failed[:8]), map[string][]byte{"nested/good.txt": []byte("good content"), "top.txt": []byte("top")})
	if _, err := os.Stat(filepath.Join(c.root, "failed-destination", "nested", "bad.txt")); !os.IsNotExist(err) {
		t.Fatal("failed restore created file")
	}
}
