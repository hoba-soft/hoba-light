package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGCCommand(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("hello"))
	c.ok("backup", c.source)
	session := latestRef(c.refs()).Session
	orphan := strings.Repeat("e", 64) + "B"
	writeFile(t, c.object(orphan), []byte("unused"))
	must(t, os.RemoveAll(filepath.Join(c.storage, "sessions")))
	out := c.ok("gc")
	if !strings.Contains(out, "3 retained (") || !strings.Contains(out, "1 moved to trash") {
		t.Fatal(out)
	}
	if _, err := os.Stat(c.object(session)); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(c.storage, "trash", orphan+"-*.orphan"))
	must(t, err)
	if len(matches) != 1 || string(readFile(t, matches[0])) != "unused" {
		t.Fatal(matches)
	}
	c.fails("usage: gc", "gc", "extra")
}
