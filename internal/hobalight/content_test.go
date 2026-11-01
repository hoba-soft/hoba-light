package hobalight

import (
	"bufio"
	"context"
	"errors"
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
