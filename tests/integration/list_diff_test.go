package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestIntegrationListAndDiff(t *testing.T) {
	c := newCLI(t)
	if out := c.ok("--help"); !strings.Contains(out, "interactive backup browser") || !strings.Contains(out, "diff <old-session>") {
		t.Fatal(out)
	}
	for p, data := range map[string]string{"top.txt": "top", "nested/move.txt": "move", "deleted.txt": "delete", "modified.txt": "old"} {
		writeFile(t, filepath.Join(c.source, filepath.FromSlash(p)), []byte(data))
	}
	c.ok("backup", c.source)
	first := c.refs()[0].Session
	if out := regexp.MustCompile(` {2,}`).ReplaceAllString(c.ok("list"), "  "); !strings.Contains(out, first[:8]) {
		t.Fatal(out)
	}
	out := c.ok("list", first[:8])
	if !strings.Contains(out, "nested/") || strings.Contains(out, "move.txt") || !strings.Contains(out, "top.txt") || strings.Contains(out, "directory  ") || regexp.MustCompile(`[0-9a-f]{8} [FB] `).MatchString(out) {
		t.Fatal(out)
	}
	out = c.ok("list", first[:8], "nested")
	if !strings.Contains(out, "move.txt  ") {
		t.Fatal(out)
	}
	out = c.ok("list", first[:8], "nested/move.txt")
	if !strings.Contains(out, "nested/move.txt") {
		t.Fatal(out)
	}
	must(t, os.Rename(filepath.Join(c.source, "nested", "move.txt"), filepath.Join(c.source, "moved.txt")))
	must(t, os.Remove(filepath.Join(c.source, "deleted.txt")))
	writeFile(t, filepath.Join(c.source, "added.txt"), []byte("new"))
	writeFile(t, filepath.Join(c.source, "modified.txt"), []byte("new modified"))
	c.ok("backup", c.source)
	refs := c.refs()
	second := refs[len(refs)-1].Session
	out = c.ok("diff", first[:8], second[:8])
	for _, want := range []string{"--- " + first[:8], "+++ " + second[:8], "+ added.txt", "- deleted.txt", "+ moved.txt", "- nested/move.txt", "~ modified.txt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	expectedOrder := []string{"- deleted.txt", "- nested/move.txt", "+ added.txt", "+ moved.txt", "~ modified.txt"}
	previous := -1
	for _, line := range expectedOrder {
		position := strings.Index(out, line)
		if position <= previous {
			t.Fatalf("incorrect diff ordering: %s", out)
		}
		previous = position
	}
	if strings.Contains(out, "top.txt") {
		t.Fatal("unchanged file appears in diff")
	}
	out = c.ok("diff", first[:8], first[:8])
	if !strings.Contains(out, "No file changes") {
		t.Fatal(out)
	}
	out = c.interact(testBinary, "\x1b[Bddd\x1b[AdjklgG\x02\x06\x1b[Dq", nil)
	if !strings.Contains(out, "+ moved.txt") || !strings.Contains(out, "- nested/move.txt") {
		t.Fatal(out)
	}
	c.fails("usage: diff", "diff", first[:8])
	c.fails("path must be relative", "list", first[:8], "../escape")
	c.fails("directory not found", "list", first[:8], "missing")
}

func TestIntegrationGroupedSessions(t *testing.T) {
	c := newCLI(t)
	alpha, beta := filepath.Join(c.root, "Alpha"), filepath.Join(c.root, "Beta")
	writeFile(t, filepath.Join(alpha, "note.txt"), []byte("alpha"))
	writeFile(t, filepath.Join(beta, "note.txt"), []byte("beta"))
	c.ok("backup", alpha)
	oldest := latestRef(c.refs()).Session[:8]
	c.ok("backup", beta)
	betaID := latestRef(c.refs()).Session[:8]
	c.ok("backup", alpha)
	newest := latestRef(c.refs()).Session[:8]
	out := c.ok("list")
	if strings.Count(out, filepath.ToSlash(alpha)) != 1 || strings.Count(out, filepath.ToSlash(beta)) != 1 || strings.Index(out, newest) > strings.Index(out, oldest) || strings.Index(out, oldest) > strings.Index(out, filepath.ToSlash(beta)) {
		t.Fatal(out)
	}
	c.fails(`unknown command "sessions"`, "sessions")
	if help := c.ok("--help"); strings.Contains(help, "Alias for list") {
		t.Fatal(help)
	}
	ui := c.interactSources(testBinary, "\r\x1b[B\r\x1b[D\x1b[D\x1b[B\r\rq", nil)
	if !strings.Contains(ui, oldest) || !strings.Contains(ui, betaID) {
		t.Fatal(ui)
	}
}

func TestIntegrationListingSizes(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "plain.txt"), make([]byte, 512))
	writeFile(t, filepath.Join(c.source, "nested", "large.bin"), make([]byte, 1024))
	writeFile(t, filepath.Join(c.source, "nested", "deep", "empty"), nil)
	c.ok("backup", c.source)
	id := latestRef(c.refs()).Session
	// Sizes are snapshot metadata: removing the original source does not affect them.
	must(t, os.RemoveAll(c.source))
	if out := regexp.MustCompile(` {2,}`).ReplaceAllString(c.ok("list"), "  "); !strings.Contains(out, id[:8]+"  3  0  1.5 KiB") {
		t.Fatal(out)
	}
	out := regexp.MustCompile(` {2,}`).ReplaceAllString(c.ok("list", id), "  ")
	for _, want := range []string{"nested/  2  0  1.0 KiB", "512 B", filepath.ToSlash(c.source) + "  3 files  1.5 KiB"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
	if out := regexp.MustCompile(` {2,}`).ReplaceAllString(c.ok("list", id, "nested"), "  "); !strings.Contains(out, "deep/  1  0  0 B") || !strings.Contains(out, "1.0 KiB") {
		t.Fatal(out)
	}
	if out := c.ok("list", id, "nested/deep/empty"); !strings.Contains(out, "0 B") {
		t.Fatal(out)
	}
	ui := regexp.MustCompile(` {2,}`).ReplaceAllString(c.interact(testBinary, "\r\rq", nil), "  ")
	for _, want := range []string{id[:8] + "  3  0  1.5 KiB", "nested/  2  0  1.0 KiB", "512 B", "deep/  1  0  0 B"} {
		if !strings.Contains(ui, want) {
			t.Fatalf("missing %s: %s", want, ui)
		}
	}
}

