package hobalight

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserNavigationRestoreAndOpen(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "nested", "file.txt"), []byte("hello"))
	s := newStore(t, filepath.Join(root, "storage"))
	id, err := s.backup(source, false)
	must(t, err)
	var output bytes.Buffer
	var opened []string
	changeWorkingDirectory(t, root)
	input := "\r\x1b[C\x1b[C\x1b[Cro\x1b[D\x1b[D\x1b[Dq"
	must(t, s.browse(strings.NewReader(input), &output, func(p string) error { opened = append(opened, p); return nil }))
	target := filepath.Join(canonical(root), restoreDirectory, id[:8], "nested", "file.txt")
	if len(opened) != 1 || canonical(opened[0]) != canonical(target) || string(readFile(t, target)) != "hello" {
		t.Fatalf("wrong open target: %v", opened)
	}
	for _, want := range []string{"nested/", "file.txt", "Opened ", "Session "} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
}
func TestBrowserEOFEmptyAndOpenFailure(t *testing.T) {
	s := newStore(t, t.TempDir())
	changeWorkingDirectory(t, t.TempDir())
	var out bytes.Buffer
	must(t, s.browse(strings.NewReader(""), &out, func(string) error { t.Fatal("unexpected open"); return nil }))
	if !strings.Contains(out.String(), "No backup sessions") {
		t.Fatal(out.String())
	}
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "file"), []byte("x"))
	_, err := s.backup(source, false)
	must(t, err)
	out.Reset()
	must(t, s.browse(strings.NewReader("\r\x1b[Coq"), &out, func(string) error { return errors.New("no associated application") }))
	if !strings.Contains(out.String(), "no associated application") {
		t.Fatal(out.String())
	}
}
func TestRestorePreservesModifiedFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("original"))
	s := newStore(t, filepath.Join(root, "storage"))
	id, err := s.backup(source, false)
	must(t, err)
	destination := filepath.Join(root, "restore")
	result, err := s.restoreResolved(id, "file", destination, &bytes.Buffer{})
	must(t, err)
	writeFile(t, result.target, []byte("modified"))
	if _, err := s.restoreResolved(id, "file", destination, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "has changed") {
		t.Fatalf("modified file accepted: %v", err)
	}
	if got := string(readFile(t, result.target)); got != "modified" {
		t.Fatal(got)
	}
}

func TestDefaultRestoreSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("original"))
	s := newStore(t, filepath.Join(root, "storage"))
	id, err := s.backup(source, false)
	must(t, err)
	changeWorkingDirectory(t, root)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, restoreDirectory)); err != nil {
		t.Skip(err)
	}
	if _, err := s.restoreResolved(id, "file", "", &bytes.Buffer{}); err == nil {
		t.Fatal("followed symlink restore directory")
	}
}
