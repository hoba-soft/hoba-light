package integration

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIntegrationForceReportsBlobPublishFailure(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("original"))
	c.ok("backup", c.source)
	blob := c.object(c.ids('B')[0])
	must(t, os.Remove(blob))
	makeDir(t, blob)
	// A nonempty directory cannot be replaced by the temporary ZIP on either OS.
	writeFile(t, filepath.Join(blob, "obstruction"), []byte("keep"))
	c.fails("session failed", "backup", "--force", c.source)
	refs := c.refs()
	session := c.session(latestRef(refs).Session)
	if len(session.Files) != 1 || len(session.Errors) != 1 || !strings.Contains(session.Errors[0], "publish blob") {
		t.Fatalf("failed blob publication reported success: %+v", session)
	}
	if string(readFile(t, filepath.Join(blob, "obstruction"))) != "keep" {
		t.Fatal("modified existing directory")
	}
}

func TestIntegrationRestoreSkipsMalformedReferences(t *testing.T) {
	for _, bad := range []string{"", "x", "1234567"} {
		t.Run(fmt.Sprintf("length-%d", len(bad)), func(t *testing.T) {
			c := newCLI(t)
			writeFile(t, filepath.Join(c.source, "intact"), []byte("recover me"))
			c.ok("backup", c.source)
			session := c.session(c.refs()[0].Session)
			session.Files = append([]string{bad}, session.Files...)
			data, err := yaml.Marshal(session)
			must(t, err)
			id := fmt.Sprintf("%xS", sha256.Sum256(data))
			writeFile(t, c.object(id), data)
			dest := filepath.Join(c.root, "restore")
			c.fails("restore incomplete: 1 files failed", "restore-to", id[:64], ".", dest)
			assertTree(t, dest, map[string][]byte{"intact": []byte("recover me")})
		})
	}
}

func TestIntegrationRestoreRejectsAbsoluteSelection(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("keep"))
	c.ok("backup", c.source)
	id := c.refs()[0].Session
	for i, selected := range []string{"/", "//", "/absolute/"} {
		dest := filepath.Join(c.root, fmt.Sprintf("restore-%d", i))
		c.fails("must be relative", "restore-to", id[:8], selected, dest)
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatalf("invalid selection created destination: %v", err)
		}
	}
}
