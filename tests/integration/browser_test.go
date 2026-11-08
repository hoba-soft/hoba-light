package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Most browser scenarios start inside the first source, at its session list.
func (c *cliTest) interact(binary, input string, extra map[string]string) string {
	c.t.Helper()
	return c.interactSources(binary, "\r"+input, extra)
}

func (c *cliTest) interactSources(binary, input string, extra map[string]string) string {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "browse")
	cmd.Dir = c.root
	cmd.Stdin = strings.NewReader(input)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(env), "HOBALIGHT_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "TZ=UTC", "HOBALIGHT_STORAGE="+c.storage, "HOME="+filepath.Join(c.root, "home"), "USERPROFILE="+filepath.Join(c.root, "home"))
	for k, v := range extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.t.Fatalf("interactive: %v\n%s", err, out)
	}
	return string(out)
}
func TestIntegrationBrowserRenamedExecutable(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "nested", "file.txt"), []byte("restored data"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	name := "renamed-backup"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(c.root, name)
	writeFile(t, binary, readFile(t, testBinary))
	must(t, os.Chmod(binary, 0755))
	out := c.interact(binary, "\x1b[C\x1b[C\x1b[Crr\x1b[D\x1b[Dr\x1b[Dq", nil)
	restored := filepath.Join(c.root, "hobalight_restore", id[:8], "nested", "file.txt")
	if string(readFile(t, restored)) != "restored data" {
		t.Fatal("wrong restored content")
	}
	for _, want := range []string{"Sessions", "file.txt", "Session ", "Restored ", "nested/"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "Error:") {
		t.Fatal(out)
	}
}
func TestIntegrationBrowserEmptyEOF(t *testing.T) {
	c := newCLI(t)
	out := c.interact(testBinary, "", nil)
	if !strings.Contains(out, "No backup sessions") {
		t.Fatal(out)
	}
}
func TestIntegrationBrowserSystemOpener(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses native ShellExecute; the browser opener is tested with a unit-test substitute")
	}
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file.txt"), []byte("open me"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	binDir := filepath.Join(c.root, "bin")
	makeDir(t, binDir)
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	launcher := filepath.Join(binDir, name)
	writeFile(t, launcher, []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$HOBALIGHT_OPEN_LOG\"\n"))
	must(t, os.Chmod(launcher, 0755))
	binary := filepath.Join(c.root, "viewer-backup")
	writeFile(t, binary, readFile(t, testBinary))
	must(t, os.Chmod(binary, 0755))
	log := filepath.Join(c.root, "opened.txt")
	out := c.interact(binary, "\x1b[Coq", map[string]string{"PATH": binDir + string(os.PathListSeparator) + os.Getenv("PATH"), "HOBALIGHT_OPEN_LOG": log})
	target := filepath.Join(c.root, "hobalight_restore", id[:8], "file.txt")
	target, err := filepath.EvalSymlinks(target)
	must(t, err)
	if string(readFile(t, log)) != target || !strings.Contains(out, "Opened ") {
		t.Fatal(out)
	}
}

func TestBrowserRestoreToTextAndCancellation(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "nested", "file.txt"), []byte("data"))
	c.ok("backup", c.source)
	for _, input := range []string{"t\rq", "t\x1bq", "tUnsubmitted"} {
		c.interact(testBinary, input, nil)
		if _, err := os.Stat(filepath.Join(c.root, "hobalight_restore")); !os.IsNotExist(err) {
			t.Fatal("cancelled restore created output")
		}
	}
	// Literal q/r/t, upper case, spaces, Unicode and backspace belong to the path,
	// not to navigation commands. Pick the directory within the session.
	destination := "Q Recovery Кириллица"
	out := c.interact(testBinary, "\x1b[Ctwrong\x15"+destination+"x\x7f\rq", nil)
	if !strings.Contains(out, "Restored 1 file") {
		t.Fatal(out)
	}
	assertTree(t, filepath.Join(c.root, destination), map[string][]byte{"nested/file.txt": []byte("data")})
	if _, err := os.Stat(filepath.Join(c.root, "hobalight_restore")); !os.IsNotExist(err) {
		t.Fatal("restore-to added default root")
	}
}

