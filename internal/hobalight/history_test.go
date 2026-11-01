package hobalight

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryPathForms(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"/home/me/docs/../file.txt", "/home/me/file.txt"},
		{`c:\Users\me\file.txt`, "C:/Users/me/file.txt"},
		{"C:/Users/me/file.txt", "C:/Users/me/file.txt"},
		{`\\server\share\file.txt`, "//server/share/file.txt"},
		{"/home/a\\b", "/home/a\\b"},
	} {
		got, err := historyPath(tc.input)
		must(t, err)
		if got != tc.want {
			t.Fatalf("%q: %q", tc.input, got)
		}
	}
	for _, invalid := range []string{"", "relative/file", "C:relative", "bad\x00name"} {
		if _, err := historyPath(invalid); err == nil {
			t.Fatal("accepted", invalid)
		}
	}
}

func TestHistoryGroupsVersionsAndIncludesFailures(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	name := filepath.Join(source, "file.txt")
	writeFile(t, name, []byte("first"))
	s := newStore(t, filepath.Join(root, "store"))
	first, err := s.backup(source, false)
	must(t, err)
	_, err = s.backup(source, false)
	must(t, err)
	writeFile(t, name, []byte("second version"))
	second, err := s.backup(source, false)
	must(t, err)
	versions, err := s.historyVersions(name)
	must(t, err)
	if len(versions) != 2 || len(versions[0].sessions) != 1 || len(versions[1].sessions) != 2 || versions[0].file.Size != 14 {
		t.Fatalf("%+v", versions)
	}
	target, err := s.historyTarget([]string{first, "file.txt"})
	must(t, err)
	if target != portable(name) {
		t.Fatal(target)
	}
	if _, err := s.historyTarget([]string{second, "."}); err == nil {
		t.Fatal("directory accepted")
	}
	// Removing indices must not hide active sessions; blob data is not needed.
	must(t, os.RemoveAll(filepath.Join(s.backend.LocalRoot(), "sessions")))
	s.sessionListings = nil
	for _, v := range versions {
		must(t, os.Remove(s.object(v.file.Blob)))
	}
	versions, err = s.historyVersions(name)
	must(t, err)
	if len(versions) != 2 {
		t.Fatal("unindexed history lost")
	}
	info, err := os.Stat(name)
	must(t, err)
	fid, _ := s.failedFile(name, info, errors.New("read denied"))
	session := Session{Directory: portable(source), Started: "2099-01-01T00:00:00Z", Files: []string{fid}}
	data, err := encode(session)
	must(t, err)
	must(t, s.put(hash(data)+"S", data))
	var out bytes.Buffer
	must(t, s.history([]string{name}, &out))
	if !strings.Contains(out.String(), "[error]") || !strings.Contains(out.String(), "2 sessions") {
		t.Fatal(out.String())
	}
	versions, err = s.historyVersions(name)
	must(t, err)
	if versions[0].id != fid {
		t.Fatal("newest version not first")
	}
}

func TestHistoryForeignSourceAndExactMatching(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	for i, name := range []string{"C:/Users/me/report.txt", "C:/Users/other/report.txt"} {
		fid, _ := s.failedFile(name, nil, errors.New("unavailable"))
		session := Session{Directory: "C:/Users", Started: []string{"2026-01-01T00:00:00.9Z", "2026-01-01T00:00:00Z"}[i], Files: []string{fid}}
		data, err := encode(session)
		must(t, err)
		must(t, s.put(hash(data)+"S", data))
	}
	versions, err := s.historyVersions(`C:\Users\me\report.txt`)
	must(t, err)
	if len(versions) != 1 || versions[0].file.Path != "C:/Users/me/report.txt" {
		t.Fatal(versions)
	}
	versions, err = s.historyVersions("C:/Users/me/report")
	must(t, err)
	if len(versions) != 0 {
		t.Fatal("matched prefix instead of exact path")
	}
}

func TestHistoryOrderingUsesParsedTimestamps(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	ids := []string{}
	for i, stamp := range []string{"2026-01-01T00:00:00Z", "2026-01-01T00:00:00.9Z"} {
		id, _ := s.failedFile("/source/file", nil, errors.New([]string{"older", "newer"}[i]))
		ids = append(ids, id)
		session := Session{Directory: "/source", Started: stamp, Files: []string{id}}
		data, err := encode(session)
		must(t, err)
		must(t, s.put(hash(data)+"S", data))
	}
	versions, err := s.historyVersions("/source/file")
	must(t, err)
	if len(versions) != 2 || versions[0].id != ids[1] {
		t.Fatal("timestamps sorted lexically")
	}
}

func TestBrowserHistorySelectionAndPaging(t *testing.T) {
	h := &browserHistory{path: "/source/file"}
	for i := 0; i < 25; i++ {
		h.versions = append(h.versions, fileVersion{id: strings.Repeat("a", 64) + "F", sessions: []searchMatch{
			{ref: Reference{Session: strings.Repeat("b", 64) + "S", Started: "2026-01-01T00:00:00Z"}, file: sessionFile{relative: "file"}},
			{ref: Reference{Session: strings.Repeat("c", 64) + "S", Started: "2025-01-01T00:00:00Z"}, file: sessionFile{relative: "file"}},
		}})
	}
	u := browserUI{history: h}
	u.key("pagedown")
	if h.cursor != 10 {
		t.Fatal(h.cursor)
	}
	u.key("end")
	if h.cursor != 24 {
		t.Fatal(h.cursor)
	}
	if view := u.view(80, 24); !strings.Contains(view, "25/25 versions") {
		t.Fatal(view)
	}
	u.key("right")
	u.key("down")
	id, selected, file, err := u.selectedRestore()
	must(t, err)
	if id != strings.Repeat("c", 64)+"S" || selected != "file" || !file {
		t.Fatal("wrong historical selection")
	}
	u.key("left")
	if h.sessions {
		t.Fatal("did not return to versions")
	}
	u.key("left")
	if u.history != nil {
		t.Fatal("did not close history")
	}
}

func TestHistoryUsesFirstSurvivingSession(t *testing.T) {
	s := newStore(t, t.TempDir())
	old, _ := s.failedFile("/source/file", nil, errors.New("older version"))
	newer, _ := s.failedFile("/source/file", nil, errors.New("newer version"))
	var first string
	for i, item := range []struct{ id, date string }{{old, "2026-01-01T00:00:00Z"}, {newer, "2026-02-01T00:00:00Z"}, {old, "2026-03-01T00:00:00Z"}} {
		data, err := encode(Session{Directory: "/source", Started: item.date, Files: []string{item.id}})
		must(t, err)
		id := hash(data) + "S"
		must(t, s.put(id, data))
		if i == 0 {
			first = id
		}
	}
	versions, err := s.historyVersions("/source/file")
	must(t, err)
	if versions[0].id != newer || versions[1].earliest.UTC().Format(time.RFC3339Nano) != "2026-01-01T00:00:00Z" {
		t.Fatal(versions)
	}
	u := browserUI{history: &browserHistory{path: "/source/file", versions: versions}}
	view := u.view(120, 30)
	if !strings.Contains(view, "First backed up") || !strings.Contains(view, "2026-01-01T00:00:00Z") || strings.Contains(view, "2026-03-01T00:00:00Z") {
		t.Fatal(view)
	}
	must(t, os.Remove(s.object(first)))
	versions, err = s.historyVersions("/source/file")
	must(t, err)
	if versions[0].id != old || versions[0].earliest.UTC().Format(time.RFC3339Nano) != "2026-03-01T00:00:00Z" {
		t.Fatal(versions)
	}
}
