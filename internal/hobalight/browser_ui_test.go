package hobalight

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestBrowserArrowKeys(t *testing.T) {
	for _, tc := range []struct{ sequence, want string }{{"\x1b[A", "up"}, {"\x1b[B", "down"}, {"\x1b[C", "right"}, {"\x1b[D", "left"}, {"\x1bOA", "up"}, {"\r", "right"}, {"\x03", "ctrl+c"}, {"r", "r"}, {"o", "o"}, {"\x1b[6~", "pagedown"}} {
		got, err := readBrowserKey(bufio.NewReader(strings.NewReader(tc.sequence)))
		must(t, err)
		if got != tc.want {
			t.Fatalf("%q: %s", tc.sequence, got)
		}
	}
}
func TestBrowserGroupedSelection(t *testing.T) {
	refs := []Reference{{Session: strings.Repeat("a", 64) + "S", Directory: "newest"}, {Session: strings.Repeat("b", 64) + "S", Directory: "oldest"}}
	u := browserUI{refs: refs}
	view := u.view(100, 24)
	if !strings.Contains(strings.Join(strings.Fields(view), " "), "> aaaaaaaa") || !strings.Contains(view, "  bbbbbbbb") {
		t.Fatal(view)
	}
	u.key("down")
	if u.cursor != 1 {
		t.Fatal("down failed")
	}
	u.key("down")
	if u.cursor != 1 {
		t.Fatal("selection escaped list")
	}
	u.key("up")
	if u.cursor != 0 {
		t.Fatal("up failed")
	}
	for i := 0; i < 40; i++ {
		u.refs = append(u.refs, Reference{Session: fmt.Sprintf("%064xS", i+1)})
	}
	u.key("end")
	view = u.view(100, 15)
	if !strings.Contains(view, fmt.Sprintf("%d/%d", len(u.refs), len(u.refs))) {
		t.Fatal("long list did not scroll")
	}
}
func TestBrowserBackPreservesDirectorySelection(t *testing.T) {
	f := sessionFile{relative: "nested/file", id: strings.Repeat("a", 64) + "F", metadata: File{Blob: strings.Repeat("b", 64) + "B"}}
	store := newStore(t, t.TempDir())
	data, err := encode(f.metadata)
	must(t, err)
	f.id = hash(data) + "F"
	must(t, store.put(f.id, data))
	u := browserUI{browser: browser{store: store}, ref: &Reference{}, files: []sessionFile{f}}
	u.entries = browserEntries(u.files, "")
	u.key("right")
	if u.current != "nested" {
		t.Fatal("did not enter directory")
	}
	u.key("right")
	if u.detail == nil {
		t.Fatal("did not show file details")
	}
	u.key("left")
	if u.detail != nil {
		t.Fatal("did not leave details")
	}
	u.key("left")
	if u.current != "" || u.cursor != 0 {
		t.Fatal("lost parent selection")
	}
}

func TestBrowserFileMetadata(t *testing.T) {
	created := "2020-01-02T03:04:05Z"
	f := sessionFile{relative: "nested/file.txt", id: strings.Repeat("a", 64) + "F", metadata: File{Identity: Identity{Path: "/home/person/documents/nested/file.txt", Size: 12345, Created: &created, Modified: "2026-10-18T12:00:00Z"}, Blob: strings.Repeat("b", 64) + "B"}}
	u := browserUI{ref: &Reference{Session: strings.Repeat("c", 64) + "S", Directory: "/home/person/documents"}, detail: &f, current: "nested"}
	view := u.view(120, 30)
	for _, want := range []string{"Path: /home/person/documents/nested/file.txt", "Size: 12345 bytes", "Created: " + created, "Modified: 2026-10-18T12:00:00Z", "File object: aaaaaaaa", "Blob object: bbbbbbbb"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q: %s", want, view)
		}
	}
	f.metadata.Created = nil
	if !strings.Contains(u.view(120, 30), "Created: unavailable") {
		t.Fatal("missing unavailable creation time")
	}
}

