package integration

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationHashResolutionAndArguments(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("content"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	fake := id[:8] + strings.Repeat("0", 56) + "S"
	if fake == id {
		fake = id[:8] + strings.Repeat("1", 56) + "S"
	}
	writeFile(t, c.object(fake), []byte("fake"))
	c.fails("ambiguous hash", "restore-to", id[:8], ".", filepath.Join(c.root, "ambiguous"))
	c.ok("restore-to", strings.ToUpper(id[:64]), ".", filepath.Join(c.root, "full"))
	for _, tc := range []struct {
		want string
		args []string
	}{
		{"unknown command", []string{"oops"}}, {"accepts one file or directory", []string{"backup", "one", "two"}},
		{"usage: restore", []string{"restore"}}, {"usage: restore", []string{"restore", id[:64], "dest", "--bad", "file"}},
		{"8 to 64", []string{"restore", "abc", "dest"}}, {"invalid hexadecimal", []string{"restore", "zzzzzzzz", "dest"}},
		{"object not found", []string{"restore", strings.Repeat("f", 64), "dest"}},
		{"path not found", []string{"restore-to", id[:64], "missing", "dest"}},
		{"must be relative", []string{"restore-to", id[:64], "../escape", "dest"}},
	} {
		t.Run(tc.want, func(t *testing.T) {
			local := *c
			local.t = t
			local.fails(tc.want, tc.args...)
		})
	}
}

func TestIntegrationDamagedObjects(t *testing.T) {
	for _, scenario := range []string{"missing-blob", "broken-zip", "wrong-content", "wrong-size", "session-checksum", "file-metadata", "invalid-blob-reference"} {
		t.Run(scenario, func(t *testing.T) {
			c := newCLI(t)
			writeFile(t, filepath.Join(c.source, "file"), []byte("original"))
			c.ok("backup", c.source)
			sid := c.refs()[0].Session
			fid := c.session(sid).Files[0]
			var f File
			fp := c.object(fid)
			decodeFile(t, fp, &f)
			blob := c.object(f.Blob)
			want := ""
			switch scenario {
			case "missing-blob":
				must(t, os.Remove(blob))
				want = f.Blob
			case "broken-zip":
				writeFile(t, blob, []byte("not a ZIP"))
				want = "not a valid zip"
			case "wrong-content", "wrong-size":
				var buf bytes.Buffer
				zw := zip.NewWriter(&buf)
				w, err := zw.Create("data")
				must(t, err)
				content := "modified" // Same size as the original: exercise the hash check.
				if scenario == "wrong-size" {
					content = "different"
				}
				_, err = w.Write([]byte(content))
				must(t, err)
				must(t, zw.Close())
				writeFile(t, blob, buf.Bytes())
				want = "blob checksum mismatch"
				if scenario == "wrong-size" {
					want = "blob size does not match file metadata"
				}
			case "session-checksum":
				p := c.object(sid)
				writeFile(t, p, append(readFile(t, p), '\n'))
				want = "session checksum mismatch"
			case "file-metadata":
				f.Path += "changed"
				yamlFile(t, fp, f)
				want = "file metadata checksum mismatch"
			case "invalid-blob-reference":
				f.Blob = "../../escape"
				yamlFile(t, fp, f)
				want = "invalid blob reference"
			}
			dest := filepath.Join(c.root, "restore")
			c.fails(want, "restore-to", sid[:8], ".", dest)
			if _, err := os.Lstat(filepath.Join(dest, "file")); !os.IsNotExist(err) {
				t.Fatalf("failed restore left output: %v", err)
			}
		})
	}
}

func TestIntegrationCorruptCachedMetadata(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("original"))
	c.ok("backup", c.source)
	fid := c.ids('F')[0]
	writeFile(t, c.object(fid), []byte("{}\n"))
	refs := c.refs()
	c.fails("file metadata checksum mismatch", "restore-to", refs[0].Session[:8], ".", filepath.Join(c.root, "damaged"))
	// The next backup rebuilds the damaged metadata from the unchanged source.
	c.ok("backup", c.source)
	refs = c.refs()
	for _, ref := range refs {
		dest := filepath.Join(c.root, "restore-"+ref.Session[:8])
		c.ok("restore-to", ref.Session[:8], ".", dest)
		assertTree(t, dest, map[string][]byte{"file": []byte("original")})
	}
}

