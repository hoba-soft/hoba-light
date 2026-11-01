package hobalight

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRepairFromTrash(t *testing.T) {
	s, sid, fid, bid := verifyFixture(t)
	var file File
	must(t, s.read(fid, 'F', &file))
	must(t, os.Remove(file.Path))
	must(t, s.trashObject(fid, "orphan"))
	must(t, s.trashObject(bid, "orphan"))
	must(t, os.WriteFile(s.object(bid), []byte("damaged active blob"), 0600))
	var out strings.Builder
	if err := s.verify([]string{sid}, &out); err == nil {
		t.Fatal("dry run hid damage")
	}
	if !strings.Contains(out.String(), "Recoverable: 2 objects") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(s.object(fid)); !os.IsNotExist(err) {
		t.Fatal("dry run wrote metadata")
	}
	data, err := os.ReadFile(s.object(bid))
	must(t, err)
	if string(data) != "damaged active blob" {
		t.Fatal("dry run changed blob")
	}
	out.Reset()
	must(t, s.verify([]string{"--repair", sid}, &out))
	if !strings.Contains(out.String(), "Repaired "+fid) || !strings.Contains(out.String(), "0 integrity issues") {
		t.Fatal(out.String())
	}
	must(t, s.verify([]string{sid}, io.Discard))
	entries, err := os.ReadDir(filepath.Join(s.backend.LocalRoot(), "trash"))
	must(t, err)
	preserved := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".broken") {
			data, e := os.ReadFile(filepath.Join(s.backend.LocalRoot(), "trash", entry.Name()))
			must(t, e)
			preserved = preserved || bytes.Equal(data, []byte("damaged active blob"))
		}
	}
	if !preserved {
		t.Fatal("old bytes not preserved")
	}
}
func TestVerifyRepairLocalExactMatch(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same", true: "changed"}[changed], func(t *testing.T) {
			s, sid, fid, bid := verifyFixture(t)
			must(t, os.Remove(s.object(bid)))
			var f File
			must(t, s.read(fid, 'F', &f))
			if changed {
				must(t, os.WriteFile(f.Path, []byte("different content"), 0600))
			}
			var out strings.Builder
			if err := s.verify([]string{sid}, &out); err == nil {
				t.Fatal("missing blob passed")
			}
			if strings.Contains(out.String(), "from original file") == changed {
				t.Fatal(out.String())
			}
			err := s.verify([]string{sid, "--repair"}, io.Discard)
			if changed {
				if err == nil {
					t.Fatal("changed file substituted")
				}
				if _, e := os.Stat(s.object(bid)); !os.IsNotExist(e) {
					t.Fatal("changed source published")
				}
			} else {
				must(t, err)
				must(t, s.verify([]string{sid}, io.Discard))
			}
		})
	}
}
func TestRepairRevalidatesBeforeReplacing(t *testing.T) {
	s, _, fid, bid := verifyFixture(t)
	var f File
	must(t, s.read(fid, 'F', &f))
	before, err := os.ReadFile(s.object(bid))
	must(t, err)
	must(t, os.WriteFile(f.Path, []byte("changed after planning"), 0600))
	if err = s.repairObject(bid, recoveryCandidate{source: f.Path, local: true}); err == nil {
		t.Fatal("source race accepted")
	}
	after, err := os.ReadFile(s.object(bid))
	must(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("active object changed on failed repair")
	}
}
func TestRepairMissingSessionAndExplicitDeletedSession(t *testing.T) {
	s, sid, _, _ := verifyFixture(t)
	must(t, s.trashObject(sid, "broken"))
	var out strings.Builder
	if err := s.verify(nil, &out); err == nil {
		t.Fatal("missing root referenced by cache not detected")
	}
	if !strings.Contains(out.String(), "Recoverable "+sid) {
		t.Fatal(out.String())
	}
	must(t, s.verify([]string{"--repair"}, io.Discard))
	must(t, s.deleteSession(sid, io.Discard))
	must(t, s.verify([]string{"--repair"}, io.Discard))
	if _, err := os.Stat(s.object(sid)); !os.IsNotExist(err) {
		t.Fatal("deleted root resurrected by whole-store repair")
	}
	if err := s.verify([]string{sid[:8]}, io.Discard); err == nil {
		t.Fatal("dry run repaired deleted root")
	}
	must(t, s.verify([]string{"--repair", sid[:8]}, io.Discard))
	refs, err := s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Session != sid {
		t.Fatal("restored session is not discoverable")
	}
}

func TestVerifyRejectsCorruptRecoveryCopy(t *testing.T) {
	s, sid, fid, bid := verifyFixture(t)
	var f File
	must(t, s.read(fid, 'F', &f))
	must(t, os.Remove(f.Path))
	must(t, s.trashObject(bid, "orphan"))
	entries, err := os.ReadDir(filepath.Join(s.backend.LocalRoot(), "trash"))
	must(t, err)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), bid) {
			must(t, os.WriteFile(filepath.Join(s.backend.LocalRoot(), "trash", e.Name()), []byte("corrupted candidate"), 0600))
		}
	}
	var out strings.Builder
	if err = s.verify([]string{"--repair", sid}, &out); err == nil {
		t.Fatal("bad trash accepted")
	}
	if !strings.Contains(out.String(), "Recoverable: 0 objects") {
		t.Fatal(out.String())
	}
	if _, err = os.Stat(s.object(bid)); !os.IsNotExist(err) {
		t.Fatal("bad trash published")
	}
}
