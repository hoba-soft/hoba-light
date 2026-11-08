package hobalight

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestDisplaySize(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want string
	}{{0, "0 B"}, {512, "512 B"}, {1024, "1.0 KiB"}, {1536, "1.5 KiB"}, {1048576, "1.0 MiB"}, {1 << 40, "1.0 TiB"}} {
		if got := displaySize(tc.size); got != tc.want {
			t.Errorf("%d: %s != %s", tc.size, got, tc.want)
		}
	}
}

func TestDirectorySizesAreRecursive(t *testing.T) {
	files := []sessionFile{
		{relative: "one", metadata: File{Identity: Identity{Size: 10}}},
		{relative: "nested/two", metadata: File{Identity: Identity{Size: 20}}},
		{relative: "nested/deep/three", metadata: File{Identity: Identity{Size: 30}}},
		{relative: "nested-other/four", metadata: File{Identity: Identity{Size: 40}}},
	}
	if got := filesSize(files, "nested"); got != 50 {
		t.Fatal(got)
	}
	if got := filesSize(files, ""); got != 100 {
		t.Fatal(got)
	}
	entries := browserEntries(files, "")
	for _, entry := range entries {
		if entry.name == "nested" && entry.size != 50 {
			t.Fatal(entry)
		}
	}
}

func TestSizeColumnAlignmentAndClipping(t *testing.T) {
	for _, width := range []int{16, 30, 80} {
		for _, name := range []string{"a.txt", "中文/", "a\u0301bc.txt", strings.Repeat("long filename ", 30)} {
			for _, size := range []string{"0 B", "123.4 MiB"} {
				line := sizeColumn(name, size, width)
				if runewidth.StringWidth(line) != width || !strings.HasSuffix(line, size) {
					t.Fatalf("width %d: %q", width, line)
				}
			}
		}
	}
}

func TestBrowserSizeColumnStaysVisible(t *testing.T) {
	f := sessionFile{id: strings.Repeat("a", 64) + "F", metadata: File{Identity: Identity{Size: 2048}}}
	u := browserUI{ref: &Reference{Session: strings.Repeat("b", 64) + "S"}, entries: []browserEntry{
		{name: strings.Repeat("long", 40), file: &f, size: 2048},
		{name: "中文.txt", file: &f, size: 3},
	}}
	view := u.view(50, 24)
	found := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "2.0 KiB") || strings.Contains(line, "3 B") {
			if runewidth.StringWidth(line) > 50 {
				t.Fatal(line)
			}
			found++
		}
	}
	if found != 2 {
		t.Fatal(view)
	}
}

func TestHashAndSizeColumnsAlign(t *testing.T) {
	previousHashEnd := -1
	for _, row := range []struct{ name, hash, size string }{
		{"短.txt", "12345678", "3 B"},
		{strings.Repeat("long", 30), "abcdef01", "12.5 MiB"},
		{"folder/", "", "1.0 KiB"},
	} {
		line := hashSizeColumn(row.name, row.hash, row.size, 60, 10)
		if runewidth.StringWidth(line) != 60 || !strings.HasSuffix(strings.TrimSpace(line), row.size) {
			t.Fatal(line)
		}
		if row.hash != "" {
			end := runewidth.StringWidth(line[:strings.Index(line, row.hash)+len(row.hash)])
			if previousHashEnd >= 0 && end != previousHashEnd {
				t.Fatal("hash columns differ", line)
			}
			previousHashEnd = end
		} else if strings.ContainsAny(line, "()") {
			t.Fatal(line)
		}
	}
}
