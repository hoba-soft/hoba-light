package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contentDiffFixture(t *testing.T) (*cliTest, string, string) {
	t.Helper()
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "dir", "note.txt"), []byte("first\nold line\nlast\n"))
	writeFile(t, filepath.Join(c.source, "binary.bin"), []byte{0, 1, 2, 3, 4})
	writeFile(t, filepath.Join(c.source, "outside.txt"), []byte("same"))
	c.ok("backup", c.source)
	old := latestRef(c.refs()).Session
	writeFile(t, filepath.Join(c.source, "dir", "note.txt"), []byte("first\nnew line\ninserted\nlast\n"))
	writeFile(t, filepath.Join(c.source, "binary.bin"), []byte{0, 1, 5, 3, 4, 6})
	writeFile(t, filepath.Join(c.source, "other", "renamed.txt"), []byte("first\nnew line\ninserted\nlast\n"))
	c.ok("backup", c.source)
	return c, old, latestRef(c.refs()).Session
}
func TestDiffFileAndDirectoryCLI(t *testing.T) {
	c, old, next := contentDiffFixture(t)
	a := fileIDAt(t, c, old, "/dir/note.txt")
	b := fileIDAt(t, c, next, "/dir/note.txt")
	for _, args := range [][]string{
		{"diff", a[:8], b[:8]},
		{"diff", old, next, "dir/note.txt"},
		{"diff", "--mode", "text", old, "dir/note.txt", next, "other/renamed.txt"},
	} {
		out := c.ok(args...)
		requireOutput(t, out, "Content (text):", "-old line", "+new line", "+inserted", " first", " last", "size:")
	}
	out := c.ok("diff", old, next, "dir")
	requireOutput(t, out, "~ note.txt", "Total: 1 file", "20 B -> 1 file", "29 B")
	if strings.Contains(out, "binary.bin") || strings.Contains(out, "outside.txt") {
		t.Fatal("directory scope leaked:", out)
	}
	requireOutput(t, c.ok("diff", old, "dir", next, "other"), "- note.txt", "+ renamed.txt")
	x := fileIDAt(t, c, old, "/binary.bin")
	y := fileIDAt(t, c, next, "/binary.bin")
	requireOutput(t, c.ok("diff", x, y), "Content (hex):", "- 00000000", "00 01 02 03 04", "+ 00000000", "00 01 05 03 04 06")
	requireOutput(t, c.ok("diff", a, b, "--mode=hex"), "Content (hex):", "00000000")
	requireOutput(t, c.ok("diff", a, a), "No content changes")
	c.fails("cannot compare", "diff", a, next)
	c.fails("mode must be", "diff", "--mode=bad", a, b)
	c.fails("relative", "diff", old, next, "../dir")
	c.fails("path not found", "diff", old, next, "absent")
	c.fails("does not accept", "diff", a, b, "dir")
	// No reliance on reference files, original sources, or migrations.
	must(t, os.RemoveAll(c.source))
	must(t, os.RemoveAll(filepath.Join(c.storage, "sessions")))
	requireOutput(t, c.ok("diff", old, next, "dir"), "~ note.txt")
}
func TestDiffBrowserContentAndHistory(t *testing.T) {
	c, _, _ := contentDiffFixture(t)
	// History from dir/note, compare two versions, search, navigate, switch modes,
	// return to history and verify that its original selection remains available.
	out := c.interact(testBinary, "\r\r\rvdjd/new line\rnN\x06\x02ggGxta\x1b[Dq", nil)
	requireOutput(t, out, "File history:", "Comparison", "Content (text):", "new line", "Content (hex):", "Match: new line")
	// Compare matching directory entries across sessions.
	out = c.interact(testBinary, "\rd\x1b[Dj\rdq", nil)
	requireOutput(t, out, "~ note.txt", "Total: 1 file")
	// Session summary: search for modified row, Enter to contents, back to summary.
	out = c.interact(testBinary, "jdkd/~ dir/note.txt\r\r\x1b[Dq", nil)
	requireOutput(t, out, "Total:", "Content (text):", "Comparison")
	// Added files compare against an empty side and retain the parent view.
	out = c.interact(testBinary, "jdkd/+ other/renamed.txt\r\r\x1b[Dq", nil)
	requireOutput(t, out, "(absent)", "+first", "+new line", "Total:")
	// Reverse order exposes the same path as a deletion.
	out = c.interact(testBinary, "djd/- other/renamed.txt\r\r\x1b[Dq", nil)
	requireOutput(t, out, "(absent)", "-first", "-new line")
}
func TestDiffLongLineBinaryInvocation(t *testing.T) {
	c := newCLI(t)
	name := filepath.Join(c.source, "long.txt")
	writeFile(t, name, []byte(strings.Repeat("a", 200000)))
	c.ok("backup", c.source)
	old := latestRef(c.refs()).Session
	writeFile(t, name, []byte(strings.Repeat("b", 210000)))
	c.ok("backup", c.source)
	next := latestRef(c.refs()).Session
	out := c.ok("diff", old, next, "long.txt")
	requireOutput(t, out, "Line continues (4096-byte fragment)", "No newline at end of file")
	// Count actual content, excluding headers and explanatory lines.
	var removed, added int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "-a") {
			removed += len(line) - 1
		}
		if strings.HasPrefix(line, "+b") {
			added += len(line) - 1
		}
	}
	if removed != 200000 || added != 210000 {
		t.Fatalf("lost content: %d/%d", removed, added)
	}
}
func TestDiffFailedFileCLI(t *testing.T) {
	c, before, failed, _, obstruction := failedSnapshot(t)
	must(t, os.RemoveAll(obstruction))
	c.fails("content unavailable", "diff", before, failed, "nested/bad.txt")
	out := c.ok("diff", before, failed, "nested")
	requireOutput(t, out, "error", "Total:")
}

func TestComparisonBaseVisibleInBrowser(t *testing.T) {
	c, _, _ := contentDiffFixture(t)
	out := c.interact(testBinary, "\r\rdd\x1b[D\x1b[Dq", nil)
	requireOutput(t, out, "[compare base]", "Comparison selection cleared")
}
