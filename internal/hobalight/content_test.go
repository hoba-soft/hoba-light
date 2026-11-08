package hobalight

import (
	"bufio"
	"context"
	"errors"
	"fmt"

	"github.com/mattn/go-runewidth"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestContentModes(t *testing.T) {
	for _, tc := range []struct{ name, data, mode, want string }{
		{"text", "hello\nмир\n", "auto", "Content (text):\nhello\nмир\n"},
		{"binary", "\x00\x01abc", "auto", "00 01 61 62 63"},
		{"force hex", "hello", "hex", "68 65 6c 6c 6f"},
		{"no newline", "hello", "text", "hello\n\\ No newline at end of file"},
		{"empty", "", "auto", "(empty file)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			s := newStore(t, filepath.Join(root, "store"))
			source := filepath.Join(root, "source")
			writeFile(t, filepath.Join(source, "file"), []byte(tc.data))
			id, err := s.backup(source, false)
			must(t, err)
			_, files, err := s.sessionFiles(id)
			must(t, err)
			var out strings.Builder
			must(t, s.content(context.Background(), files[0].id, tc.mode, &out))
			if !strings.Contains(out.String(), tc.want) {
				t.Fatal(out.String())
			}
			if err = s.content(context.Background(), files[0].id, "text", rejectingDiffWriter{}); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal(err)
			}
		})
	}
}
func TestContentUTF8Fragments(t *testing.T) {
	source := strings.Repeat("a", 4079) + strings.Repeat("🙂ж", 10000) + "\nlast"
	r := bufio.NewReaderSize(strings.NewReader(source), diffChunk)
	var joined strings.Builder
	fragments := 0
	for {
		record, err := nextContentRecord(r)
		if err == io.EOF {
			break
		}
		must(t, err)
		if !utf8.ValidString(record.data) || len(record.data) > diffChunk-16 {
			t.Fatal("broken UTF-8 or unbounded row")
		}
		joined.WriteString(record.data)
		if record.fragment {
			fragments++
		}
	}
	if joined.String() != source || fragments == 0 {
		t.Fatal("fragmented content lost")
	}
}
func TestContentUnavailable(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	f := File{Identity: Identity{Path: "/file", Error: "access denied"}}
	data, err := encode(f)
	must(t, err)
	id := hash(data) + "F"
	must(t, s.put(id, data))
	if err = s.content(context.Background(), id, "auto", io.Discard); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatal(err)
	}
}

func TestBrowserContentDoesNotInterpretBodyAsFrame(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, filepath.Join(root, "store"))
	source := filepath.Join(root, "source")
	// These literal content rows must never be interpreted as UI structure.
	data := "Content (text):\n" + strings.Repeat("─", 97) + "\n" + strings.Repeat("ordinary content\n", 30)
	writeFile(t, filepath.Join(source, "file"), []byte(data))
	id, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(id)
	must(t, err)
	for _, mode := range []string{"text", "hex"} {
		t.Run(mode, func(t *testing.T) {
			u := browserUI{browser: browser{store: s}}
			u.startContent(files[0].id, mode)
			defer u.closeDiff()
			drainDiff(t, u.streamDiff.spool)
			for _, offset := range []int{0, 1, 2, 3} {
				u.diffOffset = offset
				view := u.view(100, 24)
				rules := strings.Count(view, "\n├")
				want := 0

				if rules != want {
					t.Fatalf("offset %d: want %d rules, got %d:\n%s", offset, want, rules, view)
				}
				if offset == 0 && !strings.Contains(view, "Content ("+mode+"):") {
					t.Fatal(view)
				}
				if len(strings.Split(strings.TrimSuffix(view, "\n"), "\n")) >= 24 {
					t.Fatal("content overflowed terminal", view)
				}
			}
			u.diffOffset = 0
			u.view(100, 24)
			page := u.diffPageSize
			last := u.streamDiff.spool.row(page - 1)
			u.streamDiffKey("pagedown")
			if u.diffOffset != page {
				t.Fatal("separator changed paging indices")
			}
			next := u.view(100, 24)
			if strings.Count(next, "\n├") != 0 || last == "" {
				t.Fatal(next)
			}
		})
	}
}