func TestBrowserDetailsDoNotLoseSelection(t *testing.T) {
	files := []sessionFile{{relative: "a", id: strings.Repeat("a", 64) + "F"}, {relative: "b", id: strings.Repeat("b", 64) + "F"}}
	store := newStore(t, t.TempDir())
	for i := range files {
		files[i].metadata.Path = files[i].relative
		files[i].metadata.Blob = strings.Repeat("c", 64) + "B"
		data, err := encode(files[i].metadata)
		must(t, err)
		files[i].id = hash(data) + "F"
		must(t, store.put(files[i].id, data))
	}
	u := browserUI{browser: browser{store: store}, ref: &Reference{}, files: files, cursor: 1}
	u.entries = browserEntries(files, "")
	u.key("right")
	u.key("down")
	u.key("left")
	if u.cursor != 1 || u.detail != nil {
		t.Fatal("lost selected file after leaving details")
	}
}

func TestBrowserContextualHelp(t *testing.T) {
	file := sessionFile{}
	u := browserUI{detail: &file}
	help := u.help()
	for _, bad := range []string{"enter", "select", "refresh"} {
		if strings.Contains(help, bad) {
			t.Fatalf("unavailable action %q in %q", bad, help)
		}
	}
	for _, want := range []string{"← back", "r restore", "o restore & open", "q quit"} {
		if !strings.Contains(help, want) {
			t.Fatal(help)
		}
	}
	u = browserUI{ref: &Reference{}, entries: []browserEntry{{directory: true}}}
	if help = u.help(); strings.Contains(help, "o restore & open") || !strings.Contains(help, "enter") {
		t.Fatal(help)
	}
	u = browserUI{refs: []Reference{{}, {}}}
	help = u.help()
	if strings.Contains(help, "← back") || strings.Contains(help, "o restore & open") {
		t.Fatalf("unavailable action at sessions root: %s", help)
	}
	for _, want := range []string{"↑/↓ select", "→/Enter enter", "r restore", "d compare", "f refresh", "q quit"} {
		if !strings.Contains(help, want) {
			t.Fatalf("missing action %q: %s", want, help)
		}
	}
	u = browserUI{}
	if help = u.help(); help != "f refresh · / search · q quit" {
		t.Fatal(help)
	}
}

func TestBrowserComparisonToggleAndOpen(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	name := filepath.Join(source, "file")
	writeFile(t, name, []byte("before"))
	s := newStore(t, filepath.Join(root, "store"))
	_, err := s.backup(source, false)
	must(t, err)
	writeFile(t, name, []byte("after content"))
	_, err = s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: refs, cursor: 1}
	u.key("d")
	if u.compareBase == nil || u.compareBase.id != u.refs[1].Session || !strings.Contains(u.view(100, 24), "[compare base]") {
		t.Fatal("first session not visibly selected")
	}
	u.key("d")
	if u.compareBase != nil || u.streamDiff != nil {
		t.Fatal("same session did not toggle off")
	}
	u.key("d")
	u.key("up")
	u.key("d")
	if u.streamDiff != nil {
		defer u.closeDiff()
		drainDiff(t, u.streamDiff.spool)
	}
	if u.streamDiff == nil || !strings.Contains(u.view(100, 24), "~ ") {
		t.Fatal("second session did not open comparison")
	}
	if !strings.Contains(u.view(100, 24), "┌") {
		t.Fatal("comparison has no content panel")
	}
	u.key("left")
	if u.streamDiff != nil || u.cursor != 0 {
		t.Fatal("comparison return lost selection")
	}
}