func TestIntegrationMissingBlobWithChangedContentPreservesCompanion(t *testing.T) {
	c := newCLI(t)
	name := filepath.Join(c.source, "file")
	writeFile(t, name, []byte("first"))
	c.ok("backup", c.source)
	info, err := os.Stat(name)
	must(t, err)
	must(t, os.Remove(c.object(c.ids('B')[0])))
	writeFile(t, name, []byte("other"))
	must(t, os.Chtimes(name, info.ModTime(), info.ModTime()))
	c.ok("backup", c.source)
	dest := filepath.Join(c.root, "latest")
	c.ok("restore-to", latestRef(c.refs()).Session[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"file": []byte("other")})

}

func TestIntegrationForceRereadsUnchangedMetadata(t *testing.T) {
	c := newCLI(t)
	name := filepath.Join(c.source, "file")
	writeFile(t, name, []byte("first"))
	c.ok("backup", c.source)
	info, err := os.Stat(name)
	must(t, err)
	writeFile(t, name, []byte("other"))
	must(t, os.Chtimes(name, info.ModTime(), info.ModTime()))
	c.ok("backup", c.source)
	if len(c.ids('B')) != 1 {
		t.Fatal("unchanged metadata was reread without --force")
	}
	first := c.refs()[0].Session
	fid := c.session(first).Files[0]
	originalCompanion := readFile(t, c.object(fid))
	c.ok("backup", "--force", c.source)
	if latest := c.session(latestRef(c.refs()).Session); latest.Files[0] != fid {
		t.Fatal("force changed companion ID")
	}
	archived, err := filepath.Glob(filepath.Join(c.storage, "trash", fid+"-*.broken"))
	must(t, err)
	if len(archived) != 1 || !bytes.Equal(readFile(t, archived[0]), originalCompanion) {
		t.Fatal("old companion was not archived intact")
	}
	// Old sessions now resolve the updated companion, as explicitly requested.
	for _, ref := range c.refs() {
		dest := filepath.Join(c.root, "restore-"+ref.Session[:8])
		c.ok("restore-to", ref.Session[:8], ".", dest)
		assertTree(t, dest, map[string][]byte{"file": []byte("other")})
	}
	if len(c.ids('B')) != 2 || len(c.ids('F')) != 1 {
		t.Fatal("original blob lost or companion duplicated")
	}

}

func TestIntegrationForceRepairsDamagedBlob(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("original"))
	c.ok("backup", c.source)
	first := c.refs()[0].Session
	writeFile(t, c.object(c.ids('B')[0]), []byte("damaged"))
	c.ok("backup", c.source)
	c.fails("not a valid zip", "restore-to", first[:8], ".", filepath.Join(c.root, "damaged"))
	c.ok("backup", "--force", c.source)
	for _, ref := range c.refs() {
		dest := filepath.Join(c.root, "restore-"+ref.Session[:8])
		c.ok("restore-to", ref.Session[:8], ".", dest)
		assertTree(t, dest, map[string][]byte{"file": []byte("original")})
	}
}

func TestIntegrationCompanionReuseWithoutPreviousSessions(t *testing.T) {
	c := newCLI(t)
	name := filepath.Join(c.source, "file")
	writeFile(t, name, []byte("first"))
	c.ok("backup", c.source)
	info, err := os.Stat(name)
	must(t, err)
	for _, id := range c.ids('S') {
		must(t, os.Remove(c.object(id)))
	}
	must(t, os.RemoveAll(filepath.Join(c.storage, "sessions")))
	makeDir(t, filepath.Join(c.storage, "sessions"))
	// Corrupt session references must not affect direct companion lookup either.
	badRef := filepath.Join(c.storage, "sessions", "broken.yaml")
	writeFile(t, badRef, []byte("[invalid"))
	writeFile(t, name, []byte("other"))
	must(t, os.Chtimes(name, info.ModTime(), info.ModTime()))
	c.ok("backup", c.source)
	must(t, os.Remove(badRef))
	if len(c.ids('B')) != 1 || len(c.ids('F')) != 1 {
		t.Fatal("source was reread without any previous sessions")
	}
	dest := filepath.Join(c.root, "restore")
	c.ok("restore-to", c.refs()[0].Session[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"file": []byte("first")})
}
