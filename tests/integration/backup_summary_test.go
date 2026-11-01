package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupSummaryCLI(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "a"), []byte("alpha"))
	out := c.ok("backup", c.source)
	requireOutput(t, out, "First session:", "First session: 1 new", "added [", "Backup finished:")
	out = c.ok("backup", c.source)
	requireOutput(t, out, "0 new, 0 modified, 1 unchanged", "0 B added")
	writeFile(t, filepath.Join(c.source, "b"), []byte("alpha"))
	writeFile(t, filepath.Join(c.source, "a"), []byte("changed alpha"))
	out = c.ok("backup", c.source)
	requireOutput(t, out, "1 new, 1 modified, 0 unchanged", "added [")
	out = c.ok("backup", "--force", c.source)
	requireOutput(t, out, "0 new, 0 modified, 2 unchanged", "0 B added")
	if strings.Contains(out, "Blob") || strings.Contains(out, "Saved file data") {
		t.Fatal(out)
	}
}
func TestBackupSummaryMultipleDirectoriesAndErrors(t *testing.T) {
	c := newCLI(t)
	other := filepath.Join(c.root, "other")
	writeFile(t, filepath.Join(other, "file"), []byte("x"))
	config := filepath.Join(c.root, "multi.conf")
	yamlFile(t, config, Config{Storage: c.storage, Directories: []string{c.source, other}})
	out := c.ok("--config", config, "backup")
	requireOutput(t, out, "First session: 0 new", "First session: 1 new")
	if strings.Count(out, "Backup finished:") != 2 {
		t.Fatal(out)
	}
	bad, _, _, _, obstruction := failedSnapshot(t)
	out, err := bad.command(nil, "backup", "--force", bad.source)
	if err == nil {
		t.Fatal("failed backup passed")
	}
	requireOutput(t, out, "1 error", "Backup finished with errors:")
	must(t, os.RemoveAll(obstruction))
}
