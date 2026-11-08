package hobalight

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindScopesAndReferenceMetadata(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for _, name := range []string{"Report.txt", "docs/deep/REPORT.pdf", "docs2/report.csv", "report-folder/other.txt", "docs/Отчёт.txt"} {
		writeFile(t, filepath.Join(source, filepath.FromSlash(name)), []byte(name))
	}
	s := newStore(t, filepath.Join(root, "store"))
	first, err := s.backup(source, false)
	must(t, err)
	must(t, os.Remove(filepath.Join(source, "Report.txt")))
	_, err = s.backup(source, false)
	must(t, err)
	for _, tc := range []struct {
		query, id, dir string
		want           int
	}{{"report", "", "", 5}, {"REPORT", first, "", 3}, {"report", first, "docs", 1}, {"ОТЧЁТ", first, "docs", 1}, {"missing", first, "", 0}, {"other", first, "", 1}} {
		matches, err := s.findFiles(tc.query, tc.id, tc.dir)
		must(t, err)
		if len(matches) != tc.want {
			t.Fatalf("%+v: got %d", tc, len(matches))
		}
	}
	if _, err = s.findFiles("report", first, "absent"); err == nil {
		t.Fatal("missing scope accepted")
	}
	if _, err = s.findFiles(" ", first, ""); err == nil {
		t.Fatal("empty query accepted")
	}
	// Valid reference listings allow search even when an F object is unavailable.
	var session Session
	must(t, s.read(first, 'S', &session))
	must(t, os.Remove(s.object(session.Files[0])))
	matches, err := s.findFiles("report", first, "")
	must(t, err)
	if len(matches) != 3 {
		t.Fatal(matches)
	}
	var out bytes.Buffer
	must(t, s.find([]string{first[:8], "docs", "report"}, &out))
	for _, want := range []string{first[:8], "docs/deep/REPORT.pdf", " B"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	if err = s.find([]string{first, "../docs", "report"}, &out); err == nil {
		t.Fatal("invalid scope accepted")
	}
}

func TestBrowserSearchScopeLocateAndRestore(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "docs", "deep", "Отчёт.txt"), []byte("hello"))
	writeFile(t, filepath.Join(source, "other", "Отчёт.txt"), []byte("other"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: refs}
	u.key("n")
	for _, r := range "ОТЧЁТ" {
		u.key(string(r))
	}
	u.key("enter")
	if u.search == nil || len(u.search.matches) != 2 {
		t.Fatal("global search failed")
	}
	if view := u.view(100, 24); !strings.Contains(view, "All sessions") || !strings.Contains(view, "Отчёт.txt") {
		t.Fatal(view)
	}
	destination := filepath.Join(root, "restored")
	u.key("t")
	for _, r := range destination {
		u.key(string(r))
	}
	u.key("enter")
	if string(readFile(t, filepath.Join(destination, "docs", "deep", "Отчёт.txt"))) != "hello" {
		t.Fatal(u.message)
	}
	u.key("right")
	if u.search != nil || u.ref.Session != id || u.current != "docs/deep" || u.entries[u.cursor].name != "Отчёт.txt" {
		t.Fatal("locate failed")
	}
	u.key("left") // docs
	if u.current != "docs" {
		t.Fatal(u.current)
	}
	cursor := u.cursor
	u.key("n")
	u.key("ОТЧЁТ")
	u.key("enter")
	if len(u.search.matches) != 2 {
		t.Fatal("search should include all sessions and directories")
	}
	u.key("left")
	if u.current != "docs" || u.cursor != cursor {
		t.Fatal("cancel lost position")
	}
	u.key("left") // session root
	u.key("n")
	u.key("missing")
	u.key("enter")
	if len(u.search.matches) != 0 || !strings.Contains(u.view(100, 24), "No matching files") {
		t.Fatal("empty result")
	}
	u.key("right")
	u.key("r")
	u.key("left")
	u.key("n")
	u.key("q")
	u.key("cancel")
	if u.searchInput != nil {
		t.Fatal("cancel failed")
	}
	key, err := readBrowserKey(bufio.NewReader(strings.NewReader("\x1b")))
	must(t, err)
	if key != "left" {
		t.Fatal(key)
	}
}

func TestBrowserSearchKeyboard(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "qОтчёт.txt"), []byte("hello"))
	s := newStore(t, filepath.Join(root, "store"))
	_, err := s.backup(source, false)
	must(t, err)
	var output bytes.Buffer
	// Literal q and Unicode belong to the query; Esc then q closes search and quits.
	must(t, s.browse(strings.NewReader("nqОТЧЁТ\n\x1bq"), &output, nil))
	if !strings.Contains(output.String(), "qОтчёт.txt") {
		t.Fatal(output.String())
	}
}