func TestBrowserVimDiffScrolling(t *testing.T) {
	u := browserUI{}
	spool, err := newDiffSpool(func(_ context.Context, w io.Writer) error {
		for i := 0; i < 100; i++ {
			if _, err := fmt.Fprintf(w, "line %03d %s\n", i, strings.Repeat("long", 40)); err != nil {
				return err
			}
		}
		return nil
	})
	must(t, err)
	defer spool.Close()
	u.streamDiff = &browserDiff{spool: spool}
	drainDiff(t, spool)

	u.view(60, 20)
	u.key("j")
	if u.diffOffset != 1 {
		t.Fatal("j did not scroll down")
	}
	u.key("k")
	if u.diffOffset != 0 {
		t.Fatal("k did not scroll up")
	}
	u.key("pagedown")
	if u.diffOffset != u.diffPageSize {
		t.Fatal("page down failed")
	}
	u.key("end")
	u.view(60, 20)
	if u.diffOffset != 100-u.diffPageSize {
		t.Fatal("G did not show last page")
	}
	u.key("home")
	if u.diffOffset != 0 {
		t.Fatal("g did not go to top")
	}
	u.key("l")
	if u.diffColumn != 4 {
		t.Fatal("horizontal scrolling failed")
	}
	u.key("h")
	if u.diffColumn != 0 {
		t.Fatal("horizontal return failed")
	}
	for _, tc := range []struct{ input, want string }{{"g", "home"}, {"G", "end"}, {"\x06", "pagedown"}, {"\x02", "pageup"}, {"\x04", "halfdown"}, {"\x15", "halfup"}} {
		got, err := readBrowserKey(bufio.NewReader(strings.NewReader(tc.input)))
		must(t, err)
		if got != tc.want {
			t.Fatalf("%q: %s", tc.input, got)
		}
	}
}

func TestBrowserPanelDisplayWidth(t *testing.T) {
	for _, name := range []string{"Dowo\u0301d Rejestracyjny.pdf", "Zas\u0301wiadczenie.pdf", "中文文件.pdf", "🙂 backup"} {
		for _, width := range []int{20, 80} {
			panel := browserPanel([]string{name, strings.Repeat(name, 5)}, width)
			for _, line := range panel {
				if got := runewidth.StringWidth(line); got != width-1 {
					t.Fatalf("width %d: %d columns in %q", width, got, line)
				}
			}
		}
	}
}

func TestBrowserHeaderOutsidePanel(t *testing.T) {
	u := browserUI{}
	view := u.view(100, 24)
	if strings.Index(view, "Sessions") > strings.Index(view, "┌") {
		t.Fatal(view)
	}
	if strings.Contains(view, "│ Hoba Light Browser") {
		t.Fatal(view)
	}
}

func TestBrowserScrollPreservesGraphemes(t *testing.T) {
	if got := browserSkipColumns("a\u0301bc", 1); got != "bc" {
		t.Fatalf("%q", got)
	}
	if got := browserSkipColumns("中文abc", 1); got != "文abc" {
		t.Fatalf("%q", got)
	}
	for _, line := range wrapBrowserText("a\u0301b\u0301c\u0301", 1) {
		if runewidth.StringWidth(line) != 1 || !strings.HasSuffix(line, "\u0301") {
			t.Fatalf("%q", line)
		}
	}
}

func TestBrowserGroupedScrollContext(t *testing.T) {
	u := browserUI{}
	for i := 0; i < 20; i++ {
		directory := "/Alpha"
		if i >= 10 {
			directory = "/Beta"
		}
		u.refs = append(u.refs, Reference{Directory: directory, Session: fmt.Sprintf("%064xS", i+1), Started: "2026-10-25T10:00:00Z"})
	}
	for i := range u.refs {
		u.cursor = i
		view := u.view(100, 17)
		if !strings.Contains(strings.Join(strings.Fields(view), " "), "> "+strings.Join(strings.Fields(sessionLabel(u.refs[i])), " ")) || !strings.Contains(view, u.refs[i].Directory) {
			t.Fatalf("selection %d missing context: %s", i, view)
		}
	}
}

