package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionComments(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("contents"))
	comment := "Before upgrade — отпуск"
	c.ok("backup", "--comment", comment, c.source)
	id := latestRef(c.refs()).Session
	for _, args := range [][]string{{"list"}, {"list", id}, {"find", "UPGRADE"}, {"find", id, "ОТПУСК"}} {
		requireOutput(t, c.ok(args...), comment)
	}
	requireOutput(t, c.interact(testBinary, "q", nil), comment)
	out := c.interact(testBinary, "/UPGRADE\rrq", nil)
	requireOutput(t, out, comment, "Restored 1 file")
	requireOutput(t, c.interact(testBinary, "/UPGRADE\r\rq", nil), comment)
	assertTree(t, filepath.Join(c.root, "hobalight_restore", id[:8]), map[string][]byte{"file": []byte("contents")})
	c.ok("refresh")
	requireOutput(t, c.ok("list"), comment)
	requireOutput(t, c.ok("find", "upgrade"), comment)
	c.ok("verify")
	// Deleting a selected file creates a replacement session, preserving its label.
	c.ok("delete", "--force", id, "file")
	requireOutput(t, c.ok("list"), comment)
	out = c.ok("find", "upgrade")
	if strings.Contains(out, "No matching") {
		t.Fatal("empty labelled session not found")
	}
	requireOutput(t, c.interact(testBinary, "/upgrade\r\rq", nil), comment)
}

func TestCommentOptionParsing(t *testing.T) {
	c := newCLI(t)
	c.fails("--comment requires text", "backup", "--comment")
	c.ok("backup", "--comment=Empty source", c.source)
	requireOutput(t, c.ok("find", "EMPTY SOURCE"), "Empty source")
	c.ok("backup", "--comment", "--force", c.source)
	requireOutput(t, c.ok("list"), "--force")
}

func TestShortCommentBackup(t *testing.T) {
	for _, single := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "file"}[single], func(t *testing.T) {
			c := newCLI(t)
			file := filepath.Join(c.source, "file.txt")
			writeFile(t, file, []byte("contents"))
			source := c.source
			if single {
				source = file
			}
			for _, args := range [][]string{
				{"-c", "Short comment", source},
				{"--comment", "Long comment", source},
				{"backup", "-c", "Backup comment", source},
			} {
				c.ok(args...)
			}
			listing := c.ok("list")
			requireOutput(t, listing, "Short comment", "Long comment", "Backup comment")
			c.fails("requires text", "backup", "-c")
		})
	}
}
