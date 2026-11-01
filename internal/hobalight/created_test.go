package hobalight

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestCreationTime(t *testing.T) {
	before := time.Now().Add(-2 * time.Second)
	name := filepath.Join(t.TempDir(), "new-file")
	writeFile(t, name, []byte("test"))
	info, err := os.Stat(name)
	must(t, err)
	created := creationTime(name, info)
	if created == nil {
		if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			t.Fatal("creation time unavailable for a fresh local file")
		}
		t.Skip("filesystem does not provide birth time")
	}
	value, err := time.Parse(time.RFC3339Nano, *created)
	must(t, err)
	if value.Before(before) || value.After(time.Now().Add(2*time.Second)) {
		t.Fatalf("unexpected birth time: %s", *created)
	}
	// A future modification time avoids macOS adjusting birth time when a
	// modification time is explicitly moved before the original creation.
	changed := time.Now().Add(time.Hour)
	must(t, os.Chtimes(name, changed, changed))
	info, err = os.Stat(name)
	must(t, err)
	again := creationTime(name, info)
	if again == nil || *again != *created {
		t.Fatal("modification time change affected birth time")
	}
}
