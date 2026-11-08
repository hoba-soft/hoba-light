package hobalight

import (
	"fmt"
	"github.com/mattn/go-runewidth"
	"strings"
	"testing"
)

func assertBrowserColumn(t *testing.T, heading, row, label, value string) {
	t.Helper()
	pos := strings.Index(heading, label)
	if pos < 0 {
		t.Fatalf("missing heading %q in %q", label, heading)
	}
	column := runewidth.StringWidth(heading[:pos])
	if label != "Root" && label != "Name" && label != "Started" {
		column += runewidth.StringWidth(label) - runewidth.StringWidth(value)
	}
	got := runewidth.Truncate(browserSkipColumns(row, column), runewidth.StringWidth(value), "")
	if got != value {
		t.Fatalf("%s starts in column %d: want %q got %q\n%s\n%s", label, column, value, got, heading, row)
	}
}

func TestBrowserRootColumnsReachFrameEdge(t *testing.T) {
	ref := Reference{Session: strings.Repeat("a", 64) + "S", Directory: "/demo/Documents"}
	for _, width := range []int{60, 100, 160} {
		u := browserUI{sources: true, refs: []Reference{ref}}
		lines := strings.Split(u.view(width, 24), "\n")
		heading, row := lines[4], lines[6]
		if !strings.HasPrefix(heading, "│ Root") || !strings.HasPrefix(row, "│>/demo") || !strings.HasSuffix(heading, "Sessions│") || !strings.HasSuffix(row, "1│") {
			t.Fatalf("columns do not meet the frame at width %d: %s", width, u.view(width, 24))
		}
		assertBrowserColumn(t, heading, row, "Root", ref.Directory)
		assertBrowserColumn(t, heading, row, "Sessions", "1")
	}
}

func TestBrowserColumnCoordinatesAcrossLists(t *testing.T) {
	ref := Reference{Session: strings.Repeat("a", 64) + "S", Directory: "/demo", Started: "2026-11-08T12:00:00Z", Comment: "Before update"}
	u := browserUI{source: ref.Directory, refs: []Reference{ref}, stats: map[string]tableStats{ref.Session: numericStats(12, 3, 123)}}
	rows := u.sessionList(120, 10)
	for _, pair := range [][2]string{{"Started", sessionTime(ref)}, {"Session", ref.Session[:8]}, {"Files", "12"}, {"Errors", "3"}, {"Size", "123 B"}, {"Comment", ref.Comment}} {
		assertBrowserColumn(t, rows[0], rows[1], pair[0], pair[1])
	}
	file := sessionFile{id: strings.Repeat("b", 64) + "F", relative: "目录.txt"}
	u = browserUI{ref: &ref, entries: []browserEntry{{name: file.relative, file: &file, count: 1, size: 123, errors: 0}}}
	rows = u.directoryList(120, 10)
	for _, pair := range [][2]string{{"Name", file.relative}, {"Hash", file.id[:8]}, {"Files", "—"}, {"Errors", "0"}, {"Size", "123 B"}} {
		assertBrowserColumn(t, rows[0], rows[1], pair[0], pair[1])
	}
}

func TestBrowserFrameKeepsTitleBreadcrumbAndHelpOutside(t *testing.T) {
	file := sessionFile{id: strings.Repeat("b", 64) + "F", relative: "notes.txt", metadata: File{Identity: Identity{Path: "/demo/notes.txt"}, Blob: strings.Repeat("c", 64) + "B"}}
	for _, width := range []int{60, 100, 160} {
		u := browserUI{ref: &Reference{Session: strings.Repeat("a", 64) + "S", Directory: "/demo"}, detail: &file}
		lines := strings.Split(strings.TrimSuffix(u.view(width, 24), "\n"), "\n")
		top, bottom := -1, -1
		for i, line := range lines {
			if strings.HasPrefix(line, "┌") {
				top = i
			}
			if strings.HasPrefix(line, "└") {
				bottom = i
			}
			if runewidth.StringWidth(line) > width {
				t.Fatal(line)
			}
		}
		if top < 3 || bottom <= top || bottom >= len(lines)-1 || lines[top-1] == "" || !strings.Contains(lines[0], "hoba-light browser") || lines[1] != "" || !strings.Contains(lines[bottom+1], "back") {
			t.Fatal(lines)
		}
		for _, line := range lines[top : bottom+1] {
			if runewidth.StringWidth(line) != width {
				t.Fatal("uneven frame", line)
			}
		}
	}
}

