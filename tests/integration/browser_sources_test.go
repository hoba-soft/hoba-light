package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserStartsWithSources(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("contents"))
	for i := 0; i < 3; i++ {
		c.ok("backup", "-c", "label", c.source)
	}
	refs := c.refs()
	root := c.interactSources(testBinary, "q", nil)
	requireOutput(t, root, "Root", "source")
	for _, ref := range refs {
		if strings.Contains(root, ref.Session[:8]) {
			t.Fatal("root lists sessions")
		}
	}
	requireOutput(t, c.interactSources(testBinary, "\rq", nil), "Started", "label")
	requireOutput(t, c.interactSources(testBinary, "\r/label\r\rq", nil), "label")
}

func TestBrowserCalendarEscReturnsToSources(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("contents"))
	c.ok("backup", c.source)
	out := c.interactSources(testBinary, "D\x1bq", nil)
	requireOutput(t, out, "Select session date (local time)")
	// A cancelled date picker must not activate a filter or consume quit.
	if strings.Contains(out, "Session date:") {
		t.Fatal("Esc applied the calendar date")
	}
}
