package integration

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestParallelBackupDuplicateContentForceAndReuse(t *testing.T) {
	c := newCLI(t)
	tree := map[string][]byte{}
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("nested/file-%02d", i)
		tree[name] = []byte(fmt.Sprintf("content-%d", i%3))
		writeFile(t, filepath.Join(c.source, filepath.FromSlash(name)), tree[name])
	}
	var previous []string
	for _, force := range []bool{false, false, true} {
		args := []string{"backup", c.source}
		if force {
			args = append(args, "--force")
		}
		output, err := c.command(map[string]string{"HOBALIGHT_WORKERS": "8"}, args...)
		if err != nil {
			t.Fatalf("%v %s", err, output)
		}
		id := latestRef(c.refs()).Session
		session := c.session(id)
		if len(session.Files) != len(tree) {
			t.Fatalf("missing files: %d", len(session.Files))
		}
		if previous != nil {
			for i, file := range session.Files {
				if previous[i] != file {
					t.Fatal("session order changed")
				}
			}
		}
		previous = session.Files
	}
	destination := filepath.Join(c.root, "restored")
	c.ok("restore-to", latestRef(c.refs()).Session[:8], ".", destination)
	assertTree(t, destination, tree)
}

func TestIntegrationBackupShowsSourceAndFile(t *testing.T) {
	c := newCLI(t)
	other := filepath.Join(c.root, "other")
	writeFile(t, filepath.Join(c.source, "one.txt"), []byte("one"))
	writeFile(t, filepath.Join(other, "two.txt"), []byte("two"))
	config := filepath.Join(c.root, "progress.conf")
	yamlFile(t, config, Config{Directories: []string{c.source, other}, Workers: 2})
	out := c.ok("--config", config, "backup")
	if !strings.Contains(out, "\n\nBacking up: "+filepath.ToSlash(other)) {
		t.Fatal("missing blank line between sources: " + out)
	}
	for _, source := range []string{c.source, other} {
		if !strings.Contains(out, "Backing up: "+filepath.ToSlash(source)) {
			t.Fatal(out)
		}
	}
	for _, want := range []string{"one.txt", "two.txt"} {
		if !regexp.MustCompile(`Progress: 0/1 \(0%\)  0 B added \[\d{2}:\d{2}:\d{2}\]  `+regexp.QuoteMeta(want)).MatchString(out) || !regexp.MustCompile(`Progress: 1/1 \(100%\)  [^\r\n]+ added \[\d{2}:\d{2}:\d{2}\]\r?\n`).MatchString(out) {
			t.Fatal(out)
		}
	}
}