func TestBrowserAllTablesUseRightEdge(t *testing.T) {
	ref := Reference{Session: strings.Repeat("a", 64) + "S", Directory: "/demo", Started: "2026-11-08T12:00:00Z", Comment: "Backup"}
	file := sessionFile{id: strings.Repeat("b", 64) + "F", relative: "目录.txt", metadata: File{Identity: Identity{Size: 123}}}
	match := searchMatch{ref: ref, file: file}
	for _, width := range []int{60, 100, 160} {
		for _, tc := range []struct {
			name, heading, value string
			u                    browserUI
		}{
			{"roots", "Sessions", "1", browserUI{sources: true, refs: []Reference{ref}}},
			{"sessions", "Comment", "Backup", browserUI{source: ref.Directory, refs: []Reference{ref}}},
			{"directory", "Size", "123 B", browserUI{ref: &ref, files: []sessionFile{file}}},
			{"merged", "Type", "versions", browserUI{merged: &browserMerged{current: &mergedNode{children: map[string]*mergedNode{"目录.txt": {name: "目录.txt", file: true}}}}}},
			{"search", "Comment", "Backup", browserUI{search: &browserSearch{matches: []searchMatch{match}}}},
			{"history", "Size", "123 B", browserUI{history: &browserHistory{versions: []fileVersion{{id: file.id, file: file.metadata, sessions: []searchMatch{match}}}}}},
			{"history sessions", "Comment / Source", "Backup /demo", browserUI{history: &browserHistory{sessions: true, versions: []fileVersion{{id: file.id, file: file.metadata, sessions: []searchMatch{match}}}}}},
		} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, width), func(t *testing.T) {
				u := tc.u
				if u.ref != nil {
					u.prepareTree()
					u.entries = u.tree[""]
				}
				view := u.view(width, 24)
				lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
				var heading, selected string
				for _, line := range lines {
					if strings.HasPrefix(line, "│ ") && strings.Contains(line, tc.heading) {
						heading = line
					}
					if strings.HasPrefix(line, "│>") {
						selected = line
					}
					if strings.ContainsAny(line, "│┌└├") && runewidth.StringWidth(line) != width {
						t.Fatalf("uneven frame: %q", line)
					}
				}
				if !strings.HasSuffix(heading, tc.heading+"│") || !strings.HasSuffix(selected, tc.value+"│") {
					t.Fatalf("trailing column does not reach frame edge:\n%s", view)
				}
				assertBrowserColumn(t, heading, selected, tc.heading, tc.value)
			})
		}
	}
}

func TestBrowserSessionTrailingColumnsStayGrouped(t *testing.T) {
	ref := Reference{Session: strings.Repeat("a", 64) + "S", Directory: "/demo", Comment: "Short"}
	u := browserUI{source: ref.Directory, refs: []Reference{ref}}
	a, b := u.sessionList(120, 10)[0], u.sessionList(180, 10)[0]
	for _, heading := range []string{"Session", "Files", "Errors", "Size", "Comment"} {
		if strings.Index(b, heading)-strings.Index(a, heading) != 60 {
			t.Fatalf("%s did not move with right edge:\n%s\n%s", heading, a, b)
		}
	}
}

func TestBrowserBreadcrumbWrapPreservesTextAndWords(t *testing.T) {
	text := "/demo/目录/notes › aaaaaaaa — A useful backup comment with details"
	lines := wrapBrowserBreadcrumb(text, 40)
	if strings.Join(lines, "") != text {
		t.Fatalf("breadcrumb changed: %q", lines)
	}
	for _, line := range lines {
		if runewidth.StringWidth(line) > 40 {
			t.Fatalf("breadcrumb overflow: %q", line)
		}
	}
	if !strings.HasSuffix(lines[0], " ") {
		t.Fatalf("comment word split unnecessarily: %q", lines)
	}
}

func TestBrowserCommentKeepsBothEnds(t *testing.T) {
	ref := Reference{Session: strings.Repeat("a", 64) + "S", Directory: "/demo", Comment: "Updated notes and added a checklist"}
	u := browserUI{source: ref.Directory, refs: []Reference{ref}}
	view := u.view(100, 24)
	if !strings.Contains(view, "Updated") || !strings.Contains(view, "checklist") || !strings.Contains(view, "…") {
		t.Fatal(view)
	}
	if !strings.Contains(view, "│>") || strings.HasPrefix(view, " ") {
		t.Fatal("incorrect selection or outer margin", view)
	}
}

func TestBrowserCommentPreservesComparisonMarker(t *testing.T) {
	for _, width := range []int{14, 20, 32} {
		text := browserComment("[compare base] A long comment with an important ending", width)
		if !strings.HasPrefix(text, "[compare base]") || runewidth.StringWidth(text) > width {
			t.Fatalf("width %d: %q", width, text)
		}
	}
}
