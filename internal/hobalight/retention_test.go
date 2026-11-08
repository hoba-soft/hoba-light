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

func seedRetentionSession(t *testing.T, s *Store, file string, started time.Time, contents string, single bool) string {
	t.Helper()
	writeFile(t, file, []byte(contents))
	must(t, os.Chtimes(file, started, started))
	info, err := os.Stat(file)
	must(t, err)
	fid, err := s.backupFile(file, info, false)
	must(t, err)
	session := Session{Directory: portable(filepath.Dir(file)), Started: timestamp(started), Files: []string{fid}, Status: sessionCompleted}
	if single {
		session.Source = portable(file)
	}
	data, err := encode(session)
	must(t, err)
	id := hash(data) + "S"
	must(t, s.put(id, data))
	ref, err := s.referenceFromSession(id, session)
	must(t, err)
	data, err = encode(ref)
	must(t, err)
	must(t, writeStorageBytes(s.backend, "sessions/"+id+".yaml", data, false))
	return id
}
func retentionObjectIDs(t *testing.T, s *Store) []string {
	t.Helper()
	var ids []string
	must(t, s.walkObjects(func(id string, err error) error {
		if err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	}))
	return ids
}
func TestRetentionRules(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	a := filepath.Join(root, "a", "file")
	b := filepath.Join(root, "b", "file")
	newest := seedRetentionSession(t, s, a, now.Add(-time.Hour), "newest", false)
	dayKept := seedRetentionSession(t, s, a, now.Add(-25*time.Hour), "within days", false)
	old := seedRetentionSession(t, s, a, now.Add(-72*time.Hour), "old", false)
	countKept := seedRetentionSession(t, s, b, now.Add(-10*24*time.Hour), "count kept", false)
	bOld := seedRetentionSession(t, s, b, now.Add(-11*24*time.Hour), "b old", false)
	for _, tc := range []struct {
		policy  Retention
		expired []string
	}{
		{Retention{}, nil},
		{Retention{Days: 2}, []string{old, countKept, bOld}},
		{Retention{Sessions: 1}, []string{old, dayKept, bOld}},
		{Retention{Days: 2, Sessions: 1}, []string{old, bOld}},
	} {
		s.retention = tc.policy
		expired, err := s.expiredSessions(retentionObjectIDs(t, s), now)
		must(t, err)
		got := map[string]bool{}
		for _, session := range expired {
			got[session.id] = true
		}
		if len(got) != len(tc.expired) {
			t.Fatalf("%+v: %+v", tc.policy, got)
		}
		for _, id := range tc.expired {
			if !got[id] {
				t.Fatalf("%+v did not expire %s", tc.policy, id)
			}
		}
		if got[newest] {
			t.Fatal("newest source session expired")
		}
	}
}
func TestGCRetentionDryRunAndSharedData(t *testing.T) {
	now := time.Now().UTC()
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	a := filepath.Join(root, "a", "file")
	b := filepath.Join(root, "b", "file")
	old := seedRetentionSession(t, s, a, now.Add(-72*time.Hour), "shared data", false)
	keep := seedRetentionSession(t, s, a, now.Add(-time.Hour), "new data", false)
	other := seedRetentionSession(t, s, b, now.Add(-time.Hour), "shared data", false)
	s.retention = Retention{Sessions: 1}
	before := strings.Join(retentionObjectIDs(t, s), ",")
	var out bytes.Buffer
	must(t, s.gcOptions(&out, true, true))
	if !strings.Contains(out.String(), "Would delete session "+old) || strings.Join(retentionObjectIDs(t, s), ",") != before {
		t.Fatal("dry-run changed data or omitted plan", out.String())
	}
	refs, err := s.sessionReferences()
	must(t, err)
	if len(refs) != 3 {
		t.Fatal("dry-run deleted references")
	}
	must(t, s.gc(io.Discard))
	if _, err := s.backend.Stat(objectKey(old)); !os.IsNotExist(err) {
		t.Fatal("expired session remained")
	}
	refs, err = s.sessionReferences()
	must(t, err)
	if len(refs) != 2 {
		t.Fatal("expired references remained")
	}
	must(t, s.verify(nil, io.Discard))
	for _, tc := range []struct{ id, contents string }{{keep, "new data"}, {other, "shared data"}} {
		dest := t.TempDir()
		_, err := s.restoreResolved(tc.id, "", dest, io.Discard)
		must(t, err)
		if string(readFile(t, filepath.Join(dest, "file"))) != tc.contents {
			t.Fatal("GC lost retained data")
		}
	}
}
func TestRetentionSingleFilesAreSeparateSources(t *testing.T) {
	now := time.Now().UTC()
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	first := seedRetentionSession(t, s, filepath.Join(root, "source", "first"), now.Add(-time.Hour), "first", true)
	second := seedRetentionSession(t, s, filepath.Join(root, "source", "second"), now, "second", true)
	s.retention = Retention{Sessions: 1}
	must(t, s.gc(io.Discard))
	for _, id := range []string{first, second} {
		var session Session
		must(t, s.read(id, 'S', &session))
	}
}
func TestRetentionValidatesGraphBeforeDeleting(t *testing.T) {
	now := time.Now().UTC()
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	file := filepath.Join(root, "source", "file")
	old := seedRetentionSession(t, s, file, now.Add(-72*time.Hour), "old data", false)
	seedRetentionSession(t, s, file, now, "new data", false)
	_, files, err := s.sessionFiles(old)
	must(t, err)
	must(t, os.Remove(s.object(files[0].metadata.Blob)))
	s.retention = Retention{Sessions: 1}
	if err := s.gc(io.Discard); err == nil {
		t.Fatal("GC ignored damaged expired session")
	}
	var session Session
	must(t, s.read(old, 'S', &session))
	refs, err := s.sessionReferences()
	must(t, err)
	if len(refs) != 2 {
		t.Fatal("GC pruned before graph validation")
	}
}

func TestRetentionAllExpiredAndMissingReference(t *testing.T) {
	now := time.Now().UTC()
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	id := seedRetentionSession(t, s, filepath.Join(root, "source", "file"), now.Add(-72*time.Hour), "expired", false)
	// Losing the derived index must not exempt an authoritative session from policy.
	must(t, s.backend.Remove("sessions/"+id+".yaml"))
	s.retention = Retention{Days: 1}
	s.sessionDate = "2099-01-01" // Display filters must never constrain GC.
	must(t, s.gc(io.Discard))
	if len(retentionObjectIDs(t, s)) != 0 {
		t.Fatal("fully expired graph remained")
	}
}
func TestRetentionMalformedReferenceAbortsBeforeDeletion(t *testing.T) {
	now := time.Now().UTC()
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	id := seedRetentionSession(t, s, filepath.Join(root, "source", "file"), now.Add(-72*time.Hour), "expired", false)
	writeFile(t, filepath.Join(s.backend.LocalRoot(), "sessions", "broken.yaml"), []byte("[broken"))
	before := strings.Join(retentionObjectIDs(t, s), ",")
	s.retention = Retention{Days: 1}
	if err := s.gc(io.Discard); err == nil {
		t.Fatal("malformed cleanup plan was accepted")
	}
	if strings.Join(retentionObjectIDs(t, s), ",") != before {
		t.Fatal("GC changed objects before validating reference cleanup")
	}
	var session Session
	must(t, s.read(id, 'S', &session))
}
