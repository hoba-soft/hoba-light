package hobalight

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeleteSessionLeavesSharedDataForGC(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "shared"), []byte("shared"))
	writeFile(t, filepath.Join(source, "old-only"), []byte("old"))
	s := newStore(t, filepath.Join(root, "store"))
	first, err := s.backup(source, false)
	must(t, err)
	original := readFile(t, s.object(first))
	_, oldFiles, err := s.sessionFiles(first)
	must(t, err)
	must(t, os.Remove(filepath.Join(source, "old-only")))
	second, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	for _, ref := range refs {
		if ref.Session == first {
			data, err := encode(ref)
			must(t, err)
			writeFile(t, filepath.Join(s.backend.LocalRoot(), "sessions", "duplicate.yaml"), data)
		}
	}
	var out bytes.Buffer
	must(t, s.deleteSession(first[:8], &out))
	if !strings.Contains(out.String(), "Deleted session "+first[:8]) {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(s.object(first)); !os.IsNotExist(err) {
		t.Fatal("session object still present")
	}
	matches, err := filepath.Glob(filepath.Join(s.backend.LocalRoot(), "trash", first+"-*.deleted"))
	must(t, err)
	if len(matches) != 1 || !bytes.Equal(readFile(t, matches[0]), original) {
		t.Fatal("session not preserved in trash")
	}
	refs, err = s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Session != second {
		t.Fatalf("references: %+v", refs)
	}
	for _, f := range oldFiles {
		if _, err := os.Stat(s.object(f.id)); err != nil {
			t.Fatal("delete collected file metadata")
		}
		if _, err := os.Stat(s.object(f.metadata.Blob)); err != nil {
			t.Fatal("delete collected blob")
		}
	}
	must(t, s.refresh(&out))
	refs, err = s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Session != second {
		t.Fatal("refresh resurrected deleted session")
	}
	must(t, s.gc(&out))
	for _, f := range oldFiles {
		_, err := os.Stat(s.object(f.id))
		if f.relative == "old-only" && !os.IsNotExist(err) {
			t.Fatal("GC retained orphan metadata")
		}
		if f.relative == "shared" && err != nil {
			t.Fatal("GC collected shared metadata")
		}
		_, err = os.Stat(s.object(f.metadata.Blob))
		if f.relative == "old-only" && !os.IsNotExist(err) {
			t.Fatal("GC retained orphan blob")
		}
		if f.relative == "shared" && err != nil {
			t.Fatal("GC collected shared blob")
		}
	}
}

func TestDeleteFailurePreservesSessionAndReferences(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("data"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	writeFile(t, filepath.Join(s.backend.LocalRoot(), "trash"), []byte("obstruction"))
	var out bytes.Buffer
	if err := s.deleteSession(id, &out); err == nil {
		t.Fatal("expected trash failure")
	}
	if _, err := os.Stat(s.object(id)); err != nil {
		t.Fatal("lost session")
	}
	refs, err := s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Session != id {
		t.Fatal("lost references")
	}
}

func TestDeleteMissingReferencesAndDamagedSession(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	id := strings.Repeat("a", 64) + "S"
	writeFile(t, s.object(id), []byte("damaged object"))
	must(t, os.RemoveAll(filepath.Join(s.backend.LocalRoot(), "sessions")))
	var out bytes.Buffer
	must(t, s.deleteSession(id, &out))
	if err := s.deleteSession(id, &out); err == nil {
		t.Fatal("deleted non-existing session")
	}
}

func TestDeleteRespectsMaintenanceAndWriterLocks(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	id := strings.Repeat("a", 64) + "S"
	writeFile(t, s.object(id), []byte("session"))
	var out bytes.Buffer
	for _, begin := range []func() (func(), error){s.beginBackup, s.beginGC, func() (func(), error) { return s.beginWrite("refresh") }} {
		release, err := begin()
		must(t, err)
		if err := s.deleteSession(id, &out); err == nil {
			t.Fatal("delete bypassed lock")
		}
		release()
		if _, err := os.Stat(s.object(id)); err != nil {
			t.Fatal("lost session")
		}
	}
}

func TestBrowserDeleteRequiresExplicitConfirmation(t *testing.T) {
	for _, cancel := range []string{"n", "right", "left"} {
		t.Run(cancel, func(t *testing.T) {
			s := newStore(t, filepath.Join(t.TempDir(), "store"))
			id := strings.Repeat("a", 64) + "S"
			writeFile(t, s.object(id), []byte("session"))
			ref := Reference{Session: id, Directory: "/source", Started: "2026-10-25T10:00:00Z"}
			data, err := encode(ref)
			must(t, err)
			writeFile(t, filepath.Join(s.backend.LocalRoot(), "sessions", "ref.yaml"), data)
			u := browserUI{browser: browser{store: s}, refs: []Reference{ref}, compareBase: &diffTarget{id: ref.Session}}
			u.key("x")
			if u.pendingDelete == nil {
				t.Fatal("no confirmation")
			}
			for _, text := range []string{"Delete session?", id[:8], "/source", "2026-10-25", "n/Enter cancel"} {
				if !strings.Contains(u.view(100, 24), text) {
					t.Fatal("missing prompt detail: " + text)
				}
			}
			u.key("down")
			u.key("x")
			if u.pendingDelete == nil || u.cursor != 0 {
				t.Fatal("confirmation changed selection")
			}
			u.key(cancel)
			if u.pendingDelete != nil {
				t.Fatal("did not cancel")
			}
			if _, err := os.Stat(s.object(id)); err != nil {
				t.Fatal("deleted without confirmation")
			}
			u.key("delete")
			u.key("y")
			if len(u.refs) != 0 || u.pendingDelete != nil || u.cursor != 0 || u.compareBase != nil {
				t.Fatal("stale browser selection")
			}
			if !strings.Contains(u.view(100, 24), "No backup sessions.") {
				t.Fatal("empty browser missing")
			}
			if _, err := os.Stat(s.object(id)); !os.IsNotExist(err) {
				t.Fatal("session still present")
			}
		})
	}
}

func TestDeletePathFailurePreservesOriginal(t *testing.T) {
	for _, failure := range []string{"reference", "trash", "backup-lock", "maintenance-lock"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeFile(t, filepath.Join(source, "remove.txt"), []byte("remove"))
			writeFile(t, filepath.Join(source, "keep.txt"), []byte("keep"))
			s := newStore(t, filepath.Join(root, "store"))
			original, err := s.backup(source, false)
			must(t, err)
			originalBytes := readFile(t, s.object(original))
			refs, err := s.referencesForDeletion(original)
			must(t, err)
			var session Session
			must(t, s.read(original, 'S', &session))
			_, files, err := s.sessionFiles(original)
			must(t, err)
			session.Files = nil
			for _, f := range files {
				if f.relative != "remove.txt" {
					session.Files = append(session.Files, f.id)
				}
			}
			data, err := encode(session)
			must(t, err)
			next := hash(data) + "S"
			switch failure {
			case "reference":
				started, err := time.Parse(time.RFC3339Nano, session.Started)
				must(t, err)
				name := filepath.Join(s.backend.LocalRoot(), "sessions", sessionReferenceName(started, next))
				writeFile(t, filepath.Join(name, "obstruction"), []byte("keep"))
			case "trash":
				writeFile(t, filepath.Join(s.backend.LocalRoot(), "trash"), []byte("obstruction"))
			case "backup-lock":
				release, err := s.beginBackup()
				must(t, err)
				defer release()
			case "maintenance-lock":
				release, err := s.beginGC()
				must(t, err)
				defer release()
			}
			var out bytes.Buffer
			if _, err := s.deletePath(original, "remove.txt", &out); err == nil {
				t.Fatal("expected failure")
			}
			if !bytes.Equal(originalBytes, readFile(t, s.object(original))) {
				t.Fatal("original changed")
			}
			for _, name := range refs {
				if _, err := s.backend.Stat(name); err != nil {
					t.Fatal("original reference lost", err)
				}
			}
			if failure == "trash" {
				var replacement Session
				must(t, s.read(next, 'S', &replacement))
				if len(replacement.Files) != 1 {
					t.Fatal(replacement)
				}
				refs, err := s.referencesForDeletion(next)
				must(t, err)
				if len(refs) != 1 {
					t.Fatal("replacement not published before retiring original")
				}
			}
		})
	}
}

