package hobalight

import (
	"fmt"
	"time"
)

type browserHistory struct {
	path          string
	versions      []fileVersion
	cursor        int
	sessions      bool
	sessionCursor int
}

func (h *browserHistory) selected() (searchMatch, bool) {
	if len(h.versions) == 0 {
		return searchMatch{}, false
	}
	version := h.versions[h.cursor]
	cursor := 0
	if h.sessions {
		cursor = h.sessionCursor
	}
	return version.sessions[cursor], true
}

func (u *browserUI) showHistory() {
	id, selected, file, err := u.selectedRestore()
	if err != nil || !file {
		u.message = "Select a file to view its history"
		return
	}
	target, err := u.store.historyTarget([]string{id, selected})
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	versions, err := u.store.historyVersions(target)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	u.history = &browserHistory{path: target, versions: versions}
	u.message = ""
}

func (u *browserUI) historyKey(key string) {
	h := u.history
	cursor, count := &h.cursor, len(h.versions)
	if h.sessions {
		cursor, count = &h.sessionCursor, len(h.versions[h.cursor].sessions)
	}
	switch key {
	case "down", "j":
		*cursor = min(max(0, count-1), *cursor+1)
	case "up", "k":
		*cursor = max(0, *cursor-1)
	case "home":
		*cursor = 0
	case "end":
		*cursor = max(0, count-1)
	case "pagedown":
		*cursor = min(max(0, count-1), *cursor+10)
	case "pageup":
		*cursor = max(0, *cursor-10)
	case "left", "h", "b":
		if h.sessions {
			h.sessions = false
		} else {
			u.history = nil
		}
	case "right", "l":
		if count == 0 {
			return
		}
		if !h.sessions {
			h.sessions = true
			h.sessionCursor = 0
		} else {
			match, _ := h.selected()
			u.history = nil
			u.locateMatch(match)
		}
	case "c":
		u.showContent()
	case "d":
		u.compare()
	case "r", "o":
		u.restoreSelection(key == "o")
	case "t":
		u.beginRestoreTo()
	}
}

func (u *browserUI) historyList(width, visible int) []string {
	h := u.history
	if len(h.versions) == 0 {
		return []string{"No file versions found."}
	}
	cursor, count := h.cursor, len(h.versions)
	if h.sessions {
		cursor, count = h.sessionCursor, len(h.versions[h.cursor].sessions)
	}
	visible = max(1, visible)
	start := max(0, cursor-visible+1)
	var lines []string
	if !h.sessions {
		lines = append(lines, "  First backed up")
		visible = max(1, visible-1)
		start = max(0, cursor-visible+1)
	}
	for i := start; i < min(count, start+visible); i++ {
		var row string
		if h.sessions {
			m := h.versions[h.cursor].sessions[i]
			row = browserClip(sessionLabel(m.ref)+"  "+m.ref.Directory, max(8, width-5))
		} else {
			v := h.versions[i]
			label := fmt.Sprintf("%s  %s", v.earliest.UTC().Format(time.RFC3339Nano), historySessionCount(len(v.sessions)))
			if u.isComparisonBase(v.id, "") {
				label = "[compare base] " + label
			}
			row = hashSizeColumn(markedFileName(label, v.file), v.id[:8], displaySize(v.file.Size), max(8, width-5), 9)
		}
		lines = append(lines, selectionMarker(i == cursor)+row)
	}
	label := "versions"
	if h.sessions {
		label = "sessions"
	}
	return append(lines, fmt.Sprintf("%d/%d %s", cursor+1, count, label))
}
