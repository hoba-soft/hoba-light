package hobalight

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
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
	if len(version.sessions) == 0 {
		return searchMatch{}, false
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
		if !h.sessions && u.merged != nil {
			version := h.versions[h.cursor]
			full := sessionFile{id: version.id, metadata: version.file, relative: version.file.Path}
			u.detail = &full
			return
		}
		if !h.sessions {
			h.sessions = true
			h.sessionCursor = 0
		} else {
			match, _ := h.selected()
			u.history = nil
			if u.merged != nil {
				u.merged = nil
				u.sources = true
			}
			u.locateMatch(match)
		}
	case "s":
		if len(h.versions) > 0 {
			h.sessions = !h.sessions
			h.sessionCursor = 0
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
	heading := " " + hashSizeColumn("First backed up / Sessions", "Hash", "Size", width, 9)
	if h.sessions {
		heading = " " + historySessionRow("Started", "Session", "Comment / Source", "", width)
	}
	if len(h.versions) == 0 {
		return []string{heading, "No file versions found."}
	}
	cursor, count := h.cursor, len(h.versions)
	if h.sessions {
		cursor, count = h.sessionCursor, len(h.versions[h.cursor].sessions)
	}
	visible = max(1, visible-1)
	start := max(0, cursor-visible+1)
	lines := []string{heading}
	for i := start; i < min(count, start+visible); i++ {
		var row string
		if h.sessions {
			m := h.versions[h.cursor].sessions[i]
			row = historySessionRow(sessionTime(m.ref), m.ref.Session[:8], m.ref.Comment, m.ref.Directory, width)
		} else {
			v := h.versions[i]
			label := fmt.Sprintf("%s  %s", v.earliest.In(time.Local).Format(time.RFC3339Nano), historySessionCount(len(v.sessions)))
			if u.isComparisonBase(v.id, "") {
				label = "[compare base] " + label
			}
			row = hashSizeColumn(markedFileName(label, v.file), v.id[:8], displaySize(v.file.Size), width, 9)
		}
		lines = append(lines, selectionMarker(i == cursor)+row)
	}
	return lines
}

func historySessionRow(started, session, comment, source string, width int) string {
	commentWidth := min(40, max(16, (width-38)/2))
	return browserTextColumns(started, browserRight(session, 8)+"  "+browserRight(historyCommentSource(comment, source, commentWidth), commentWidth), width, 8+2+commentWidth)
}

func historyCommentSource(comment, source string, width int) string {
	comment, source = browserText(comment), browserText(source)
	if comment == "" {
		return browserPath(source, width)
	}
	if source == "" {
		return browserPath(comment, width)
	}
	// Preserve short comments; shorten the source independently so it cannot
	// swallow the comment when the two share a column.
	commentWidth := min(runewidth.StringWidth(comment), max(1, width-1-min(10, width/3)))
	comment = browserPath(comment, commentWidth)
	source = browserPath(source, max(0, width-runewidth.StringWidth(comment)-1))
	return strings.TrimSpace(comment + " " + source)
}
