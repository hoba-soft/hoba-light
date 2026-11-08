package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func historyFixture(t *testing.T) (*cliTest, string, string, string) {
	t.Helper()
	c := newCLI(t)
	target := filepath.Join(c.source, "target.txt")
	writeFile(t, target, []byte("old version"))
	writeFile(t, filepath.Join(c.source, "other", "target.txt"), []byte("different file"))
	c.ok("backup", c.source)
	oldest := latestRef(c.refs()).Session
	c.ok("backup", c.source)
	writeFile(t, target, []byte("new version with different size"))
	c.ok("backup", c.source)
	newest := latestRef(c.refs()).Session
	return c, target, oldest, newest
}

func TestHistoryCommand(t *testing.T) {
	c, target, oldest, newest := historyFixture(t)
	oldFile := fileIDAt(t, c, oldest, "/source/target.txt")
	newFile := fileIDAt(t, c, newest, "/source/target.txt")
	other := fileIDAt(t, c, oldest, "/other/target.txt")
	for _, args := range [][]string{{"history", target}, {"history", oldest[:8], "target.txt"}, {"history", newest, "target.txt"}} {
		out := c.ok(args...)
		requireOutput(t, out, "History:", oldFile[:8], newFile[:8], "2 sessions", "1 session", "11 B")
		if strings.Count(out, oldFile[:8]) != 1 || strings.Contains(out, other[:8]) {
			t.Fatal("versions not grouped or path match not exact:", out)
		}
		if strings.Index(out, newFile[:8]) > strings.Index(out, oldFile[:8]) {
			t.Fatal("wrong version ordering:", out)
		}
	}
	c.fails("usage: history", "history")
	c.fails("absolute source path", "history", "target.txt")
	c.fails("file not found", "history", oldest, "other")
	c.fails("file not found", "history", oldest, "absent.txt")
	c.fails("relative", "history", oldest, "../target.txt")
	requireOutput(t, c.ok("history", filepath.Join(c.source, "absent.txt")), "No file versions found")
	must(t, os.RemoveAll(filepath.Join(c.storage, "sessions")))
	requireOutput(t, c.ok("history", target), "2 sessions", newFile[:8], oldFile[:8])
	c.ok("refresh")
	c.ok("delete", "--force", oldFile)
	out := c.ok("history", target)
	if strings.Contains(out, oldFile[:8]) {
		t.Fatal("history includes archived versions:", out)
	}
	requireOutput(t, out, newFile[:8])
}

func TestHistoryBrowser(t *testing.T) {
	c, _, oldest, _ := historyFixture(t)
	oldFile := fileIDAt(t, c, oldest, "/source/target.txt")
	for _, navigation := range []string{"\r\x1b[F", "\r\x1b[F\r", "\r/target\r\x1b[F"} {
		t.Run(navigation, func(t *testing.T) {
			local := *c
			local.t = t
			out := local.interact(testBinary, navigation+"v\x1b[B\r\x1b[B\r\rq", nil)
			requireOutput(t, out, "File history:", "2 sessions", "Version: "+oldFile[:8], "Located target.txt", "File object: "+oldFile[:8])
		})
	}
	// Restore directly from the older grouped version, without leaving history.
	out := c.interact(testBinary, "\r\x1b[Fv\x1b[Btold-version\rq", nil)
	requireOutput(t, out, "Restored 1 file")
	assertTree(t, filepath.Join(c.root, "old-version"), map[string][]byte{"target.txt": []byte("old version")})
	// Escape/back returns from sessions to versions, then to the original file list.
	out = c.interact(testBinary, "\r\x1b[Fv\r\x1b[D\x1b[D\rq", nil)
	requireOutput(t, out, "File history:", "First backed up", "Path:", "Blob object:")
	out = c.interact(testBinary, "v\r vq", nil)
	requireOutput(t, out, "Select a file to view its history")
}

func TestHistoryIncludesFailedVersion(t *testing.T) {
	c, _, failed, fid, obstruction := failedSnapshot(t)
	must(t, os.RemoveAll(obstruction))
	out := c.ok("history", failed, "nested/bad.txt")
	requireOutput(t, out, "[error] "+fid[:8], "11 B")
	out = c.interact(testBinary, "\r\rvrq", nil)
	requireOutput(t, out, "File history:", "[error]", "file was not backed up")
}