func TestBrowserSearchScopesAndNavigation(t *testing.T) {
	c := newCLI(t)
	for _, name := range []string{"nested/договор.txt", "договор.txt", "nested/deep/note.txt"} {
		writeFile(t, filepath.Join(c.source, filepath.FromSlash(name)), []byte(name))
	}
	c.ok("backup", c.source)
	c.ok("backup", c.source)
	for _, tc := range []struct {
		name, input string
		want        []string
	}{
		{"source sessions", "nдоговор\rq", []string{"All sessions"}},
		{"session", "\rnдоговор\rq", []string{"договор.txt"}},
		{"directory", "\r\rnдоговор\rq", []string{"/nested", "договор.txt"}},
		{"empty results", "nabsent\r\x1b[Dq", []string{"No matching files or directories."}},
		{"edit input", "nwrong\x15договорx\x7f\r\x1b[B\x1b[A\x1b[F\x1b[H\x1b[6~\x1b[5~\rq", []string{"Find: договор", "Located"}},
		{"cancel input", "nunsubmitted\x1bq", []string{"Started"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := *c
			local.t = t
			requireOutput(t, local.interact(testBinary, tc.input, nil), tc.want...)
		})
	}
	out := c.interact(testBinary, "nnote\rtsearch-restore\rq", nil)
	requireOutput(t, out, "Restored 1 file")
	assertTree(t, filepath.Join(c.root, "search-restore"), map[string][]byte{"nested/deep/note.txt": []byte("nested/deep/note.txt")})
}

func TestBrowserNavigationAndRefresh(t *testing.T) {
	c := newCLI(t)
	for i := 0; i < 35; i++ {
		writeFile(t, filepath.Join(c.source, fmt.Sprintf("file-%02d.txt", i)), []byte("data"))
	}
	c.ok("backup", c.source)
	out := c.interact(testBinary, "f\r\x1b[F\r\x1b[D\x1b[H\x1b[6~\x1b[5~\x1b[Dq", nil)
	requireOutput(t, out, "file-34.txt", "File object:", "Path:")
	if strings.Contains(out, "Error:") {
		t.Fatal(out)
	}
}

func TestIntegrationBrowserCompactHelp(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file.txt"), []byte("content"))
	c.ok("backup", c.source)
	out := c.interact(testBinary, "\r\r?\x1b[Dq", nil)
	requireOutput(t, out, "Keyboard shortcuts", "X delete file globally", "? help", "Path:")
	for _, unwanted := range []string{"Content object:", "Name: file.txt", "Hoba Light Browser ·"} {
		if strings.Contains(out, unwanted) {
			t.Fatal(out)
		}
	}
}

func TestBrowserFilterListsAndRestoreSelectedFile(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "alpha", "first.txt"), []byte("first"))
	writeFile(t, filepath.Join(c.source, "beta", "second.txt"), []byte("second"))
	c.ok("backup", "-c", "Before upgrade", c.source)
	c.ok("backup", "-c", "After upgrade", c.source)
	out := c.interact(testBinary, "/AFTER\r\rq", nil)
	requireOutput(t, out, "After upgrade", "Session ")
	// Select beta after filtering the session's entries, then restore its file
	// after a second filter. The selected item must map to the unfiltered list.
	out = c.interact(testBinary, "\r/beta\r\r/SECOND\rtfiltered-restore\rq", nil)
	requireOutput(t, out, "Restored 1 file")
	assertTree(t, filepath.Join(c.root, "filtered-restore"), map[string][]byte{"beta/second.txt": []byte("second")})
	// Esc cancels editing; a second Left clears a committed filter.
	out = c.interact(testBinary, "/missing\r\x1b[D/unfinished\x1bq", nil)
	requireOutput(t, out, "Before upgrade", "After upgrade")
}
