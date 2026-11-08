package hobalight

import (
	"bufio"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

func TestCalendarEscapeCancelsWithoutChangingFilter(t *testing.T) {
	day, _ := parseSessionDate("2024-02-29")
	u := browserUI{browser: browser{store: &Store{sessionDate: "2024-02-28"}}, datePicker: &day}
	reader := bufio.NewReader(strings.NewReader("\x1bq"))
	key, err := readCalendarKey(reader)
	must(t, err)
	if key != "cancel" {
		t.Fatalf("Esc decoded as %q", key)
	}
	u.key(key)
	if u.datePicker != nil || u.store.sessionDate != "2024-02-28" {
		t.Fatal("Esc did not cancel the picker")
	}
	key, err = readBrowserKey(reader)
	must(t, err)
	if key != "q" {
		t.Fatal("Esc consumed the following command")
	}
}
func TestBrowserModalHelpHasPriority(t *testing.T) {
	u := browserUI{sources: true, merged: &browserMerged{}, history: &browserHistory{}}
	if help := u.briefHelp(); !strings.Contains(help, "Enter metadata") || strings.Contains(help, "Enter sessions") {
		t.Fatal(help)
	}
	input := ""
	u.restoreToInput = &input
	if help := u.briefHelp(); !strings.Contains(help, "Enter restore") || !strings.Contains(help, "Esc cancel") {
		t.Fatal(help)
	}
	u.restoreToInput = nil
	u.pendingDelete = &Reference{}
	if help := u.briefHelp(); !strings.Contains(help, "y delete") {
		t.Fatal(help)
	}
}
func TestBrowserHistoryKeepsSizeVisibleAt80Columns(t *testing.T) {
	file := File{Identity: Identity{Size: 12345}}
	u := browserUI{history: &browserHistory{path: "/source/file", versions: []fileVersion{{id: strings.Repeat("a", 64) + "F", file: file, earliest: time.Now()}}}}
	view := u.view(80, 24)
	if !strings.Contains(view, displaySize(file.Size)) {
		t.Fatal(view)
	}
	for _, line := range strings.Split(view, "\n") {
		if runewidth.StringWidth(line) > 80 {
			t.Fatal("line exceeds terminal width", line)
		}
	}
}
func TestMergedHomeAvoidsEmptyAncestorNavigation(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	for _, folder := range []string{"Documents", "Photos"} {
		source := filepath.Join(root, "sources", folder)
		writeFile(t, filepath.Join(source, "file"), []byte("contents"))
		_, err := s.backup(source, false)
		must(t, err)
	}
	m, err := s.mergedBrowser()
	must(t, err)
	if m.current.fullPath != portable(filepath.Join(root, "sources")) || len(m.entries()) != 2 {
		t.Fatal("merged browser did not start at useful common ancestor")
	}
	node, err := m.locateFile(portable(filepath.Join(root, "sources", "Documents", "file")))
	must(t, err)
	if node == nil {
		t.Fatal("missing selected file")
	}
	u := browserUI{merged: m}
	u.mergedKey("left")
	if m.current != m.home {
		t.Fatal("back did not return to common ancestor")
	}
	u.mergedKey("left")
	if m.current != m.home {
		t.Fatal("back escaped the merged home unexpectedly")
	}
}

func TestHistoryHelpAboveUnderlyingMetadata(t *testing.T) {
	u := browserUI{detail: &sessionFile{}, history: &browserHistory{}}
	if help := u.briefHelp(); !strings.Contains(help, "Enter sessions") {
		t.Fatal(help)
	}
	if help := u.help(); !strings.Contains(help, "Enter sessions/locate") {
		t.Fatal(help)
	}
}
func TestLocateFileReturnsToContainingSession(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("contents"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	session, files, err := s.sessionFiles(id)
	must(t, err)
	u := browserUI{browser: browser{store: s}}
	u.locateMatch(searchMatch{ref: Reference{Session: id, Directory: session.Directory, Started: session.Started}, file: files[0]})
	u.enter()
	if u.detail == nil {
		t.Fatal("file metadata did not open")
	}
	u.back()
	if u.ref == nil || u.detail != nil {
		t.Fatal("back skipped the containing session after locating a file")
	}
}
