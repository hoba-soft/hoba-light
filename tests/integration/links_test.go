package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIntegrationLinksAndNestedStore(t *testing.T) {
	c := newCLI(t)
	c.storage = filepath.Join(c.source, "store")
	c.ok("list")
	writeFile(t, filepath.Join(c.source, "file"), []byte("contents"))
	if err := os.Symlink("file", filepath.Join(c.source, "file-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	must(t, os.Symlink(".", filepath.Join(c.source, "directory-link")))
	must(t, os.Symlink("missing", filepath.Join(c.source, "broken")))
	c.failsCode(2, "session incomplete", "backup", c.source)
	refs := c.refs()
	if len(refs) != 1 {
		t.Fatal("partial session not published")
	}
	session := c.session(refs[0].Session)
	if len(session.Files) != 3 || len(session.Errors) != 1 {
		t.Fatalf("unexpected session: %+v", session)
	}
	dest := filepath.Join(c.root, "restore")
	c.fails("file was not backed up", "restore-to", refs[0].Session[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"file": []byte("contents"), "file-link": []byte("contents")})
	info, err := os.Lstat(filepath.Join(dest, "file-link"))
	must(t, err)
	if !info.Mode().IsRegular() {
		t.Fatal("restored link is not a regular file")
	}
}

func TestIntegrationRestoreRejectsSymlinkDestination(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "nested", "file"), []byte("backup"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	outside := filepath.Join(c.root, "outside")
	makeDir(t, outside)
	writeFile(t, filepath.Join(outside, "file"), []byte("keep"))
	dest := filepath.Join(c.root, "restore")
	makeDir(t, dest)
	if err := os.Symlink(outside, filepath.Join(dest, "nested")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c.fails("not a real directory", "restore-to", id[:8], ".", dest)
	if string(readFile(t, filepath.Join(outside, "file"))) != "keep" {
		t.Fatal("wrote outside destination")
	}
}

func TestIntegrationUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode bits do not implement Windows access control")
	}
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "good"), []byte("good"))
	denied := filepath.Join(c.source, "denied")
	writeFile(t, denied, []byte("private"))
	must(t, os.Chmod(denied, 0000))
	t.Cleanup(func() { os.Chmod(denied, 0600) })
	if f, err := os.Open(denied); err == nil {
		f.Close()
		t.Skip("current account can bypass file permissions")
	}
	out, backupErr := c.command(nil, "backup", c.source)
	if backupErr == nil || !strings.Contains(out, "1 error") || !strings.Contains(out, "2/2 (100%)") {
		t.Fatalf("%v: %s", backupErr, out)
	}
	refs := c.refs()
	s := c.session(refs[0].Session)
	if len(s.Files) != 2 || len(s.Errors) != 1 {
		t.Fatalf("unexpected partial snapshot: %+v", s)
	}
	dest := filepath.Join(c.root, "restore")
	c.fails("file was not backed up", "restore-to", refs[0].Session[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"good": []byte("good")})
	listing := c.ok("list", refs[0].Session)
	if !strings.Contains(listing, "[error] denied") || !strings.Contains(listing, "7 B") || !strings.Contains(listing, "2 files, 1 errors") {
		t.Fatal(listing)
	}
	detail := c.ok("list", refs[0].Session, "denied")
	if !strings.Contains(detail, "Error:") || !strings.Contains(detail, "permission denied") {
		t.Fatal(detail)
	}
	c.ok("refresh")
	c.ok("gc")
	must(t, os.Chmod(denied, 0600))
	c.ok("backup", c.source)
	if strings.Contains(c.ok("list", latestRef(c.refs()).Session), "[error]") {
		t.Fatal("failed file not retried")
	}

}

func TestIntegrationDirectoryLinkAsSource(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("content"))
	link := filepath.Join(c.root, "linked-source")
	if err := os.Symlink(c.source, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c.fails("backup source must not be a symlink", "backup", link)
	if refs := c.refs(); len(refs) != 0 {
		t.Fatalf("published a misleading empty snapshot: %+v", refs)
	}
}