func TestIntegrationDiffAttributeOnlyChange(t *testing.T) {
	c := newCLI(t)
	name := filepath.Join(c.source, "metadata.txt")
	writeFile(t, name, []byte("unchanged content"))
	c.ok("backup", c.source)
	first := latestRef(c.refs()).Session
	changed := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	must(t, os.Chtimes(name, changed, changed))
	c.ok("backup", c.source)
	second := latestRef(c.refs()).Session
	oldSession, newSession := c.session(first), c.session(second)
	want := "--- " + first[:8] + "  " + cliTimestamp(t, oldSession.Started) + "  " + oldSession.Directory + "\n+++ " + second[:8] + "  " + cliTimestamp(t, newSession.Started) + "  " + newSession.Directory + "\n~ metadata.txt  17 B\n"
	if out := c.ok("diff", first[:8], second[:8]); !strings.HasPrefix(out, want) || !strings.Contains(out, "modified: ") || !strings.Contains(out, " -> "+cliTimestamp(t, "2025-01-02T03:04:05Z")) || strings.Contains(out, "content: changed") {
		t.Fatalf("got %q, want %q", out, want)
	}
	// The interactive comparison uses exactly the same lines.
	ui := c.interact(testBinary, "\x1b[Bd\x1b[Adq", nil)
	if !strings.Contains(ui, "~ metadata.txt") {
		t.Fatal(ui)
	}
}
