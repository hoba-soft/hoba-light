package integration

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestIntegrationSnapshotLifecycle(t *testing.T) {
	c := newCLI(t)
	original := map[string][]byte{".hidden": []byte("same"), "nested/space name.txt": []byte("same"), "nested/данные.txt": []byte("unicode"), "empty": {}, "binary": bytes.Repeat([]byte{0, 1, 255, 128}, 1<<18)}
	modified := time.Date(2024, 3, 2, 1, 2, 3, 0, time.UTC)
	for p, b := range original {
		p = filepath.Join(c.source, filepath.FromSlash(p))
		writeFile(t, p, b)
		must(t, os.Chtimes(p, modified, modified))
	}
	c.ok("backup", c.source)
	refs := c.refs()
	if len(refs) != 1 {
		t.Fatal(refs)
	}
	first := refs[0].Session
	if len(c.ids('B')) != 4 || len(c.ids('F')) != 5 {
		t.Fatal("content deduplication or metadata count incorrect")
	}
	session := c.session(first)
	if session.Directory != filepath.ToSlash(c.source) || len(session.Files) != 5 {
		t.Fatalf("unexpected session: %+v", session)
	}
	if !strings.HasSuffix(session.Started, "Z") {
		t.Fatal("session time is not UTC")
	}
	for _, id := range c.ids('S') {
		sum := sha256.Sum256(readFile(t, c.object(id)))
		if fmt.Sprintf("%xS", sum) != id {
			t.Fatal("session ID differs from its content hash")
		}
	}
	c.ok("backup", c.source)
	refs = c.refs()
	second := refs[len(refs)-1].Session
	a, b := append([]string{}, session.Files...), append([]string{}, c.session(second).Files...)
	sort.Strings(a)
	sort.Strings(b)
	if strings.Join(a, ",") != strings.Join(b, ",") {
		t.Fatal("unchanged files absent or new identities created")
	}
	if len(c.ids('B')) != 4 || len(c.ids('F')) != 5 {
		t.Fatal("unchanged backup created duplicate objects")
	}
	logs, err := os.ReadDir(filepath.Join(c.storage, "logs"))
	must(t, err)
	var logText string
	for _, l := range logs {
		logText += string(readFile(t, filepath.Join(c.storage, "logs", l.Name())))
	}
	for _, message := range []string{"reason=unchanged_metadata", "file=", "blob="} {
		if !strings.Contains(logText, message) {
			t.Fatalf("missing log detail %q", message)
		}
	}
	writeFile(t, filepath.Join(c.source, ".hidden"), []byte("changed"))
	must(t, os.Remove(filepath.Join(c.source, "empty")))
	writeFile(t, filepath.Join(c.source, "new"), []byte("added"))
	c.ok("backup", c.source)
	refs = c.refs()
	latest := refs[len(refs)-1].Session
	dest := filepath.Join(c.root, "old snapshot")
	c.ok("restore-to", first[:8], ".", dest)
	assertTree(t, dest, original)
	info, err := os.Stat(filepath.Join(dest, "binary"))
	must(t, err)
	if !info.ModTime().Equal(modified) {
		t.Fatalf("modification time changed: %s", info.ModTime())
	}
	current := map[string][]byte{}
	for p, b := range original {
		if p != "empty" {
			current[p] = b
		}
	}
	current[".hidden"] = []byte("changed")
	current["new"] = []byte("added")
	dest = filepath.Join(c.root, "new snapshot")
	c.ok("restore-to", latest[:64], ".", dest)
	assertTree(t, dest, current)
	single := filepath.Join(c.root, "single")
	selected := "nested/space name.txt"
	if runtime.GOOS == "windows" {
		selected = `nested\space name.txt`
	}
	c.ok("restore-to", first[:9], selected, single)
	assertTree(t, single, map[string][]byte{"nested/space name.txt": []byte("same")})
	c.ok("restore-to", latest[:8], ".", dest)
	assertTree(t, dest, current)
	if out := c.ok("list"); strings.Count(out, " UTC  ") != 3 {
		t.Fatal(out)
	}
}

func TestIntegrationEmptyAndFailedSources(t *testing.T) {
	c := newCLI(t)
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	if len(c.session(id).Files) != 0 {
		t.Fatal("empty snapshot contains files")
	}
	dest := filepath.Join(c.root, "empty restore")
	c.ok("restore-to", id[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{})
	yamlFile(t, filepath.Join(c.root, "hobalight.conf"), Config{Directories: []string{filepath.Join(c.root, "missing"), c.source}})
	c.fails("missing", "--config", filepath.Join(c.root, "hobalight.conf"), "backup")
	if len(c.refs()) != 2 {
		t.Fatal("backup did not continue after failed source")
	}
	c.fails("outside storage", "backup", c.storage)
	file := filepath.Join(c.root, "regular")
	writeFile(t, file, []byte("x"))
	c.fails("must be a directory", "backup", file)
}

func TestIntegrationMissingBlobIsRebuilt(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("original"))
	c.ok("backup", c.source)
	id := c.ids('B')[0]
	must(t, os.Remove(c.object(id)))
	c.ok("backup", c.source)
	refs := c.refs()
	dest := filepath.Join(c.root, "restore")
	c.ok("restore-to", refs[len(refs)-1].Session[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"file": []byte("original")})
}

func TestIntegrationProgress(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "one"), []byte("one"))
	writeFile(t, filepath.Join(c.source, "two"), []byte("two"))
	out := c.ok("backup", c.source)
	if !strings.Contains(out, "Progress: counting files") || !strings.Contains(out, "Progress: 2/2 (100%)") {
		t.Fatal(out)
	}
	out = c.ok("restore-to", c.refs()[0].Session[:8], ".", filepath.Join(c.root, "restore"))
	if !strings.Contains(out, "Restore: 2/2 (100%)") {
		t.Fatal(out)
	}
}

func TestIntegrationBackupPathShortcut(t *testing.T) {
	for _, relative := range []bool{false, true} {
		t.Run(fmt.Sprint("relative=", relative), func(t *testing.T) {
			c := newCLI(t)
			source := filepath.Join(c.root, "documents with spaces данные")
			writeFile(t, filepath.Join(source, "note.txt"), []byte("shortcut backup"))
			path := source
			if relative {
				path = filepath.Base(source)
			}
			c.ok(path)
			refs := c.refs()
			if len(refs) != 1 {
				t.Fatalf("expected one snapshot: %+v", refs)
			}
			dest := filepath.Join(c.root, "restored")
			c.ok("restore-to", refs[0].Session, ".", dest)
			if got := string(readFile(t, filepath.Join(dest, "note.txt"))); got != "shortcut backup" {
				t.Fatal(got)
			}
		})
	}
	c := newCLI(t)
	writeFile(t, filepath.Join(c.root, "list", "file"), []byte("command collision"))
	c.ok("list")
	if len(c.refs()) != 0 {
		t.Fatal("command name was treated as a directory")
	}
	c.ok("./list")
	if len(c.refs()) != 1 {
		t.Fatal("explicit relative path did not create a snapshot")
	}
	if out, err := c.command(nil, "./missing-directory"); err == nil || strings.Contains(out, "unknown command") {
		t.Fatalf("missing path did not report a backup error: %v %s", err, out)
	}
}
