package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMergedBrowserCLI(t *testing.T) {
	c := newCLI(t)
	file := filepath.Join(c.source, "file.txt")
	writeFile(t, file, []byte("older"))
	c.ok("backup", c.source)
	old := retimeSession(t, c, latestRef(c.refs()).Session, "2024-02-28T12:00:00Z")
	writeFile(t, file, []byte("newer contents"))
	c.ok("backup", c.source)
	retimeSession(t, c, latestRef(c.refs()).Session, "2024-02-29T12:00:00Z")
	out := c.interactSources(testBinary, "mnfile\r\r\rq", nil)
	requireOutput(t, out, "Merged files", "File version metadata", "Size:")
	out, err := c.commandInput(nil, "mnfile\r\r\rq", "browse", "--date", "2024-02-28")
	must(t, err)
	requireOutput(t, out, "File version metadata", old[:8])
	out = c.interactSources(testBinary, "mmq", nil)
	requireOutput(t, out, "Merged files mode", "Sessions mode", "Root")
}
func TestSingleFileSessionOpensListBeforeMetadata(t *testing.T) {
	c := newCLI(t)
	file := filepath.Join(c.source, "file.txt")
	writeFile(t, file, []byte("contents"))
	c.ok("backup", file)
	out := c.interactSources(testBinary, "\r\rq", nil)
	requireOutput(t, out, "Name", "file.txt")
	if strings.Contains(out, "Blob object:") {
		t.Fatal("single-file session skipped its file list", out)
	}
	out = c.interactSources(testBinary, "\r\r\r\x1b[Dq", nil)
	requireOutput(t, out, "Size: 8 bytes", "Blob object:")
}
