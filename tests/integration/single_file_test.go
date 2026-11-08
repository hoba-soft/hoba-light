package integration

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSingleFileBackupLifecycle(t *testing.T) {
	c := newCLI(t)
	name := "данные with spaces.txt"
	file := filepath.Join(c.source, name)
	writeFile(t, file, []byte("original"))
	writeFile(t, filepath.Join(c.source, "sibling"), []byte("must not be included"))
	c.ok("backup", file)
	first := latestRef(c.refs()).Session
	session := c.session(first)
	if len(session.Files) != 1 || session.Directory != filepath.ToSlash(c.source) {
		t.Fatalf("unexpected session: %+v", session)
	}
	c.ok("verify")
	firstDest := filepath.Join(c.root, "first restore")
	c.ok("restore-to", first, ".", firstDest)
	assertTree(t, firstDest, map[string][]byte{name: []byte("original")})
	// Path-only invocation and forced rereading both accept files.
	writeFile(t, file, []byte("new contents"))
	c.ok(file)
	second := latestRef(c.refs()).Session
	c.ok("backup", "--force", file)
	c.ok("refresh")
	c.ok("verify")
	dest := filepath.Join(c.root, "second restore")
	c.ok("restore-to", second, ".", dest)
	assertTree(t, dest, map[string][]byte{name: []byte("new contents")})
	c.ok("history", file)
	c.ok("delete", "--force", first)
	c.ok("gc")
	c.ok("verify")
	dest = filepath.Join(c.root, "after gc")
	c.ok("restore-to", second, ".", dest)
	assertTree(t, dest, map[string][]byte{name: []byte("new contents")})
}

func TestConfiguredSingleFileAndExclusions(t *testing.T) {
	c := newCLI(t)
	file := filepath.Join(c.source, "only.txt")
	writeFile(t, file, []byte("contents"))
	writeFile(t, filepath.Join(c.source, "sibling"), []byte("not selected"))
	sources, err := json.Marshal([]string{file})
	must(t, err)
	out, err := c.command(map[string]string{"HOBALIGHT_DIRECTORIES": string(sources)}, "backup")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(c.session(latestRef(c.refs()).Session).Files) != 1 {
		t.Fatal("configured file not saved")
	}
	c.ok("--exclude", "*.txt", "backup", file)
	if len(c.session(latestRef(c.refs()).Session).Files) != 0 {
		t.Fatal("excluded file was saved")
	}
	c.fails("outside storage", "backup", c.object(c.ids('B')[0]))
}

func TestSingleFileBackupBrowserRestore(t *testing.T) {
	for _, mode := range []string{"session", "file", "restore-to"} {
		t.Run(mode, func(t *testing.T) {
			c := newCLI(t)
			name := "photo данные.txt"
			writeFile(t, filepath.Join(c.source, name), []byte("saved photo"))
			c.ok("backup", filepath.Join(c.source, name))
			id := latestRef(c.refs()).Session
			destination := filepath.Join(c.root, "hobalight_restore", id[:8])
			input := "rq"
			if mode == "file" {
				input = "\rrq"
			}
			if mode == "restore-to" {
				input = "\rtRestored Files\rq"
				destination = filepath.Join(c.root, "Restored Files")
			}
			out := c.interact(testBinary, input, nil)
			requireOutput(t, out, "Restored 1 file")
			assertTree(t, destination, map[string][]byte{name: []byte("saved photo")})
		})
	}
}
