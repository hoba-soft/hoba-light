package hobalight

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReferenceNamesAndSidecars(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("hello"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	started, err := time.Parse(time.RFC3339Nano, refs[0].Started)
	must(t, err)
	dir := filepath.Join(s.backend.LocalRoot(), "sessions")
	name := sessionReferenceName(started, id)
	original := readFile(t, filepath.Join(dir, name))
	sidecar := filepath.Join(dir, "._"+name)
	metadata := []byte{0, 5, 22, 7, 0, 0}
	writeFile(t, sidecar, metadata)
	writeFile(t, filepath.Join(dir, ".hidden.yaml"), metadata)
	// Listing (also used by the interactive browser) ignores binary sidecars.
	var output bytes.Buffer
	must(t, s.list(nil, &output))
	if !strings.Contains(output.String(), id[:8]) {
		t.Fatal(output.String())
	}
	// Legacy names remain readable and refresh must not rewrite sidecars.
	legacy := started.UTC().Format("20060102T150405.000000000Z") + "-" + id[:8] + ".yaml"
	must(t, os.Rename(filepath.Join(dir, name), filepath.Join(dir, legacy)))
	must(t, s.refresh(io.Discard))
	if !bytes.Equal(readFile(t, sidecar), metadata) {
		t.Fatal("sidecar changed")
	}
	refs, err = s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Session != id {
		t.Fatal(refs)
	}
	// Rebuilt references use the same start time and name as backup.
	must(t, os.Remove(filepath.Join(dir, legacy)))
	must(t, s.refresh(io.Discard))
	if !bytes.Equal(readFile(t, filepath.Join(dir, name)), original) {
		t.Fatal("rebuilt reference differs")
	}
	must(t, s.deleteSession(id, io.Discard))
	if !bytes.Equal(readFile(t, sidecar), metadata) {
		t.Fatal("delete touched sidecar")
	}
	refs, err = s.sessionReferences()
	must(t, err)
	if len(refs) != 0 {
		t.Fatal(refs)
	}
}

func TestSessionReferenceNameUTCAndOrder(t *testing.T) {
	id := strings.Repeat("a", 64) + "S"
	stamp := time.Date(2026, 10, 18, 23, 58, 4, 735327800, time.FixedZone("offset", 7200))
	name := sessionReferenceName(stamp, id)
	if name != "2026-10-18_21-58-04Z-aaaaaaaa.yaml" {
		t.Fatal(name)
	}
	for _, delta := range []time.Duration{time.Second, time.Hour, 24 * time.Hour} {
		if name >= sessionReferenceName(stamp.Add(delta), id) {
			t.Fatal("names do not sort chronologically")
		}
	}
}
