package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileIDAt(t *testing.T, c *cliTest, session, suffix string) string {
	t.Helper()
	for _, id := range c.session(session).Files {
		var f File
		decodeFile(t, c.object(id), &f)
		if strings.HasSuffix(f.Path, suffix) {
			return id
		}
	}
	t.Fatal("file not found", suffix)
	return ""
}

func TestDeleteConfirmationModes(t *testing.T) {
	for _, mode := range []string{"session", "path", "file"} {
		t.Run(mode, func(t *testing.T) {
			c := newCLI(t)
			writeFile(t, filepath.Join(c.source, "nested", "file.txt"), []byte("data"))
			c.ok("backup", c.source)
			sid := c.refs()[0].Session
			target := []string{"delete", sid}
			if mode == "path" {
				target = append(target, "nested")
			}
			if mode == "file" {
				target[1] = fileIDAt(t, c, sid, "/file.txt")
			}
			for _, answer := range []string{"n\n", "\n", "anything\n"} {
				out, err := c.commandInput(nil, answer, target...)
				must(t, err)
				requireOutput(t, out, "Confirm deletion? [y/N]", "Deletion cancelled", sid)
				if mode == "path" {
					requireOutput(t, out, "nested", "1 file", "hash will change")
				}
				if mode == "file" {
					requireOutput(t, out, "Affected sessions: 1", target[1], "file.txt")
				}
				if c.refs()[0].Session != sid {
					t.Fatal("cancelled deletion mutated session")
				}
			}
			out, err := c.command(nil, target...)
			if err == nil {
				t.Fatal("EOF accepted")
			}
			requireOutput(t, out, "--force")
			out, err = c.commandInput(nil, "y\n", target...)
			must(t, err)
			if mode == "session" {
				requireOutput(t, out, "Deleted session")
				if len(c.refs()) != 0 {
					t.Fatal("session remains")
				}
			} else {
				requireOutput(t, out, "Updated session")
				if len(c.session(c.refs()[0].Session).Files) != 0 {
					t.Fatal("file remains")
				}
			}
		})
	}
}

func TestDeleteFileAcrossAllSessionObjects(t *testing.T) {
	c := newCLI(t)
	name := filepath.Join(c.source, "target.txt")
	writeFile(t, name, []byte("shared bytes"))
	writeFile(t, filepath.Join(c.source, "other.txt"), []byte("shared bytes"))
	c.ok("backup", c.source)
	first := latestRef(c.refs()).Session
	fid := fileIDAt(t, c, first, "/target.txt")
	c.ok("backup", c.source)
	second := latestRef(c.refs()).Session
	writeFile(t, name, []byte("new version content"))
	c.ok("backup", c.source)
	third := latestRef(c.refs()).Session
	newer := fileIDAt(t, c, third, "/target.txt")
	if newer == fid {
		t.Fatal("fixture has no new version")
	}
	entries, err := os.ReadDir(filepath.Join(c.storage, "sessions"))
	must(t, err)
	for _, entry := range entries {
		p := filepath.Join(c.storage, "sessions", entry.Name())
		var ref Reference
		decodeFile(t, p, &ref)
		if ref.Session == first {
			must(t, os.Remove(p))
		}
	}
	out := c.ok("delete", "--force", fid[:8])
	requireOutput(t, out, "Affected sessions: 2", first, second, "from 2 sessions")
	if strings.Contains(out, "Confirm deletion?") {
		t.Fatal("force requested confirmation")
	}
	if len(c.refs()) != 3 {
		t.Fatal("missing reconstructed reference")
	}
	for _, id := range c.ids('S') {
		for _, file := range c.session(id).Files {
			if file == fid {
				t.Fatal("file reference remains")
			}
		}
	}
	if _, err := os.Stat(c.object(newer)); err != nil {
		t.Fatal("new version lost")
	}
	if _, err := os.Stat(c.object(fid)); err != nil {
		t.Fatal("file collected before gc")
	}
	c.ok("gc")
	if _, err := os.Stat(c.object(fid)); !os.IsNotExist(err) {
		t.Fatal("orphan not collected")
	}
	c.ok("restore-to", third, ".", filepath.Join(c.root, "restore"))
	assertTree(t, filepath.Join(c.root, "restore"), map[string][]byte{"target.txt": []byte("new version content"), "other.txt": []byte("shared bytes")})
	c.ok("refresh")
	if len(c.refs()) != 3 {
		t.Fatal("old roots resurrected")
	}
}

func TestBrowserGlobalFileDeletion(t *testing.T) {
	for _, navigation := range []string{"\r", "\r\r", "ntarget\r"} {
		t.Run(navigation, func(t *testing.T) {
			c := newCLI(t)
			writeFile(t, filepath.Join(c.source, "target.txt"), []byte("data"))
			c.ok("backup", c.source)
			c.ok("backup", c.source)
			old := c.refs()[0].Session
			fid := fileIDAt(t, c, old, "/target.txt")
			for _, cancel := range []string{"nq", "\rq", "q", ""} {
				out := c.interact(testBinary, navigation+"X"+cancel, nil)
				requireOutput(t, out, "Delete file from all sessions?", "Affected sessions: 2")
				if len(c.session(old).Files) != 1 {
					t.Fatal("cancelled global deletion changed session")
				}
			}
			out := c.interact(testBinary, navigation+"X\x1b[B\x1b[Ayq", nil)
			requireOutput(t, out, "Removed file "+fid[:8]+" from 2 sessions")
			for _, ref := range c.refs() {
				if len(c.session(ref.Session).Files) != 0 {
					t.Fatal("global deletion left references")
				}
			}
		})
	}
}

func TestDeleteFileRejectsAmbiguityAndInvalidTargets(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("data"))
	c.ok("backup", c.source)
	sid := c.refs()[0].Session
	fid := c.session(sid).Files[0]
	fake := fid[:8] + strings.Repeat("0", 56) + "S"
	writeFile(t, c.object(fake), []byte("invalid session"))
	c.fails("ambiguous hash", "delete", "--force", fid[:8])
	c.fails("session checksum mismatch", "delete", "--force", fid)
	must(t, os.Remove(c.object(fake)))
	c.fails("a path can only", "delete", "--force", fid, "path")
	c.fails("unknown delete option", "delete", "--forece", sid)
	c.ok("delete", "--force", sid)
	requireOutput(t, c.ok("delete", "--force", fid), "No sessions reference this file")
}

func TestDeleteFailedFileGlobally(t *testing.T) {
	c, _, failed, fid, obstruction := failedSnapshot(t)
	// Remove the deliberate invalid directory so object inventory validation succeeds.
	must(t, os.RemoveAll(obstruction))
	requireOutput(t, c.ok("delete", "--force", fid), "from 1 sessions")
	if _, err := os.Stat(c.object(failed)); !os.IsNotExist(err) {
		t.Fatal("failed session not replaced")
	}
	for _, ref := range c.refs() {
		session := c.session(ref.Session)
		if len(session.Files) == 2 && len(session.Errors) != 0 {
			t.Fatal("deleted error diagnostic remains")
		}
		for _, file := range session.Files {
			if file == fid {
				t.Fatal("failed file remains")
			}
		}
	}
}
