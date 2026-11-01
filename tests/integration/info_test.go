package integration

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestInfoStorageCLI(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("data"))
	c.ok("backup", c.source)
	writeFile(t, filepath.Join(c.storage, "trash", "example"), []byte("trash"))
	out := c.ok("info")
	requireOutput(t, out, "Sessions: 1", "Active objects: 3,", "Trash: 1 files, 5 B", "Total stored:", "working_directory:", "executable:", "config:", "restore_default:", "trash_retention_days: 30", strconv.Quote(c.storage))
	c.fails("usage: info", "info", "extra")
	// A broken config must not prevent help from being displayed.
	bad := filepath.Join(c.root, "broken.conf")
	writeFile(t, bad, []byte("invalid: ["))
	requireOutput(t, c.ok("--config", bad), "Usage:")
	before, err := os.ReadDir(c.storage)
	must(t, err)
	c.ok("info")
	after, err := os.ReadDir(c.storage)
	must(t, err)
	if len(before) != len(after) {
		t.Fatal("info modified storage")
	}
}