func TestBrowserSessionSelectionInShortTerminal(t *testing.T) {
	refs := []Reference{
		{Session: strings.Repeat("a", 64) + "S", Directory: "one"},
		{Session: strings.Repeat("b", 64) + "S", Directory: "one"},
		{Session: strings.Repeat("c", 64) + "S", Directory: "two"},
	}
	for _, height := range []int{1, 10, 11, 12, 13} {
		for cursor, ref := range refs {
			u := browserUI{refs: refs, cursor: cursor}
			view := u.view(100, height)
			if !strings.Contains(strings.Join(strings.Fields(view), " "), "> "+ref.Session[:8]) {
				t.Fatalf("height %d, cursor %d lost selection: %s", height, cursor, view)
			}
		}
	}
}

func TestBrowserCompactViewsAndHelp(t *testing.T) {
	f := sessionFile{relative: "nested/file.txt", id: strings.Repeat("a", 64) + "F", metadata: File{Identity: Identity{Path: "/source/nested/file.txt", Size: 12}}}
	u := browserUI{ref: &Reference{Session: strings.Repeat("b", 64) + "S", Directory: "/source"}, files: []sessionFile{f}, current: "nested", detail: &f}
	view := u.view(100, 24)
	for _, unwanted := range []string{"Content object:", "Name:", "Directory:", "darwin/", "linux/", "windows/"} {
		if strings.Contains(view, unwanted) {
			t.Fatal(view)
		}
	}
	if strings.Count(view, "/source") != 1 || !strings.Contains(view, "? help") {
		t.Fatal(view)
	}
	u.key("?")
	if !strings.Contains(u.view(100, 24), "X delete file globally") {
		t.Fatal("missing advanced action")
	}
	u.key("left")
	if u.showHelp || u.detail != &f {
		t.Fatal("help changed selection")
	}
	u.detail = nil
	u.search = &browserSearch{query: "file", matches: []searchMatch{{ref: *u.ref, file: f}, {ref: *u.ref, file: f}}}
	view = u.view(100, 24)
	if strings.Count(view, "bbbbbbbb") != 1 || strings.Count(view, "/source") != 1 {
		t.Fatal(view)
	}
	if strings.Contains(view, "nested/file.txt") {
		t.Fatal("search repeated scope")
	}
	u.ref = nil
	view = u.view(100, 24)
	if strings.Count(view, "bbbbbbbb") != 2 {
		t.Fatal("global search lost sessions", view)
	}
}
func TestBrowserPathKeepsDestination(t *testing.T) {
	got := browserPath("/very/long/parent/directory/Документы", 20)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "/Документы") {
		t.Fatal(got)
	}
}

func TestComparisonBaseRemainsVisibleAcrossNavigation(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file.txt"), []byte("data"))
	_, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: refs}
	u.enter()
	u.key("d")
	if view := u.view(100, 24); !strings.Contains(view, "[compare base] file.txt") || !strings.Contains(view, "Compare base:") {
		t.Fatal(view)
	}
	base := *u.compareBase
	u.key("left")
	if view := u.view(100, 24); !strings.Contains(view, "Compare base: "+base.id[:8]) || !strings.Contains(view, "file.txt") {
		t.Fatal(view)
	}
	u.enter()
	u.key("d")
	if u.compareBase != nil || strings.Contains(u.view(100, 24), "Compare base:") {
		t.Fatal("base not cleared")
	}
}

func TestRefreshClearsComparisonAndTableCache(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("data"))
	_, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	u := browserUI{browser: browser{store: s}, refs: refs}
	u.view(100, 24)
	if u.sessionTable == nil {
		t.Fatal("session table not cached")
	}
	u.key("d")
	if u.compareBase == nil {
		t.Fatal("no comparison selected")
	}
	u.key("f")
	if u.compareBase != nil || u.compareBaseLabel != "" || u.sessionTable != nil {
		t.Fatal("refresh retained stale state")
	}
	u.enter()
	u.prepareTree()
	original := u.directoryTables[u.current]
	u.view(100, 24)
	if u.directoryTables[u.current] != original {
		t.Fatal("render changed cached layout")
	}
}
