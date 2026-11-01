package hobalight

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedFileIdentityAndRetry(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	name := filepath.Join(root, "file")
	writeFile(t, name, []byte("data"))
	info, err := os.Stat(name)
	must(t, err)
	first, err := s.failedFile(name, info, errors.New("read failed"))
	if err == nil {
		t.Fatal("failure lost")
	}
	again, _ := s.failedFile(name, info, errors.New("read failed"))
	changed, _ := s.failedFile(name, info, errors.New("different error"))
	if first != again || first == changed {
		t.Fatal("error not part of identity")
	}
	var failed File
	must(t, s.read(first, 'F', &failed))
	if failed.Blob != "" || failed.Error != "read failed" || failed.Size != 4 {
		t.Fatalf("%+v", failed)
	}
	data := string(readFile(t, s.object(first)))
	if strings.Contains(data, "blob:") {
		t.Fatal(data)
	}
	// Tampering with the error must invalidate the object checksum.
	writeFile(t, s.object(first), []byte(strings.Replace(data, "read failed", "tampered", 1)))
	if err := s.read(first, 'F', &failed); err == nil {
		t.Fatal("error checksum not checked")
	}
	writeFile(t, s.object(first), []byte(data))
	success, err := s.backupFile(name, info, false)
	must(t, err)
	if success == first {
		t.Fatal("failed object reused")
	}
	var healthy File
	must(t, s.read(success, 'F', &healthy))
	if healthy.Error != "" || healthy.Blob == "" {
		t.Fatalf("%+v", healthy)
	}
	unknown, _ := s.failedFile(name, nil, errors.New("stat failed"))
	must(t, s.read(unknown, 'F', &failed))
	if failed.Size != 0 || failed.Modified != "" || failed.Created != nil || failed.Path != portable(name) {
		t.Fatalf("%+v", failed)
	}
}

func TestFailedFileSessionLifecycle(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bad := filepath.Join(source, "nested", "bad.txt")
	writeFile(t, bad, []byte("known size"))
	writeFile(t, filepath.Join(source, "good.txt"), []byte("good"))
	s := newStore(t, filepath.Join(root, "store"))
	removed := false
	var updates []progressUpdate
	s.progress = func(p progressUpdate) {
		updates = append(updates, p)
		if p.total == 2 && !removed {
			removed = true
			must(t, os.Remove(bad))
		}
	}
	id, err := s.backup(source, false)
	if err == nil || id == "" {
		t.Fatal("expected published partial session")
	}
	last := updates[len(updates)-1]
	if last.done != 2 || last.errors != 1 || !strings.Contains(last.text(), "1 error") {
		t.Fatalf("%+v", last)
	}
	s.progress = nil
	var session Session
	must(t, s.read(id, 'S', &session))
	if len(session.Files) != 2 || len(session.Errors) != 1 {
		t.Fatalf("%+v", session)
	}
	ref, err := s.buildReference(id)
	must(t, err)
	if s.referenceStats([]Reference{ref})[id].files != "2" || s.referenceStats([]Reference{ref})[id].errors != "1" {
		t.Fatal("session count missing")
	}
	// Legacy references must produce the same counts from original objects.
	legacy := ref
	legacy.Files = nil
	if s.referenceStats([]Reference{legacy})[id].files != "2" || s.referenceStats([]Reference{legacy})[id].errors != "1" {
		t.Fatal("legacy count missing")
	}
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: refs}
	if view := u.view(100, 24); !strings.Contains(strings.Join(strings.Fields(view), " "), "2 1 14 B") {
		t.Fatal(view)
	}
	u.enter()
	u.enter() // nested directory
	if view := u.view(100, 24); !strings.Contains(view, "1 file, 1 errors") || !strings.Contains(view, "[error] bad.txt") {
		t.Fatal(view)
	}
	u.enter() // failed file details
	if view := u.view(100, 24); !strings.Contains(view, "Error:") || !strings.Contains(view, "Size: 10 bytes") {
		t.Fatal(view)
	}
	var out bytes.Buffer
	must(t, s.list([]string{id}, &out))
	if !strings.Contains(strings.Join(strings.Fields(out.String()), " "), "1 1 10 B") {
		t.Fatal(out.String())
	}
	out.Reset()
	must(t, s.list([]string{id, "nested"}, &out))
	if !strings.Contains(out.String(), "[error] bad.txt") || !strings.Contains(out.String(), "10 B") {
		t.Fatal(out.String())
	}
	out.Reset()
	must(t, s.list([]string{id, "nested/bad.txt"}, &out))
	if !strings.Contains(out.String(), "Error: open") {
		t.Fatal(out.String())
	}
	out.Reset()
	must(t, s.find([]string{"bad"}, &out))
	if !strings.Contains(out.String(), "[error]") {
		t.Fatal(out.String())
	}
	_, files, err := s.sessionFiles(id)
	must(t, err)
	tree := browserTree(files)
	if tree[""][0].errors != 1 {
		t.Fatal(tree)
	}
	for _, f := range files {
		if f.metadata.Error != "" {
			if !strings.Contains(strings.Join(fileDetails(f, 100), "\n"), "Error:") {
				t.Fatal("detail missing error")
			}
		}
	}
	out.Reset()
	dest := filepath.Join(root, "restored")
	if _, err := s.restoreResolved(id, "", dest, &out); err == nil {
		t.Fatal("restore unexpectedly succeeded")
	}
	if string(readFile(t, filepath.Join(dest, "good.txt"))) != "good" {
		t.Fatal("healthy file not restored")
	}
	if _, err := os.Stat(filepath.Join(dest, "nested", "bad.txt")); !os.IsNotExist(err) {
		t.Fatal("created failed file")
	}
	must(t, s.refresh(&out))
	must(t, s.gc(&out))
	for _, file := range session.Files {
		var f File
		must(t, s.read(file, 'F', &f))
	}
	// A later healthy backup keeps the old failed session readable.
	writeFile(t, bad, []byte("recovered"))
	next, err := s.backup(source, false)
	must(t, err)
	_, healthy, err := s.sessionFiles(next)
	must(t, err)
	if filesErrors(healthy, "") != 0 {
		t.Fatal("retry failed")
	}
}