func TestBrowserContentViewportAndBodySearch(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	source := filepath.Join(t.TempDir(), "metadata-only-name")
	var data strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&data, "body row %02d with enough text for horizontal scrolling\n", i)
	}
	writeFile(t, filepath.Join(source, "file"), []byte(data.String()))
	id, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(id)
	must(t, err)
	u := browserUI{browser: browser{store: s}}
	u.startContent(files[0].id, "text")
	defer u.closeDiff()
	drainDiff(t, u.streamDiff.spool)
	if u.streamDiff.spool.count != 40 || u.streamDiff.spool.row(0) != "body row 00 with enough text for horizontal scrolling" {
		t.Fatal("metadata entered content row indices")
	}
	initial := u.view(100, 24)
	if !strings.Contains(initial, "Content (text): file") || strings.Contains(initial, "│ File:") || strings.Contains(initial, "│ Size:") {
		t.Fatal(initial)
	}
	page := u.diffPageSize
	u.diffOffset = page - 1
	u.view(100, 24)
	if u.streamDiff.contentTop != 0 {
		t.Fatal("viewport moved before cursor reached edge")
	}
	u.streamDiffKey("down")
	u.view(100, 24)
	if u.streamDiff.contentTop != 1 {
		t.Fatal("viewport did not follow cursor across edge")
	}
	u.streamDiffKey("home")
	u.view(100, 24)
	u.streamDiffKey("pagedown")
	u.view(100, 24)
	if u.streamDiff.contentTop != page || u.diffOffset != page {
		t.Fatal("page navigation did not advance whole window")
	}
	for _, key := range []string{"down", "pagedown", "l", "end", "home"} {
		u.streamDiffKey(key)
		view := u.view(100, 24)
		if strings.Contains(view, "│ File:") || strings.Contains(view, "│ Size:") {
			t.Fatal("metadata consumes content viewport", view)
		}
		if key == "end" && (u.diffOffset != 39 || !strings.Contains(view, "row 39")) {
			t.Fatal("End did not reach content end", view)
		}
		if key == "home" && u.diffOffset != 0 {
			t.Fatal("Home did not reach first body row")
		}
	}
	u.diffColumn = 0
	search := func(query string) {
		u.diffOffset = 0
		u.streamDiffKey("/")
		*u.streamDiff.input = query
		u.streamDiffKey("enter")
	}
	search("body row 00")
	if u.diffOffset != 0 || !strings.HasPrefix(u.message, "Match:") {
		t.Fatal("search skipped first body row", u.message)
	}
	search("metadata-only-name")
	if u.message != "No further matches" {
		t.Fatal("search matched pinned metadata", u.message)
	}
	for _, width := range []int{40, 60, 100} {
		for _, height := range []int{18, 24, 40} {
			view := u.view(width, height)
			if len(strings.Split(strings.TrimSuffix(view, "\n"), "\n")) >= height {
				t.Fatalf("%dx%d overflow:\n%s", width, height, view)
			}
			for _, row := range strings.Split(view, "\n") {
				if runewidth.StringWidth(row) > width {
					t.Fatal("width overflow", row)
				}
			}
		}
	}
	u.streamDiffKey("x")
	drainDiff(t, u.streamDiff.spool)
	view := u.view(100, 24)
	if u.diffOffset != 0 || u.diffColumn != 0 || !strings.Contains(view, "Content (hex):") || u.streamDiff.spool.count != (data.Len()+15)/16 {
		t.Fatal("hex toggle mixed header and body", view)
	}
	u.streamDiffKey("t")
	drainDiff(t, u.streamDiff.spool)
	if u.streamDiff.spool.count != 40 {
		t.Fatal("text toggle included metadata")
	}
}

func TestBrowserContentEmptyAndUnterminatedCounts(t *testing.T) {
	for _, data := range []string{"", "one line without a newline"} {
		t.Run(fmt.Sprintf("bytes%d", len(data)), func(t *testing.T) {
			s := newStore(t, filepath.Join(t.TempDir(), "store"))
			source := filepath.Join(t.TempDir(), "source")
			writeFile(t, filepath.Join(source, "file"), []byte(data))
			id, err := s.backup(source, false)
			must(t, err)
			_, files, err := s.sessionFiles(id)
			must(t, err)
			u := browserUI{browser: browser{store: s}}
			u.startContent(files[0].id, "auto")
			defer u.closeDiff()
			drainDiff(t, u.streamDiff.spool)
			view := u.view(100, 24)
			if data == "" {
				if u.streamDiff.spool.count != 0 || !strings.Contains(view, "0 lines") || !strings.Contains(view, "(empty file)") || strings.Contains(view, "│>") {
					t.Fatal(view)
				}
			} else if u.streamDiff.spool.count != 1 || !strings.Contains(view, "line 1/1") {
				t.Fatal(view)
			}
		})
	}
}

func TestBrowserThreeLineContentKeepsStableFullscreenViewport(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	source := filepath.Join(t.TempDir(), "source")
	writeFile(t, filepath.Join(source, "three.txt"), []byte("alpha\nbeta\ngamma\n"))
	id, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(id)
	must(t, err)
	u := browserUI{browser: browser{store: s}}
	u.startContent(files[0].id, "text")
	defer u.closeDiff()
	drainDiff(t, u.streamDiff.spool)
	for _, key := range []string{"home", "down", "down", "down", "up", "end", "pagedown", "home"} {
		u.streamDiffKey(key)
		view := u.view(100, 24)
		rows := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
		if u.streamDiff.contentTop != 0 || len(rows) != 23 || !strings.HasPrefix(rows[21], "└") {
			t.Fatalf("%s changed viewport size or top:\n%s", key, view)
		}
		for i, line := range []string{"alpha", "beta", "gamma"} {
			if !strings.Contains(rows[4+i], line) {
				t.Fatalf("%s moved %s off its row:\n%s", key, line, view)
			}
		}
		if strings.Count(view, "│>") != 1 {
			t.Fatal("cursor missing", view)
		}
	}
}
