package hobalight

import (
	"fmt"
	"github.com/mattn/go-runewidth"
	"path"
	"strings"
)

type browserSearch struct {
	query         string
	matches       []searchMatch
	cursor        int
	parentHistory *browserHistory
	parentSearch  *browserSearch
}

func (u *browserUI) searchScope(width int) string {
	return "All sessions"
}

func (u *browserUI) searchInputKey(key string) {
	switch key {
	case "cancel":
		u.searchInput = nil
		if u.search == nil {
			u.history = u.searchHistory
			u.search = u.searchParent
			u.searchParent = nil
			u.searchHistory = nil
		}
	case "enter":
		query := *u.searchInput
		matches, err := u.store.findBrowserNames(query)
		if err != nil {
			u.message = "Error: " + err.Error()
			return
		}
		parentHistory, parentSearch := u.searchHistory, u.searchParent
		if u.search != nil {
			parentHistory, parentSearch = u.search.parentHistory, u.search.parentSearch
		}
		u.search = &browserSearch{query: query, matches: matches, parentHistory: parentHistory, parentSearch: parentSearch}
		u.searchHistory, u.searchParent = nil, nil
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
	if len(search.matches) > 0 && search.matches[search.cursor].file.id == "" {
		switch key {
		case "o", "c", "d", "X", "v":
			u.message = "Select a file for this action"
			return
		}
	}
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
		u.search = search.parentSearch
		u.history = search.parentHistory
		u.message = ""
	case "n":
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
	u.history = nil
	u.searchHistory = nil
	u.searchParent = nil
	if u.merged != nil {
		if match.file.id != "" && !match.directory {
			full := strings.TrimRight(match.ref.Directory, "/") + "/" + match.file.relative
			node, err := u.merged.locateFile(full)
			if err != nil {
				u.message = "Error: " + err.Error()
				return
			}
			u.search = nil
			u.openMergedFile(node)
			return
		}
		u.merged = nil
		u.sources = true
	}
	_, files, err := u.store.listingFiles(match.ref.Session)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	if u.sources || u.source != "" {
		u.selectSource(match.ref.Directory)
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
	if match.directory {
		u.current = match.file.relative
		u.entries = u.tree[u.current]
		u.rebuildSearchParents()
		u.message = "Located directory " + match.file.relative
		return
	}
	if match.file.id == "" {
		u.entries = u.tree[""]
		u.message = "Located session " + match.ref.Session[:8]
		return
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
		return []string{" " + browserSearchRow("Path", "Session", "Hash", "Size", "Comment", width), "No matching files or directories."}
	}
	visible = max(1, visible-1)
	start := max(0, search.cursor-visible+1)
	end := min(len(search.matches), start+visible)
	rowWidth := width
	lines := []string{" " + browserSearchRow("Path", "Session", "Hash", "Size", "Comment", rowWidth)}
	for i := start; i < end; i++ {
		m := search.matches[i]
		name := strings.TrimRight(m.ref.Directory, "/") + "/" + m.file.relative
		id, size := "—", "—"
		if m.directory {
			name += "/"
		} else if m.file.id != "" {
			id = m.file.id[:8]
			size = displaySize(m.file.metadata.Size)
			name = markedFileName(name, m.file.metadata)
		}
		if u.isComparisonBase(m.file.id, "") {
			name = "[compare base] " + name
		}
		row := browserSearchRow(name, m.ref.Session[:8], id, size, browserText(m.ref.Comment), rowWidth)
		lines = append(lines, selectionMarker(i == search.cursor)+row)
	}
	return lines
}

func browserSearchRow(name, session, id, size, comment string, width int) string {
	suffix := fmt.Sprintf("%8s  %8s  %8s", session, id, size)
	commentWidth := min(24, max(7, (width-50)/2))
	if commentWidth > 0 {
		suffix += "  " + browserRight(browserComment(comment, commentWidth), commentWidth)
	}
	nameWidth := max(1, width-runewidth.StringWidth(suffix)-2)
	prefix := ""
	for _, mark := range []string{"[compare base] ", "[error] "} {
		if strings.HasPrefix(name, mark) {
			prefix += mark
			name = strings.TrimPrefix(name, mark)
		}
	}
	name = browserClip(prefix+browserPath(name, max(1, nameWidth-runewidth.StringWidth(prefix))), nameWidth)
	return name + strings.Repeat(" ", max(0, nameWidth-runewidth.StringWidth(name))) + "  " + suffix
}
