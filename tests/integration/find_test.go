package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFindCommand(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "nested", "Report.txt"), []byte("hello"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	for _, args := range [][]string{{"find", "REPORT"}, {"find", id[:8], "report"}, {"find", id[:8], "nested", "report"}} {
		out := c.ok(args...)
		for _, want := range []string{id[:8], "nested/Report.txt", "5 B"} {
			if !strings.Contains(out, want) {
				t.Fatal(out)
			}
		}
	}
	c.fails("usage: find", "find")
	c.fails("directory not found", "find", id, "absent", "report")
	if out := c.ok("find", "absent"); !strings.Contains(out, "No matching files") {
		t.Fatal(out)
	}
}
