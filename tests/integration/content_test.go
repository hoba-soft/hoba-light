package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentCommandAndBrowser(t *testing.T) {
	c, old, _ := contentDiffFixture(t)
	fid := fileIDAt(t, c, old, "/dir/note.txt")
	for _, args := range [][]string{{"view", fid[:8]}, {"view", old, "dir/note.txt"}, {"view", "--mode=text", fid}} {
		out, err := c.commandInput(nil, "/old line\rnN\x06\x02ggGxta\x1b[D", args...)
		must(t, err)
		requireOutput(t, out, "Content (text):", "old line", "Content (hex):", "Match: old line", "End of file")
		if strings.Contains(out, "Comparison ·") {
			t.Fatal("wrong viewer title")
		}
	}
	// Current file, its details, search results, and historical version.
	for _, nav := range []string{"\r\r", "\r\r\r", "/note\r\r\r", "\r\r\rvj"} {
		out := c.interact(testBinary, nav+"cxta\x1b[Dq", nil)
		requireOutput(t, out, "Content (text):", "Content (hex):", "note.txt")
	}
	c.fails("requires a file", "view", old)
	c.fails("requires a file", "view", old, "dir")
	c.fails("usage: view", "view")
	c.fails("mode must be", "view", "--mode=bad", fid)
	c.fails("relative", "view", old, "../dir/note.txt")
	must(t, os.RemoveAll(c.source))
	out, err := c.commandInput(nil, "q", "view", fid)
	must(t, err)
	requireOutput(t, out, "old line")
	if _, err = os.Stat(filepath.Join(c.root, "hobalight_restore")); !os.IsNotExist(err) {
		t.Fatal("viewer restored content to disk")
	}
}
func TestContentFailedFileCLI(t *testing.T) {
	c, _, _, fid, _ := failedSnapshot(t)
	out, err := c.commandInput(nil, "q", "view", fid)
	if err == nil {
		t.Fatal("failed file reported success")
	}
	requireOutput(t, out, "file was not backed up", "publish blob")
}

func TestContentLargeNavigationAndCleanup(t *testing.T) {
	c := newCLI(t)
	var data strings.Builder
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&data, "row %04d %s\n", i, strings.Repeat("wide ", 40))
	}
	writeFile(t, filepath.Join(c.source, "large.txt"), []byte(data.String()))
	c.ok("backup", c.source)
	session := latestRef(c.refs()).Session
	fid := fileIDAt(t, c, session, "/large.txt")
	temp := filepath.Join(c.root, "temp")
	must(t, os.MkdirAll(temp, 0700))
	env := map[string]string{"TMPDIR": temp, "TEMP": temp, "TMP": temp}
	out, err := c.commandInput(env, "/row 1400\rnkN\x06\x02\x04\x15ggGhl\x1b[D", "view", fid)
	must(t, err)
	requireOutput(t, out, "Match: row 1400", "row 1499", "End of file")
	entries, err := os.ReadDir(temp)
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("temporary viewer files remain:", entries)
	}
	var f File
	decodeFile(t, c.object(fid), &f)
	must(t, os.Remove(c.object(f.Blob)))
	out, err = c.commandInput(env, "q", "view", fid)
	if err == nil {
		t.Fatal("missing blob reported success")
	}
	requireOutput(t, out, "Error:")
	entries, err = os.ReadDir(temp)
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("failed viewer left temporary files")
	}
}
