package hobalight

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

type tableStats struct{ files, errors, size string }

func numericStats(files, errors int, size int64) tableStats {
	return tableStats{fmt.Sprint(files), fmt.Sprint(errors), displaySize(size)}
}
func entryStats(e browserEntry) tableStats {
	st := numericStats(e.count, e.errors, e.size)
	if !e.directory {
		st.files = "—"
	}
	return st
}

type listingTable struct{ name, files, errors, size int }

func newListingTable(names []string, stats []tableStats) listingTable {
	t := listingTable{name: 7, files: 5, errors: 6, size: 4}
	for _, n := range names {
		t.name = max(t.name, runewidth.StringWidth(browserText(n)))
	}
	for _, s := range stats {
		t.files = max(t.files, len(s.files))
		t.errors = max(t.errors, len(s.errors))
		t.size = max(t.size, len(s.size))
	}
	return t
}
func (t listingTable) row(name, hash string, s tableStats, width int) string {
	suffix := fmt.Sprintf("%8s  %*s  %*s  %*s", hash, t.files, s.files, t.errors, s.errors, t.size, s.size)
	nameWidth := t.name
	if width > 0 {
		nameWidth = max(1, width-runewidth.StringWidth(suffix)-2)
	}
	name = browserPath(name, nameWidth)
	return name + strings.Repeat(" ", max(0, nameWidth-runewidth.StringWidth(name))) + "  " + suffix
}
func (t listingTable) header(session bool, width int) string {
	name, hash := "Name", "Hash"
	if session {
		name, hash = "Started", "Session"
	}
	return t.row(name, hash, tableStats{"Files", "Errors", "Size"}, width)
}

func browserEntryNames(entries []browserEntry) []string {
	var names []string
	for _, entry := range entries {
		name := entry.name
		if entry.directory {
			name += "/"
		} else if entry.file != nil {
			name = markedFileName(name, entry.file.metadata)
		}
		names = append(names, name)
	}
	return names
}
