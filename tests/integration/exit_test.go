package integration

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBackupExitCodes(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("contents"))
	c.ok("backup", c.source)
	// A directory at the existing blob's location prevents saving this file.
	for _, id := range c.ids('B') {
		must(t, os.Remove(c.object(id)))
		makeDir(t, c.object(id))
	}
	c.failsCode(1, "session failed", "backup", c.source)
	writeFile(t, filepath.Join(c.source, "good"), []byte("different contents"))
	c.failsCode(1, "session failed", "backup", c.source)
	c.failsCode(1, "missing", "backup", filepath.Join(c.root, "missing"))
}

func TestBackupExitCodesForMultipleSources(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "bad"), []byte("bad"))
	c.ok("backup", c.source)
	for _, id := range c.ids('B') {
		must(t, os.Remove(c.object(id)))
		makeDir(t, c.object(id))
	}
	writeFile(t, filepath.Join(c.source, "good"), []byte("good"))
	clean := filepath.Join(c.root, "clean")
	writeFile(t, filepath.Join(clean, "file"), []byte("clean"))
	missing := filepath.Join(c.root, "missing")
	for _, tc := range []struct {
		dirs []string
		code int
	}{
		{[]string{clean, c.source}, 1},
		{[]string{c.source, missing}, 1},
		{[]string{missing, c.source}, 1},
	} {
		dirs, err := json.Marshal(tc.dirs)
		must(t, err)
		out, err := c.command(map[string]string{"HOBALIGHT_DIRECTORIES": string(dirs)}, "backup")
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != tc.code {
			t.Fatalf("expected %d: %v\n%s", tc.code, err, out)
		}
	}
}
