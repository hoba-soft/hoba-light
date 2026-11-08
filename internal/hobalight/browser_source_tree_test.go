package hobalight

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestBrowserSourceTreeCompressesSharedPaths(t *testing.T) {
	names := []string{"/home/me/Docs", "/home/me/Docs/archive/2025", "/home/me/Photos", "/home/me/Projects/client/app"}
	want := []browserSourceRow{
		{"/home/me/", -1},
		{"├─ Docs", 0},
		{"│  └─ archive/2025", 1},
		{"├─ Photos", 2},
		{"└─ Projects/client/app", 3},
	}
	if got := browserSourceTree(names); !reflect.DeepEqual(got, want) {
		t.Fatalf("tree:\n%#v\nwant:\n%#v", got, want)
	}
}

func TestBrowserSourceTreePreservesFilesystemRoots(t *testing.T) {
	for _, tc := range []struct {
		names []string
		want  []browserSourceRow
	}{
		{[]string{"/", "/etc", "/var/log"}, []browserSourceRow{{"/", 0}, {"├─ etc", 1}, {"└─ var/log", 2}}},
		{[]string{`C:\Users\me\Docs`, `C:\Users\me\Photos`, `D:\Backup`}, []browserSourceRow{{`C:\Users\me\`, -1}, {"├─ Docs", 0}, {"└─ Photos", 1}, {`D:\Backup`, 2}}},
		{[]string{`\\server\share`, `\\server\share\Docs`, `\\server\share\Photos`}, []browserSourceRow{{`\\server\share`, 0}, {"├─ Docs", 1}, {"└─ Photos", 2}}},
		{[]string{"C:/", "C:/Docs", "C:/Photos"}, []browserSourceRow{{"C:/", 0}, {"├─ Docs", 1}, {"└─ Photos", 2}}},
		{[]string{"/data/name\\with-backslash", "/data/other"}, []browserSourceRow{{"/data/", -1}, {"├─ name\\with-backslash", 0}, {"└─ other", 1}}},
	} {
		if got := browserSourceTree(tc.names); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q:\n%#v\nwant:\n%#v", tc.names, got, tc.want)
		}
	}
}

func TestBrowserSourceTreeNavigationSkipsGroups(t *testing.T) {
	// Lexical path order differs from tree order for a root with descendants.
	paths := []string{"/data/a", "/data/a-extra", "/data/a/child", "/data/z"}
	var refs []Reference
	for i, p := range paths {
		refs = append(refs, Reference{Directory: p, Session: fmt.Sprintf("%064dS", i)})
	}
	u := browserUI{sources: true, refs: refs}
	want := []string{"/data/a", "/data/a/child", "/data/a-extra", "/data/z"}
	if got := u.sourceNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("navigation order: %q", got)
	}
	if u.count() != len(want) {
		t.Fatal("group counted as source")
	}
	for i, p := range want {
		u.cursor = i
		rows := u.sourceList(100, 20)
		selected := 0
		for _, row := range rows {
			if strings.HasPrefix(row, ">") {
				selected++
			}
		}
		if selected != 1 {
			t.Fatalf("selection count %d: %q", selected, rows)
		}
		u.enter()
		if u.sources || u.source != p || u.count() != 1 {
			t.Fatalf("wrong root opened: %s", u.source)
		}
		u.back()
		if !u.sources || u.cursor != i {
			t.Fatalf("lost source cursor %d: %d", i, u.cursor)
		}
	}
	applyBrowserFilter(&u, "child")
	if u.count() != 1 || u.sourceNames()[0] != "/data/a/child" {
		t.Fatal("filter did not select nested root")
	}
	u.key("right")
	if u.source != "/data/a/child" {
		t.Fatal("filtered enter used group index")
	}
	u.back()
	if u.cursor != 1 {
		t.Fatalf("filtered navigation lost original index: %d", u.cursor)
	}
}

func TestBrowserSourceTreeScrollKeepsSelectionVisible(t *testing.T) {
	var refs []Reference
	for i := 0; i < 40; i++ {
		refs = append(refs, Reference{Directory: fmt.Sprintf("/data/group%02d/backup", i), Session: fmt.Sprintf("%064dS", i)})
	}
	u := browserUI{sources: true, refs: refs}
	for cursor := range refs {
		u.cursor = cursor
		rows := u.sourceList(60, 5)
		if len(rows) > 5 {
			t.Fatalf("viewport overflow: %d", len(rows))
		}
		selected := ""
		for _, row := range rows {
			if strings.HasPrefix(row, ">") {
				selected = row
			}
		}
		if !strings.Contains(selected, fmt.Sprintf("group%02d/backup", cursor)) {
			t.Fatalf("selected root invisible: %d: %q", cursor, rows)
		}
	}
}

func TestBrowserFrameSelectionStaysOutside(t *testing.T) {
	u := browserUI{sources: true, refs: []Reference{{Directory: "/data/Docs", Session: strings.Repeat("a", 64) + "S"}, {Directory: "/data/Photos", Session: strings.Repeat("b", 64) + "S"}}}
	view := u.view(80, 24)
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, ">") {
			t.Fatalf("selection broke border: %q", line)
		}
	}
	if !strings.Contains(view, "│ /data/") || !strings.Contains(view, "│>├─ Docs") || !strings.Contains(view, "│ └─ Photos") {
		t.Fatal(view)
	}
	if strings.Count(view, "/data/") != 1 {
		t.Fatal("common path repeated", view)
	}
}
