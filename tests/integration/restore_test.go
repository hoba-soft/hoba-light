package integration

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIntegrationForeignPlatformSnapshot(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "nested", "file"), []byte("portable"))
	c.ok("backup", c.source)
	original := c.refs()[0].Session
	s := c.session(original)
	var f File
	decodeFile(t, c.object(s.Files[0]), &f)
	s.Directory = "C:/Users/person/source"
	if runtime.GOOS == "windows" {
		s.Directory = "/home/person/source"
	}
	f.Path = s.Directory + "/nested/file"
	metadata, err := yaml.Marshal(f)
	must(t, err)
	fid := fmt.Sprintf("%xF", sha256.Sum256(metadata))
	writeFile(t, c.object(fid), metadata)
	s.Files = []string{fid}
	data, err := yaml.Marshal(s)
	must(t, err)
	sid := fmt.Sprintf("%xS", sha256.Sum256(data))
	writeFile(t, c.object(sid), data)
	dest := filepath.Join(c.root, "foreign restore")
	c.ok("restore-to", sid[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"nested/file": []byte("portable")})
	c.ok("restore", fid)
	assertTree(t, filepath.Join(c.root, "hobalight_restore", fid[:8]), map[string][]byte{"file": []byte("portable")})
}

func TestIntegrationUnixOnlyNamesDoNotBreakSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot create these file names")
	}
	c := newCLI(t)
	files := map[string][]byte{"plain.txt": []byte("plain"), "nested/meeting 10:30.txt": []byte("colon"), `back\slash`: []byte("backslash")}
	for p, b := range files {
		writeFile(t, filepath.Join(c.source, filepath.FromSlash(p)), b)
	}
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	if out := c.ok("list", id[:8], "nested"); !strings.Contains(out, "meeting 10:30.txt") {
		t.Fatal(out)
	}
	dest := filepath.Join(c.root, "restore")
	c.ok("restore-to", id[:8], ".", dest)
	assertTree(t, dest, files)
	single := filepath.Join(c.root, "single")
	c.ok("restore-to", id[:8], `back\slash`, single)
	assertTree(t, single, map[string][]byte{`back\slash`: []byte("backslash")})
}

func TestIntegrationRestoreContinuesAfterDamagedFile(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "a"), []byte("damaged"))
	writeFile(t, filepath.Join(c.source, "b"), []byte("intact"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	var f File
	decodeFile(t, c.object(c.session(id).Files[0]), &f)
	must(t, os.Remove(c.object(f.Blob)))
	dest := filepath.Join(c.root, "restore")
	c.fails("restore incomplete: 1 files failed", "restore-to", id[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"b": []byte("intact")})
}

func TestUnifiedRestoreLayouts(t *testing.T) {
	for _, scenario := range []string{"session", "directory", "file-path", "file-hash", "session-to", "directory-to", "file-path-to", "file-hash-to"} {
		t.Run(scenario, func(t *testing.T) {
			c := newCLI(t)
			tree := map[string][]byte{"Documents/report.pdf": []byte("report"), "Documents/deep/note.txt": []byte("note"), "Documents-extra/other": []byte("other")}
			for name, data := range tree {
				writeFile(t, filepath.Join(c.source, filepath.FromSlash(name)), data)
			}
			c.ok("backup", c.source)
			session := c.refs()[0].Session
			fileID := ""
			for _, id := range c.session(session).Files {
				var f File
				decodeFile(t, c.object(id), &f)
				if strings.HasSuffix(f.Path, "/Documents/report.pdf") {
					fileID = id
				}
			}
			if fileID == "" {
				t.Fatal("missing file object")
			}
			destination := filepath.Join(c.root, "Recovery with spaces")
			expected := tree
			args := []string{}
			switch scenario {
			case "session":
				args = []string{"restore", session[:8]}
				destination = filepath.Join(c.root, "hobalight_restore", session[:8])
			case "directory":
				args = []string{"restore", session[:8], "Documents/"}
				destination = filepath.Join(c.root, "hobalight_restore", session[:8])
				expected = map[string][]byte{"Documents/report.pdf": []byte("report"), "Documents/deep/note.txt": []byte("note")}
			case "file-path":
				args = []string{"restore", session[:8], "Documents/report.pdf"}
				destination = filepath.Join(c.root, "hobalight_restore", session[:8])
				expected = map[string][]byte{"Documents/report.pdf": []byte("report")}
			case "file-hash":
				args = []string{"restore", fileID}
				destination = filepath.Join(c.root, "hobalight_restore", fileID[:8])
				expected = map[string][]byte{"report.pdf": []byte("report")}
			case "session-to":
				args = []string{"restore-to", session[:8], ".", destination}
			case "directory-to":
				args = []string{"restore-to", session[:8], "Documents", destination}
				expected = map[string][]byte{"Documents/report.pdf": []byte("report"), "Documents/deep/note.txt": []byte("note")}
			case "file-path-to":
				args = []string{"restore-to", session[:8], "Documents/report.pdf", destination}
				expected = map[string][]byte{"Documents/report.pdf": []byte("report")}
			case "file-hash-to":
				args = []string{"restore-to", fileID[:8], destination}
				expected = map[string][]byte{"report.pdf": []byte("report")}
			}
			if out := c.ok(args...); !strings.Contains(out, "Restoring to:") {
				t.Fatal(out)
			}
			assertTree(t, destination, expected)
			c.ok(args...) // Identical files are reusable in both modes.
			assertTree(t, destination, expected)
		})
	}
}

func TestRestoreFileHashWithoutSession(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "nested", "report.pdf"), []byte("report"))
	c.ok("backup", c.source)
	session := c.refs()[0].Session
	fileID := c.session(session).Files[0]
	c.ok("delete", "--force", session)
	c.ok("restore", fileID[:8])
	assertTree(t, filepath.Join(c.root, "hobalight_restore", fileID[:8]), map[string][]byte{"report.pdf": []byte("report")})
	c.fails("does not accept a session path", "restore", fileID, "nested")
	c.fails("usage: restore-to", "restore-to", fileID, "nested", "dest")
}

func TestRestoreChangedFileAndUnsafeSelection(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("original"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	c.ok("restore", id[:8])
	target := filepath.Join(c.root, "hobalight_restore", id[:8], "file")
	writeFile(t, target, []byte("edited"))
	c.fails("exists with different content", "restore", id[:8])
	if got := string(readFile(t, target)); got != "edited" {
		t.Fatal(got)
	}
	for _, selection := range []string{"../escape", "/absolute", "dir/../file"} {
		c.fails("must be relative", "restore", id[:8], selection)
	}
	c.fails("path not found", "restore", id[:8], "missing")
	c.fails("usage: restore-to", "restore-to", id[:8], "dest")
}

func TestRestoreEmptySessionToDefault(t *testing.T) {
	c := newCLI(t)
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	c.ok("restore", id[:8])
	assertTree(t, filepath.Join(c.root, "hobalight_restore", id[:8]), map[string][]byte{})
}
