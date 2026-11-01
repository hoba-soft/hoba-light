package hobalight

import (
	"fmt"
	"path"
	"strings"
)

type browserSearch struct {
	query   string
	matches []searchMatch
	cursor  int
}

func (u *browserUI) searchScope(width int) string {
	if u.ref == nil {
		return "All sessions"
	}
	directory := u.current
	return u.ref.Session[:8] + " · " + browserPath(strings.TrimRight(u.ref.Directory, "/")+"/"+directory, max(1, width-11))
}

func (u *browserUI) searchInputKey(key string) {
	switch key {
	case "cancel":
		u.searchInput = nil
	case "enter":
		query := *u.searchInput
		session := ""
		if u.ref != nil {
			session = u.ref.Session
		}
		matches, err := u.store.findFiles(query, session, u.current)
		if err != nil {
			u.message = "Error: " + err.Error()
			return
		}
		u.search = &browserSearch{query: query, matches: matches}
		u.searchInput = nil
		u.message = ""
	case "backspace":
		runes := []rune(*u.searchInput)
		if len(runes) > 0 {
			*u.searchInput = string(runes[:len(runes)-1])
		}
	case "clear":
		*u.searchInput = ""
	default:
		*u.searchInput += key
	}
}

func (u *browserUI) searchKey(key string) {
	search := u.search
	switch key {
	case "up", "k":
		search.cursor = max(0, search.cursor-1)
	case "down", "j":
		search.cursor = min(max(0, len(search.matches)-1), search.cursor+1)
	case "home":
		search.cursor = 0
	case "end":
		search.cursor = max(0, len(search.matches)-1)
	case "pageup":
		search.cursor = max(0, search.cursor-10)
	case "pagedown":
		search.cursor = min(max(0, len(search.matches)-1), search.cursor+10)
	case "left", "b", "h":
		u.search = nil
		u.message = ""
	case "/":
		text := search.query
		u.searchInput = &text
	case "right", "l":
		u.locateSearchResult()
	case "c":
		u.showContent()
	case "d":
		u.compare()
	case "r", "o":
		u.restoreSelection(key == "o")
	case "t":
		u.beginRestoreTo()
	case "x", "delete":
		u.requestDelete()
	case "X":
		u.requestFileDelete()
	case "v":
		u.showHistory()
	}
}

func (u *browserUI) locateSearchResult() {
	if len(u.search.matches) == 0 {
		return
	}
	u.locateMatch(u.search.matches[u.search.cursor])
}

func (u *browserUI) locateMatch(match searchMatch) {
	_, files, err := u.store.listingFiles(match.ref.Session)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	// Rebuild the ancestor stack so Back behaves exactly like ordinary navigation.
	u.search = nil
	u.detail = nil
	u.ref = &match.ref
	u.files = files
	u.tree = nil
	u.prepareTree()
	u.parents = nil
	u.current = ""
	u.cursor = 0
	for i, ref := range u.refs {
		if ref.Session == match.ref.Session {
			u.sessionCursor = i
			break
		}
	}
	parts := strings.Split(match.file.relative, "/")
	for _, component := range parts[:len(parts)-1] {
		cursor := 0
		for i, entry := range u.tree[u.current] {
			if entry.name == component {
				cursor = i
				break
			}
		}
		u.parents = append(u.parents, browserPosition{u.current, cursor})
		u.current = path.Join(u.current, component)
	}
	u.entries = u.tree[u.current]
	for i, entry := range u.entries {
		if entry.name == parts[len(parts)-1] {
			u.cursor = i
			break
		}
	}
	u.message = "Located " + match.file.relative
}

func (u *browserUI) searchList(width, visible int) []string {
	search := u.search
	if len(search.matches) == 0 {
		return []string{"No matching files."}
	}
	// Each match includes its source/session context, even when scrolled.
	if u.ref == nil {
		visible = max(1, visible/3)
	} else {
		visible = max(1, visible)
	}
	start := max(0, search.cursor-visible+1)
	end := min(len(search.matches), start+visible)
	var lines []string
	for i := start; i < end; i++ {
		m := search.matches[i]
		if u.ref == nil {
			lines = append(lines, browserPath(m.ref.Directory, max(8, width-5)), "  "+sessionLabel(m.ref))
		}
		name := markedFileName(strings.TrimPrefix(m.file.relative, strings.TrimRight(u.current, "/")+"/"), m.file.metadata)
		if u.isComparisonBase(m.file.id, "") {
			name = "[compare base] " + name
		}
		row := hashSizeColumn(name, m.file.id[:8], displaySize(m.file.metadata.Size), max(8, width-7), len(displaySize(m.file.metadata.Size)))
		lines = append(lines, selectionMarker(i == search.cursor)+row)
	}
	return append(lines, fmt.Sprintf("%d/%d matches", search.cursor+1, len(search.matches)))
}
