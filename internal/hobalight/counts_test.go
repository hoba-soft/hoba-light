package hobalight

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestCountsInListingsAndBrowser(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	for _, name := range []string{"one.txt", "nested/two.txt", "nested/deep/three.txt", "nested-other/four.txt"} {
		writeFile(t, filepath.Join(source, filepath.FromSlash(name)), []byte("hello"))
	}
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	_, files, err := s.listingFiles(id)
	must(t, err)
	for _, tree := range []map[string][]browserEntry{browserTree(files), {"": browserEntries(files, "")}} {
		for _, entry := range tree[""] {
			if entry.name == "nested" && entry.count != 2 {
				t.Fatal(entry)
			}
		}
	}
	// Counts and sizes remain available from references without F objects.
	must(t, os.Remove(s.object(files[0].id)))
	for _, tc := range []struct {
		args []string
		want string
	}{{nil, "Files"}, {[]string{id}, "4 files"}, {[]string{id, "nested"}, "2 files"}} {
		var out bytes.Buffer
		must(t, s.list(tc.args, &out))
		if !strings.Contains(out.String(), tc.want) {
			t.Fatal(out.String())
		}
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.HasPrefix(line, "one.txt") && strings.Contains(line, "file") {
				t.Fatal(line)
			}
		}
	}
	u := browserUI{browser: browser{store: s}, refs: refs}
	if view := u.view(100, 24); !strings.Contains(strings.Join(strings.Fields(view), " "), "4 0 20 B") {
		t.Fatal(view)
	}
	u.enter()
	if view := u.view(100, 24); !strings.Contains(view, "4 files  20 B") || !strings.Contains(strings.Join(strings.Fields(view), " "), "2 0 10 B") {
		t.Fatal(view)
	}
	// Same-prefix sibling must not inflate a directory's recursive count.
	if filesCount(files, "nested") != 2 {
		t.Fatal("incorrect scoped count")
	}
}

func TestCountColumnsWithLongNames(t *testing.T) {
	file := sessionFile{id: strings.Repeat("a", 64) + "F"}
	entries := []browserEntry{{name: strings.Repeat("目录", 30), directory: true, count: 42, size: 4096}, {name: strings.Repeat("document", 20) + ".pdf", file: &file, count: 1, size: 123}}
	table := newListingTable(nil, []tableStats{entryStats(entries[0]), entryStats(entries[1])})
	for _, entry := range entries {
		name, hash := entryColumns(entry)
		line := table.row(name, hash, entryStats(entry), 60)
		if runewidth.StringWidth(line) != 60 || !strings.HasSuffix(line, displaySize(entry.size)) {
			t.Fatal(line)
		}
		if entry.directory && !strings.Contains(strings.Join(strings.Fields(line), " "), "42 0 4.0 KiB") {
			t.Fatal(line)
		}
		if !entry.directory && (!strings.Contains(line, "aaaaaaaa") || strings.Contains(line, "files")) {
			t.Fatal(line)
		}
	}
}

func TestEmptySessionCounts(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "empty")
	must(t, os.Mkdir(source, 0700))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	for _, args := range [][]string{nil, {id}} {
		var out bytes.Buffer
		must(t, s.list(args, &out))
		if !strings.Contains(out.String(), "Files") && !strings.Contains(out.String(), "0 files") || !strings.Contains(out.String(), "0 B") {
			t.Fatal(out.String())
		}
	}
}
