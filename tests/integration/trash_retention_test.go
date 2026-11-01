package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGCTrashRetentionCLI(t *testing.T) {
	c := newCLI(t)
	c.ok("backup", c.source)
	name := strings.Repeat("a", 64) + "B-trashed-" + time.Now().UTC().AddDate(0, 0, -10).Format("20060102T150405Z") + "-123.orphan"
	target := filepath.Join(c.storage, "trash", name)
	writeFile(t, target, []byte("unused"))
	requireOutput(t, c.ok("gc"), "Trash: 0 expired objects deleted")
	config := filepath.Join(c.root, "retention.conf")
	writeFile(t, config, []byte("storage: "+filepath.ToSlash(c.storage)+"\ntrash_retention_days: 5\n"))
	requireOutput(t, c.ok("--config", config, "gc"), "Trash: 1 expired objects deleted")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	writeFile(t, target, []byte("unused"))
	out, err := c.command(map[string]string{"HOBALIGHT_TRASH_RETENTION_DAYS": "20"}, "--config", config, "gc")
	must(t, err)
	requireOutput(t, out, "Trash: 0 expired objects deleted")
	for _, value := range []string{"0", "-1", "no", "365001"} {
		_, err := c.command(map[string]string{"HOBALIGHT_TRASH_RETENTION_DAYS": value}, "gc")
		if err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
}

func TestGCDryRunCLI(t *testing.T) {
	c := newCLI(t)
	c.ok("backup", c.source)
	id := strings.Repeat("a", 64) + "B"
	orphan := c.object(id)
	writeFile(t, orphan, []byte("unused"))
	old := filepath.Join(c.storage, "trash", id+"-trashed-20000101T000000Z-123.orphan")
	legacy := filepath.Join(c.storage, "trash", id+"-123.broken")
	writeFile(t, old, []byte("old"))
	writeFile(t, legacy, []byte("legacy"))
	snapshot := func() map[string]string {
		result := map[string]string{}
		must(t, filepath.Walk(c.storage, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				result[p] = string(readFile(t, p))
			}
			return nil
		}))
		return result
	}
	before := snapshot()
	quiet := c.ok("gc", "--dry-run")
	requireOutput(t, quiet, "1 would move to trash (6 B)", "1 expired objects would be deleted (3 B)")
	for _, hidden := range []string{id, "Would trash", "Would delete", "Would start"} {
		if strings.Contains(quiet, hidden) {
			t.Fatal(quiet)
		}
	}
	out := c.ok("gc", "--dry-run", "--verbose")
	requireOutput(t, out, "Would trash "+id, "Would delete trash/", "Would start trash retention:", "1 would move to trash", "1 expired objects would be deleted")
	after := snapshot()
	if len(before) != len(after) {
		t.Fatal("dry run changed files")
	}
	for p, data := range before {
		if got, ok := after[p]; !ok || got != data {
			t.Fatalf("dry run changed %s", p)
		}
	}
	c.fails("usage: gc", "gc", "--unknown")
	c.ok("gc")
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
