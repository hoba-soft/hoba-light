package hobalight

import (
	"fmt"
)

// At the top level refs contains all sessions. Inside a source it contains
// only that source's sessions; allRefs retains the complete navigation list.
func (u *browserUI) sourceNames() []string {
	return browserSourceNames(u.refs)
}
func browserSourceNames(refs []Reference) []string {
	var names []string
	seen := map[string]bool{}
	for _, ref := range refs {
		if !seen[ref.Directory] {
			names = append(names, ref.Directory)
			seen[ref.Directory] = true
		}
	}
	var ordered []string
	for _, row := range browserSourceTree(names) {
		if row.source >= 0 {
			ordered = append(ordered, names[row.source])
		}
	}
	return ordered
}
func (u *browserUI) selectSource(source string) {
	if u.allRefs == nil && u.source == "" {
		u.allRefs = u.refs
	}
	for index, name := range browserSourceNames(u.allRefs) {
		if name == source {
			u.sourceCursor = index
			break
		}
	}
	u.source = source
	u.sources = false
	u.refs = nil
	for _, ref := range u.allRefs {
		if ref.Directory == source {
			u.refs = append(u.refs, ref)
		}
	}
	u.cursor = 0
	u.sessionTable = nil
}
func (u *browserUI) replaceReferences(refs []Reference) {
	filtered, err := filterSessionDates(refs, u.store.sessionDate)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	refs = filtered
	refs = groupedReferences(refs)
	u.stats = u.store.referenceStats(refs)
	if u.source != "" {
		u.allRefs = refs
		u.selectSource(u.source)
	} else {
		u.refs = refs
	}
	u.sessionTable = nil
	u.cursor = min(u.cursor, max(0, u.count()-1))
}
func (u *browserUI) sourceList(width, visible int) []string {
	rows, selected := u.sourceTreeTable(width)
	visible = max(1, visible-1)
	start := max(0, selected-visible+1)
	end := min(len(rows)-1, start+visible)
	return append(rows[:1:1], rows[1+start:1+end]...)
}

// Both the browser and calendar preview use the same expanded tree and counts.
// A negative cursor renders a read-only tree with no selection marker.
func (u *browserUI) sourceTreeTable(width int) ([]string, int) {
	names := u.sourceNames()
	heading := " " + browserTextColumns("Root", "Sessions", width, 8)
	if len(names) == 0 {
		return []string{heading, "No backup sessions."}, 0
	}
	rows := browserSourceTree(names)
	counts := map[string]int{}
	for _, ref := range u.refs {
		counts[ref.Directory]++
	}
	lines := []string{heading}
	selected := 0
	for i, row := range rows {
		count := ""
		if row.source >= 0 {
			count = fmt.Sprint(counts[names[row.source]])
		}
		active := row.source >= 0 && row.source == u.cursor
		if active {
			selected = i
		}
		lines = append(lines, selectionMarker(active)+browserTextColumns(row.label, count, width, 8))
	}
	return lines, selected
}
