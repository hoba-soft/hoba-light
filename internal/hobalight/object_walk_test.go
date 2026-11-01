package hobalight

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGCPreservesObjectsWhenSessionBucketIsSymlink(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	hidden := t.TempDir()
	if err := os.Symlink(hidden, filepath.Join(s.backend.LocalRoot(), "objects", "aa")); err != nil {
		t.Skip(err)
	}
	// Even if the symlink hides all roots, GC must not treat the store as empty.
	writeFile(t, filepath.Join(hidden, strings.Repeat("a", 64)+"S"), []byte("hidden session"))
	orphan := strings.Repeat("b", 64) + "B"
	writeFile(t, s.object(orphan), []byte("retained on failure"))
	var out bytes.Buffer
	if err := s.gc(&out); err == nil || !strings.Contains(err.Error(), "bucket is not a real directory") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(s.object(orphan)); err != nil {
		t.Fatal("GC moved objects despite incomplete inventory")
	}
	if err := s.refresh(&out); err == nil {
		t.Fatal("refresh silently ignored invalid bucket")
	}
}

func TestMaintenanceRejectsMisplacedObjectDirectories(t *testing.T) {
	for _, kind := range []string{"S", "F", "B"} {
		t.Run(kind, func(t *testing.T) {
			s := newStore(t, filepath.Join(t.TempDir(), "store"))
			id := strings.Repeat("a", 64) + kind
			makeDir(t, s.object(id))
			var out bytes.Buffer
			if err := s.gc(&out); err == nil {
				t.Fatal("GC accepted object directory")
			}
			if err := s.refresh(&out); err == nil {
				t.Fatal("refresh accepted object directory")
			}
		})
	}
}