func TestGlobalDeletionRevalidatesConfirmationAndLocks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("data"))
	s := newStore(t, filepath.Join(root, "store"))
	first, err := s.backup(source, false)
	must(t, err)
	var session Session
	must(t, s.read(first, 'S', &session))
	plan, err := s.planFileDeletion(session.Files[0])
	must(t, err)
	second, err := s.backup(source, false)
	must(t, err)
	var out bytes.Buffer
	if err := s.deleteFileConfirmed(plan, &out); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale confirmation accepted: %v", err)
	}
	for _, id := range []string{first, second} {
		must(t, s.read(id, 'S', &session))
	}
	plan, err = s.planFileDeletion(plan.id)
	must(t, err)
	release, err := s.beginBackup()
	must(t, err)
	if err := s.deleteFileConfirmed(plan, &out); err == nil {
		t.Fatal("global deletion bypassed backup lock")
	}
	release()
	writeFile(t, filepath.Join(s.backend.LocalRoot(), "trash"), []byte("obstruct"))
	if err := s.deleteFileConfirmed(plan, &out); err == nil || !strings.Contains(err.Error(), "stopped after 0 of 2") {
		t.Fatalf("partial error not reported: %v", err)
	}
	for _, id := range []string{first, second} {
		must(t, s.read(id, 'S', &session))
	}
}

func TestGlobalDeletionReportsPartialReplacement(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("data"))
	s := newStore(t, filepath.Join(root, "store"))
	first, err := s.backup(source, false)
	must(t, err)
	_, err = s.backup(source, false)
	must(t, err)
	var session Session
	must(t, s.read(first, 'S', &session))
	plan, err := s.planFileDeletion(session.Files[0])
	must(t, err)
	blocked := plan.sessions[1]
	must(t, s.read(blocked.Session, 'S', &session))
	session.Files = []string{}
	data, err := encode(session)
	must(t, err)
	next := hash(data) + "S"
	started, err := time.Parse(time.RFC3339Nano, session.Started)
	must(t, err)
	writeFile(t, filepath.Join(s.backend.LocalRoot(), "sessions", sessionReferenceName(started, next), "obstruction"), []byte("keep"))
	var out bytes.Buffer
	err = s.deleteFileConfirmed(plan, &out)
	if err == nil || !strings.Contains(err.Error(), "stopped after 1 of 2") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(s.object(plan.sessions[0].Session)); !os.IsNotExist(err) {
		t.Fatal("first session not replaced")
	}
	must(t, s.read(blocked.Session, 'S', &session))
	if len(session.Files) != 1 {
		t.Fatal("blocked original lost its file")
	}
}
