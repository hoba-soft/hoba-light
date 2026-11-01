package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCLI(t *testing.T) {
	c := newCLI(t)
	requireOutput(t, c.ok("verify"), "0 sessions, 0 file objects, 0 blobs", "0 integrity issues")
	writeFile(t, filepath.Join(c.source, "file.txt"), []byte("payload"))
	c.ok("backup", c.source)
	first := latestRef(c.refs()).Session
	c.ok("backup", c.source)
	second := latestRef(c.refs()).Session
	for _, args := range [][]string{{"verify"}, {"verify", first[:8]}} {
		requireOutput(t, c.ok(args...), "0 integrity issues", "1 file objects, 1 blobs")
	}
	must(t, os.RemoveAll(c.source))
	must(t, os.RemoveAll(filepath.Join(c.storage, "sessions")))
	requireOutput(t, c.ok("verify", first), "OK")
	fid := c.session(first).Files[0]
	var f File
	decodeFile(t, c.object(fid), &f)
	must(t, os.WriteFile(c.object(f.Blob), []byte("broken zip"), 0600))
	out, err := c.command(nil, "verify")
	if err == nil {
		t.Fatal("broken storage passed")
	}
	requireOutput(t, out, first[:8]+" DAMAGED", second[:8]+" DAMAGED", f.Blob, "2 damaged")
	c.fails("usage: verify", "verify", first, second)
	c.fails("object not found", "verify", fid[:8])
	c.fails("object not found", "verify", "ffffffff")
	if _, err = os.Stat(c.object(f.Blob)); err != nil {
		t.Fatal("verify removed corrupt blob")
	}
	if _, err = os.Stat(filepath.Join(c.storage, ".gc.lock")); !os.IsNotExist(err) {
		t.Fatal("lock remains")
	}
}
func TestVerifyScopedAndIncomplete(t *testing.T) {
	c, before, failed, _, obstruction := failedSnapshot(t)
	must(t, os.RemoveAll(obstruction))
	// A failed F is valid metadata, but does not represent restorable content.
	out, err := c.command(nil, "verify", failed)
	if err == nil {
		t.Fatal("incomplete session passed")
	}
	requireOutput(t, out, "INCOMPLETE", "0 integrity issues", "Backup error")
	// The older session points at the now missing original blob and is damaged.
	out, err = c.command(nil, "verify", before)
	if err == nil {
		t.Fatal("missing content passed")
	}
	requireOutput(t, out, "DAMAGED")
	// Unrelated malformed objects do not affect scoped verification.
	id := strings.Repeat("a", 64) + "B"
	writeFile(t, c.object(id), []byte("bad"))
	out, err = c.command(nil, "verify", failed)
	if err == nil {
		t.Fatal("incomplete session passed")
	}
	if strings.Contains(out, id) {
		t.Fatal("scope leaked")
	}
}

func TestVerifyRepairCLI(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file.txt"), []byte("recover me"))
	c.ok("backup", c.source)
	sid := latestRef(c.refs()).Session
	fid := c.session(sid).Files[0]
	var f File
	decodeFile(t, c.object(fid), &f)
	must(t, os.Remove(c.object(f.Blob)))
	out, err := c.command(nil, "verify", sid)
	if err == nil {
		t.Fatal("missing blob passed")
	}
	requireOutput(t, out, "from original file", "Recoverable: 1 objects")
	if _, err = os.Stat(c.object(f.Blob)); !os.IsNotExist(err) {
		t.Fatal("dry run changed storage")
	}
	requireOutput(t, c.ok("verify", "--repair", sid), "Repaired "+f.Blob, "Rechecking", "0 integrity issues")
	c.ok("delete", "--force", sid)
	c.ok("verify", "--repair")
	if _, err = os.Stat(c.object(sid)); !os.IsNotExist(err) {
		t.Fatal("deleted session unexpectedly restored")
	}
	requireOutput(t, c.ok("verify", sid, "--repair"), "Repaired "+sid, "0 integrity issues")
	requireOutput(t, c.ok("list"), sid[:8])
	c.fails("usage: verify", "verify", "--force")
}
