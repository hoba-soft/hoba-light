package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshRecreatesSessionDirectory(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "nested", "file.txt"), []byte("hello"))
	c.ok("backup", c.source)
	refs := c.refs()
	must(t, os.RemoveAll(filepath.Join(c.storage, "sessions")))
	out := c.ok("refresh")
	if !strings.Contains(out, "1 refreshed, 0 unchanged, 0 failed") {
		t.Fatal(out)
	}
	restored := c.refs()
	if len(restored) != 1 || restored[0].Session != refs[0].Session || restored[0].Format != 1 || restored[0].Files == nil {
		t.Fatalf("%+v", restored)
	}
	if out := c.ok("list", refs[0].Session[:8], "nested"); !strings.Contains(out, "file.txt") {
		t.Fatal(out)
	}
	if out := c.ok("refresh"); !strings.Contains(out, "0 refreshed, 1 unchanged") {
		t.Fatal(out)
	}
	c.fails("usage: refresh", "refresh", "extra")
}

func TestRefreshRequiresExistingStore(t *testing.T) {
	c := newCLI(t)
	must(t, os.RemoveAll(c.storage))
	c.fails("not available", "refresh")
	if _, err := os.Stat(c.storage); !os.IsNotExist(err) {
		t.Fatal("refresh initialized storage")
	}
}
