package integration

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetentionGCCLI(t *testing.T) {
	c := newCLI(t)
	file := filepath.Join(c.source, "file")
	writeFile(t, file, []byte("old"))
	c.ok("backup", c.source)
	old := retimeSession(t, c, latestRef(c.refs()).Session, "2020-01-01T12:00:00Z")
	writeFile(t, file, []byte("new contents"))
	c.ok("backup", c.source)
	newest := latestRef(c.refs()).Session
	config := filepath.Join(c.root, "retention.conf")
	writeFile(t, config, []byte("retention:\n  days: 30\n  sessions: 1\n"))
	out := c.ok("--config", config, "gc", "--dry-run")
	requireOutput(t, out, "Would delete session "+old, "retention")
	if len(c.refs()) != 2 {
		t.Fatal("dry-run removed sessions")
	}
	c.ok("--config", config, "backup", c.source)
	if len(c.refs()) != 3 {
		t.Fatal("backup applied retention")
	}
	c.ok("--config", config, "gc")
	if len(c.refs()) != 2 {
		t.Fatal("union retention did not preserve both recent sessions")
	}
	c.ok("verify")
	c.ok("restore-to", newest, ".", filepath.Join(c.root, "restored"))
	assertTree(t, filepath.Join(c.root, "restored"), map[string][]byte{"file": []byte("new contents")})
	for _, bad := range []string{"days: -1", "sessions: -1", "days: 1.5", "sesions: 2"} {
		writeFile(t, config, []byte(fmt.Sprintf("retention:\n  %s\n", bad)))
		out, err := c.command(nil, "--config", config, "gc")
		if err == nil || !strings.Contains(out, "retention") {
			t.Fatalf("invalid policy accepted: %v %s", err, out)
		}
	}
}
