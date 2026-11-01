package hobalight

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func verifyFixture(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("content"))
	sid, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(sid)
	must(t, err)
	return s, sid, files[0].id, files[0].metadata.Blob
}
func TestVerifyHealthyScopeAndOrphans(t *testing.T) {
	s, sid, _, _ := verifyFixture(t)
	var out strings.Builder
	must(t, s.verify(nil, &out))
	if !strings.Contains(out.String(), "1 sessions, 1 file objects, 1 blobs") || !strings.Contains(out.String(), "0 integrity issues") {
		t.Fatal(out.String())
	}
	must(t, os.RemoveAll(filepath.Join(s.backend.LocalRoot(), "sessions")))
	orphan := strings.Repeat("f", 64) + "B"
	must(t, os.MkdirAll(filepath.Dir(s.object(orphan)), 0700))
	must(t, os.WriteFile(s.object(orphan), []byte("bad zip"), 0600))
	must(t, s.verify([]string{sid[:8]}, io.Discard))
	out.Reset()
	if err := s.verify(nil, &out); err == nil {
		t.Fatal("corrupt orphan passed")
	}
	if !strings.Contains(out.String(), orphan) {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(s.object(orphan)); err != nil {
		t.Fatal("verify changed object")
	}
}
func TestVerifyCorruption(t *testing.T) {
	for _, kind := range []string{"missing file", "missing blob", "session checksum", "file checksum", "blob checksum", "zip", "size", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			s, sid, fid, bid := verifyFixture(t)
			switch kind {
			case "missing file":
				must(t, os.Remove(s.object(fid)))
			case "missing blob":
				must(t, os.Remove(s.object(bid)))
			case "session checksum":
				f, err := os.OpenFile(s.object(sid), os.O_APPEND|os.O_WRONLY, 0600)
				must(t, err)
				_, err = f.WriteString(" ")
				must(t, err)
				must(t, f.Close())
			case "file checksum":
				must(t, os.WriteFile(s.object(fid), []byte("path: changed\n"), 0600))
			case "zip":
				must(t, os.WriteFile(s.object(bid), []byte("broken"), 0600))
			case "blob checksum":
				var b bytes.Buffer
				z := zip.NewWriter(&b)
				w, err := z.Create("data")
				must(t, err)
				_, err = w.Write([]byte("WRONG!!"))
				must(t, err)
				must(t, z.Close())
				must(t, os.WriteFile(s.object(bid), b.Bytes(), 0600))
			case "size":
				var f File
				must(t, s.read(fid, 'F', &f))
				f.Size++
				data, err := encode(f)
				must(t, err)
				next := hash(data) + "F"
				must(t, s.put(next, data))
				var session Session
				must(t, s.read(sid, 'S', &session))
				session.Files = []string{next}
				data, err = encode(session)
				must(t, err)
				sid = hash(data) + "S"
				must(t, s.put(sid, data))
			case "duplicate":
				var session Session
				must(t, s.read(sid, 'S', &session))
				session.Files = append(session.Files, fid)
				data, err := encode(session)
				must(t, err)
				sid = hash(data) + "S"
				must(t, s.put(sid, data))
			}
			var out strings.Builder
			if err := s.verify([]string{sid}, &out); err == nil {
				t.Fatal("corruption passed")
			}
			if !strings.Contains(out.String(), "DAMAGED") {
				t.Fatal(out.String())
			}
			if _, err := os.Stat(filepath.Join(s.backend.LocalRoot(), ".gc.lock")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("lock not cleaned up")
			}
		})
	}
}
func TestVerifyIncompleteAndSharedBlob(t *testing.T) {
	s, sid, fid, bid := verifyFixture(t)
	v := &verifier{store: s, out: io.Discard, checked: map[string]bool{}, blobs: map[string]verifiedBlob{}}
	if got := v.blob(bid); got.err != nil || got.size != 7 {
		t.Fatal(got)
	}
	if got := v.blob(bid); got.err != nil || v.blobCount != 1 {
		t.Fatal("blob not deduplicated")
	}
	var f File
	must(t, s.read(fid, 'F', &f))
	f.Blob = ""
	f.Error = "access denied"
	f.Modified = ""
	data, err := encode(f)
	must(t, err)
	failed := hash(data) + "F"
	must(t, s.put(failed, data))
	var session Session
	must(t, s.read(sid, 'S', &session))
	session.Files = []string{failed}
	data, err = encode(session)
	must(t, err)
	sid = hash(data) + "S"
	must(t, s.put(sid, data))
	var out strings.Builder
	if err = s.verify([]string{sid}, &out); err == nil {
		t.Fatal("incomplete backup passed")
	}
	if !strings.Contains(out.String(), "INCOMPLETE") || !strings.Contains(out.String(), "0 integrity issues, 1 backup errors") {
		t.Fatal(out.String())
	}
}
func TestVerifyLockAndOutputFailure(t *testing.T) {
	s, sid, _, _ := verifyFixture(t)
	release, err := s.beginBackup()
	must(t, err)
	if err = s.verify(nil, io.Discard); err == nil {
		t.Fatal("verify raced backup")
	}
	release()
	if err = s.verify([]string{sid}, rejectingDiffWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	must(t, s.verify(nil, io.Discard))
	if err = s.verify([]string{sid, sid}, io.Discard); err == nil {
		t.Fatal("extra args accepted")
	}
}
