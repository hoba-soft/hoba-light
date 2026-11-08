package hobalight

import (
	"fmt"
	"strings"
	"time"
)

// A filter projects only the current list. Restore the original list and map
// the selected row back before any navigation or action uses that selection.
type browserFilter struct {
	text    string
	restore func()
}

func filterRows[T any](rows []T, text string, label func(T) string) ([]T, []int) {
	var filtered []T
	var indices []int
	for i, row := range rows {
		if strings.Contains(strings.ToLower(browserText(label(row))), strings.ToLower(text)) {
			filtered = append(filtered, row)
			indices = append(indices, i)
		}
	}
	return filtered, indices
}

func restoreFilterCursor(cursor *int, indices []int, previous int) {
	if len(indices) == 0 {
		*cursor = previous
		return
	}
	*cursor = indices[min(*cursor, len(indices)-1)]
}

func (u *browserUI) clearFilter() {
	if u.filter != nil {
		u.filter.restore()
		u.filter = nil
	}
}

func (u *browserUI) canFilter() bool {
	return (u.detail == nil || (u.merged == nil && (u.search != nil || u.history != nil))) && u.streamDiff == nil && u.datePicker == nil && !u.showHelp && u.pendingDelete == nil && u.pendingFileDelete == nil && u.restoreToInput == nil && u.searchInput == nil
}

func (u *browserUI) filterKey(key string) bool {
	if u.filterInput != nil {
		switch key {
		case "cancel":
			u.filterInput = nil
		case "enter":
			text := *u.filterInput
			u.filterInput = nil
			u.clearFilter()
			if text != "" {
				u.applyFilter(text)
			}
		case "backspace":
			r := []rune(*u.filterInput)
			if len(r) > 0 {
				*u.filterInput = string(r[:len(r)-1])
			}
		case "clear":
			*u.filterInput = ""
		default:
			*u.filterInput += key
		}
		return true
	}
	if !u.canFilter() {
		return false
	}
	if key == "/" {
		text := ""
		if u.filter != nil {
			text = u.filter.text
		}
		u.filterInput = &text
		u.message = ""
		return true
	}
	if key == "n" {
		u.clearFilter()
		if u.history != nil {
			u.searchHistory = u.history
			u.searchParent = u.search
			u.search = nil
			u.history = nil
		}
		text := ""
		u.searchInput = &text
		u.message = ""
		return true
	}
	if u.filter == nil {
		return false
	}
	switch key {
	case "left", "b", "h":
		u.clearFilter()
		return true
	case "right", "l", "r", "o", "t", "x", "delete", "X", "c", "d", "v", "s":
		if u.filterCount() == 0 {
			u.message = "No matching rows"
			return true
		}
		u.clearFilter()
	case "m", "D", "f":
		u.clearFilter()
	}
	return false
}

func (u *browserUI) filterCount() int {
	if u.history != nil {
		if u.history.sessions {
			return len(u.history.versions[u.history.cursor].sessions)
		}
		return len(u.history.versions)
	}
	if u.search != nil {
		return len(u.search.matches)
	}
	if u.merged != nil {
		return len(u.merged.entries())
	}
	return u.count()
}

func (u *browserUI) applyFilter(text string) {
	f := &browserFilter{text: text}
	switch {
	case u.history != nil:
		h := u.history
		if h.sessions {
			version := h.cursor
			original, previous := h.versions[version].sessions, h.sessionCursor
			filtered, indices := filterRows(original, text, func(m searchMatch) string { return sessionLabel(m.ref) + " " + m.ref.Directory })
			h.versions[version].sessions, h.sessionCursor = filtered, 0
			f.restore = func() {
				restoreFilterCursor(&h.sessionCursor, indices, previous)
				h.versions[version].sessions = original
			}
		} else {
			original, previous := h.versions, h.cursor
			filtered, indices := filterRows(original, text, func(v fileVersion) string {
				return fmt.Sprintf("%s %s %s %s", v.earliest.In(time.Local).Format(time.RFC3339Nano), historySessionCount(len(v.sessions)), v.id[:8], displaySize(v.file.Size))
			})
			h.versions, h.cursor = filtered, 0
			f.restore = func() { restoreFilterCursor(&h.cursor, indices, previous); h.versions = original }
		}
	case u.search != nil:
		h := u.search
		original, previous := h.matches, h.cursor
		filtered, indices := filterRows(original, text, browserSearchFilterText)
		h.matches, h.cursor = filtered, 0
		f.restore = func() { restoreFilterCursor(&h.cursor, indices, previous); h.matches = original }
	case u.merged != nil:
		m := u.merged
		original, previous := m.entries(), m.cursor
		_, indices := filterRows(original, text, mergedFilterText)
		m.filter, m.cursor = text, 0
		f.restore = func() { restoreFilterCursor(&m.cursor, indices, previous); m.filter = "" }
	case u.ref != nil:
		original, previous := u.entries, u.cursor
		filtered, indices := filterRows(original, text, func(e browserEntry) string {
			name, id := entryColumns(e)
			stats := entryStats(e)
			return name + " " + id + " " + stats.files + " " + stats.errors + " " + stats.size
		})
		u.entries, u.cursor = filtered, 0
		f.restore = func() { restoreFilterCursor(&u.cursor, indices, previous); u.entries = original }
	case u.sources:
		original, previous := u.refs, u.cursor
		names := u.sourceNames()
		counts := map[string]int{}
		for _, ref := range original {
			counts[ref.Directory]++
		}
		filteredNames, indices := filterRows(names, text, func(name string) string { return fmt.Sprintf("%s %d", name, counts[name]) })
		selected := map[string]bool{}
		for _, name := range filteredNames {
			selected[name] = true
		}
		u.refs = nil
		for _, ref := range original {
			if selected[ref.Directory] {
				u.refs = append(u.refs, ref)
			}
		}
		u.cursor = 0
		f.restore = func() { restoreFilterCursor(&u.cursor, indices, previous); u.refs = original; u.sessionTable = nil }
	default:
		original, previous := u.refs, u.cursor
		filtered, indices := filterRows(original, text, func(ref Reference) string {
			stats := u.stats[ref.Session]
			return sessionLabel(ref) + " " + stats.files + " " + stats.errors + " " + stats.size
		})
		u.refs, u.cursor, u.sessionTable = filtered, 0, nil
		f.restore = func() { restoreFilterCursor(&u.cursor, indices, previous); u.refs = original; u.sessionTable = nil }
	}
	u.filter = f
}

func browserSearchFilterText(m searchMatch) string {
	name := m.file.relative
	if m.directory {
		name += "/"
	} else if m.file.id != "" {
		name = markedFileName(name, m.file.metadata) + " " + m.file.id[:8] + " " + displaySize(m.file.metadata.Size)
	}
	return m.ref.Directory + " " + sessionLabel(m.ref) + " " + name
}
