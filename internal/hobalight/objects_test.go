package hobalight

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAmbiguousHashAndCorruption(t *testing.T) {
	s := newStore(t, t.TempDir())
	for _, suffix := range []string{"0", "1"} {
		id := "12345678" + strings.Repeat("0", 55) + suffix + "S"
		writeFile(t, s.object(id), []byte("invalid"))
	}
	if _, err := s.resolve("12345678", 'S'); err == nil {
		t.Fatal("accepted ambiguous hash")
	}
	if _, err := s.resolve("../abcde", 'S'); err == nil {
		t.Fatal("accepted invalid hash")
	}
	id := "12345678" + strings.Repeat("0", 56) + "S"
	var session Session
	if err := s.read(id, 'S', &session); err == nil {
		t.Fatal("accepted corrupt session")
	}
}

func TestConcurrentObjectWriters(t *testing.T) {
	s := newStore(t, t.TempDir())
	data := []byte("the same immutable object")
	id := hash(data) + "S"
	results := make(chan error, 16)
	for i := 0; i < cap(results); i++ {
		go func() { results <- s.put(id, data) }()
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if string(readFile(t, s.object(id))) != string(data) {
		t.Fatal("concurrent writers corrupted the object")
	}
	entries, err := os.ReadDir(filepath.Dir(s.object(id)))
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("temporary objects leaked: %v", entries)
	}
}

func TestRestoreFileRejectsInvalidBlobReference(t *testing.T) {
	s := newStore(t, t.TempDir())
	f := File{Identity: Identity{Path: "/source/file", Modified: timestamp(time.Now())}, Blob: "../../escape"}
	dest := t.TempDir()
	if err := s.restoreFile(f, dest, "file"); err == nil || !strings.Contains(err.Error(), "invalid blob reference") {
		t.Fatalf("accepted invalid blob reference: %v", err)
	}
}

func TestObjectsAreBucketed(t *testing.T) {
	s := newStore(t, t.TempDir())
	data := []byte("bucketed")
	id := hash(data) + "S"
	must(t, s.put(id, data))
	want := filepath.Join(s.backend.LocalRoot(), "objects", id[:2], id)
	if s.object(id) != want || string(readFile(t, want)) != "bucketed" {
		t.Fatal("object not stored in its bucket")
	}
	got, err := s.resolve(id[:8], 'S')
	if err != nil || got != id {
		t.Fatalf("resolve: %s %v", got, err)
	}
}

func TestRetryFileOperation(t *testing.T) {
	transient := errors.New("transient")
	permanent := errors.New("permanent")
	for _, tc := range []struct {
		name     string
		failures []error
		want     error
		calls    int
	}{
		{"recovers", []error{transient, transient}, nil, 3},
		{"exhausted", []error{transient, transient, transient, transient}, transient, 4},
		{"permanent", []error{permanent}, permanent, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			operation := func() error {
				calls++
				if calls <= len(tc.failures) {
					return tc.failures[calls-1]
				}
				return nil
			}
			if err := retryFileOperation(operation, func(err error) bool { return errors.Is(err, transient) }, []time.Duration{0, 0, 0}); !errors.Is(err, tc.want) || calls != tc.calls {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestCompanionReplacementArchivesOldBytes(t *testing.T) {
	s := newStore(t, t.TempDir())
	id := hash([]byte("identity")) + "F"
	must(t, s.put(id, []byte("first")))
	must(t, s.put(id, []byte("second")))
	must(t, s.put(id, []byte("third")))
	must(t, s.put(id, []byte("third")))
	entries, err := os.ReadDir(filepath.Join(s.backend.LocalRoot(), "trash"))
	must(t, err)
	if len(entries) != 2 {
		t.Fatalf("expected two archived versions, got %d", len(entries))
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), id+"-") || !strings.HasSuffix(entry.Name(), ".broken") {
			t.Fatal(entry.Name())
		}
		seen[string(readFile(t, filepath.Join(s.backend.LocalRoot(), "trash", entry.Name())))] = true
	}
	if !seen["first"] || !seen["second"] || string(readFile(t, s.object(id))) != "third" {
		t.Fatal("replacement lost versions")
	}
}

func TestArchiveFailurePreservesActiveCompanion(t *testing.T) {
	s := newStore(t, t.TempDir())
	id := hash([]byte("identity")) + "F"
	must(t, s.put(id, []byte("old")))
	writeFile(t, filepath.Join(s.backend.LocalRoot(), "trash"), []byte("obstruction"))
	if err := s.put(id, []byte("new")); err == nil {
		t.Fatal("ignored archive failure")
	}
	if string(readFile(t, s.object(id))) != "old" {
		t.Fatal("replaced object despite failed archival")
	}
}

func TestResolveSessionAndFileHashes(t *testing.T) {
	s := newStore(t, t.TempDir())
	file := strings.Repeat("a", 63) + "fF"
	session := strings.Repeat("a", 63) + "eS"
	writeFile(t, s.object(file), []byte("file"))
	writeFile(t, s.object(session), []byte("session"))
	if _, err := s.resolveKinds("aaaaaaaa", "SF"); err == nil {
		t.Fatal("ambiguous S/F prefix accepted")
	}
	for _, input := range []string{file, strings.ToUpper(file[:64])} {
		id, err := s.resolveKinds(input, "SF")
		must(t, err)
		if id != file {
			t.Fatalf("%s: %s", input, id)
		}
	}
}
