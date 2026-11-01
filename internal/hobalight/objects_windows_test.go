package hobalight

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestReadPublishedFileRetriesSharingViolation(t *testing.T) {
	name := filepath.Join(t.TempDir(), "object")
	must(t, os.WriteFile(name, []byte("content"), 0600))
	path, err := windows.UTF16PtrFromString(name)
	must(t, err)
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	must(t, err)
	released := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		released <- windows.CloseHandle(handle)
	}()
	data, readErr := readPublishedFile(name)
	// Always join the helper, including on failure.
	must(t, <-released)
	must(t, readErr)
	if string(data) != "content" {
		t.Fatalf("%q", data)
	}
}
