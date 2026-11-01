package hobalight

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupRestoreAndReuse(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	makeDir(t, filepath.Join(source, "nested"))
	original := []byte("hello backup\n")
	file := filepath.Join(source, "nested", "hello.txt")
	writeFile(t, file, original)
	s, err := openStore(filepath.Join(root, "storage"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.backup(source, false)
	if err != nil {
		t.Fatal(err)
	}
	var a Session
	if err = s.read(first, 'S', &a); err != nil {
		t.Fatal(err)
	}
	if a.Version != Version() || a.Platform != Platform() {
		t.Fatalf("missing binary provenance: %+v", a)
	}
	// Preserve metadata but replace content: unchanged identity must reuse the blob.
	info, err := os.Stat(file)
	must(t, err)
	writeFile(t, file, []byte("other backup\n"))
	must(t, os.Chtimes(file, info.ModTime(), info.ModTime()))
	second, err := s.backup(source, false)
	if err != nil {
		t.Fatal(err)
	}
	var b Session
	must(t, s.read(second, 'S', &b))
	if len(a.Files) != 1 || len(b.Files) != 1 || a.Files[0] != b.Files[0] {
		t.Fatal("unchanged files missing from snapshot")
	}
	destination := filepath.Join(root, "restored")
	if err = s.restore(second[:8], destination, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "nested", "hello.txt"))
	if err != nil || string(data) != string(original) {
		t.Fatalf("restore: %q %v", data, err)
	}
	must(t, s.restore(second[:8], destination, ""))
	if err = s.restore(first[:8], filepath.Join(root, "single"), "nested/hello.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestSymlinksAndStorageExclusion(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	makeDir(t, source)
	writeFile(t, filepath.Join(source, "file"), []byte("content"))
	if err := os.Symlink("file", filepath.Join(source, "link")); err != nil {
		t.Skip(err)
	}
	must(t, os.Symlink(".", filepath.Join(source, "directory-link")))
	must(t, os.Symlink("missing", filepath.Join(source, "broken")))
	s := newStore(t, filepath.Join(source, "storage"))
	id, err := s.backup(source, false)
	if err == nil || id == "" {
		t.Fatal("expected partial session")
	}
	var session Session
	must(t, s.read(id, 'S', &session))
	if len(session.Files) != 3 || len(session.Errors) != 1 {
		t.Fatalf("unexpected session: %+v", session)
	}
	dest := filepath.Join(root, "restored")
	if err = s.restore(id[:8], dest, ""); err == nil {
		t.Fatal("expected partial restore error")
	}
	info, err := os.Lstat(filepath.Join(dest, "link"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatal("symlink was not restored as regular file")
	}
}

func TestModificationCreatesNewIdentity(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	makeDir(t, source)
	name := filepath.Join(source, "file")
	writeFile(t, name, []byte("first"))
	s := newStore(t, filepath.Join(root, "storage"))
	first, err := s.backup(source, false)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, name, []byte("second content"))
	now := time.Now().Add(time.Second)
	must(t, os.Chtimes(name, now, now))
	second, err := s.backup(source, false)
	if err != nil {
		t.Fatal(err)
	}
	var a, b Session
	must(t, s.read(first, 'S', &a))
	must(t, s.read(second, 'S', &b))
	if a.Files[0] == b.Files[0] {
		t.Fatal("modified file reused identity")
	}
	if err := s.restore(second[:8], filepath.Join(root, "restore"), ""); err != nil {
		t.Fatal(err)
	}
}

func TestCompanionIDUsesOnlyIdentity(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	name := filepath.Join(source, "file")
	writeFile(t, name, []byte("content"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	var session Session
	must(t, s.read(id, 'S', &session))
	info, err := os.Stat(name)
	must(t, err)
	data, err := encode(identity(name, info))
	must(t, err)
	if len(session.Files) != 1 || session.Files[0] != hash(data)+"F" {
		t.Fatalf("companion ID includes more than identity: %v", session.Files)
	}
}
