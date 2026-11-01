package hobalight

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

func (u *browserUI) view(width, height int) string {
	header := []string{"Hoba Light Browser"}
	if u.compareBase != nil {
		height--
	}
	var content, footer []string
	if u.showHelp {
		header = append(header, "Keyboard shortcuts", "")
		help := u.help()
		if u.streamDiff != nil {
			help = "j/k ↑/↓ scroll · Ctrl+F/B page · Ctrl+U/D half page · gg/G start/end · / search · n/N next/previous · h/l horizontal scroll · Enter file · a auto · t text · x hex · ← back · q quit"
		}
		if u.streamDiff == nil {
			help += " · Home/End first/last · PgUp/PgDn page"
		}
		var rows []string
		for _, action := range strings.Split(help, " · ") {
			rows = append(rows, wrapBrowserText(action, max(8, width-5))...)
		}
		visible := max(1, height-8)
		u.helpOffset = min(u.helpOffset, max(0, len(rows)-visible))
		content = rows[u.helpOffset:min(len(rows), u.helpOffset+visible)]
		footer = []string{"↑/↓ scroll · ?/Esc/← close · q quit"}
	} else if u.streamDiff != nil {
		h, c, f := u.streamDiffView(width, height)
		header = append(header, h...)
		content = c
		footer = f
	} else if u.pendingFileDelete != nil {
		header = append(header, "Delete file from all sessions?", "")
		var lines []string
		for _, line := range fileDeletionDescription(*u.pendingFileDelete) {
			lines = append(lines, wrapBrowserText(line, max(8, width-5))...)
		}
		visible := max(1, height-9)
		u.deleteOffset = min(u.deleteOffset, max(0, len(lines)-visible))
		content = lines[u.deleteOffset:min(len(lines), u.deleteOffset+visible)]
		footer = []string{u.briefHelp()}
	} else if u.restoreToInput != nil {
		header = append(header, "Restore to directory", "")
		id, selected, _, _ := u.selectedRestore()
		content = []string{"Session: " + id[:8]}
		if selected == "" {
			selected = "."
		}
		content = append(content, wrapBrowserText("Path: "+selected, max(8, width-5))...)
		content = append(content, "Destination (relative to working directory):")
		content = append(content, wrapBrowserText("> "+*u.restoreToInput+"█", max(8, width-5))...)
		footer = wrapBrowserText(u.briefHelp(), max(8, width-1))
	} else if u.history != nil {
		header = append(header, "File history: "+browserPath(u.history.path, max(8, width-15)), "")
		if u.history.sessions {
			header = append(header, "Version: "+u.history.versions[u.history.cursor].id[:8])
		}
		content = u.historyList(width, max(1, height-11))
		footer = wrapBrowserText(u.briefHelp(), max(8, width-1))
	} else if u.searchInput != nil {
		header = append(header, "Find files · "+u.searchScope(max(8, width-14)), "")
		content = wrapBrowserText("> "+*u.searchInput+"█", max(8, width-5))
		footer = wrapBrowserText(u.briefHelp(), max(8, width-1))
	} else if u.search != nil && u.pendingDelete == nil {
		header = append(header, "Find: "+u.search.query, "Scope: "+u.searchScope(max(8, width-8)), "")
		content = u.searchList(width, max(1, height-11))
		footer = wrapBrowserText(u.briefHelp(), max(8, width-1))
	} else if u.pendingDelete != nil {
		ref := *u.pendingDelete
		header = append(header, "Delete session?", "")
		content = []string{"Session: " + ref.Session[:8], "Started: " + sessionTime(ref)}
		content = append(content, wrapBrowserText("Directory: "+ref.Directory, max(8, width-5))...)
		if u.pendingDeletePath != "" {
			header[1] = "Delete from session?"
			content = append(content, wrapBrowserText("Path: "+u.pendingDeletePath, max(8, width-5))...)
			content = append(content, "", "Create a replacement session without this path and its descendants?", "The old session moves to trash. Run gc later to collect unused data.")
		} else {
			content = append(content, "", "Move the session to trash and remove its references?", "Run gc later to collect unreferenced file data.")
		}
		footer = wrapBrowserText(u.briefHelp(), max(8, width-1))

	} else {
		visible := max(1, height-11)
		if u.ref == nil {
			header = append(header, "Sessions", "")
			content = u.sessionList(width, visible)
		} else {
			u.prepareTree()
			directory := strings.TrimRight(u.ref.Directory, "/") + "/" + u.current
			header = append(header, "Session "+u.ref.Session[:8])
			if u.detail == nil {
				suffix := displayFileCounts(u.directoryCounts[u.current], u.directoryErrors[u.current]) + "  " + displaySize(u.directorySizes[u.current])
				header = append(header, sizeColumn(browserPath(directory, max(1, width-3-runewidth.StringWidth(suffix))), suffix, max(8, width-1)))
			}
			header = append(header, "")
			if u.detail != nil {
				content = fileDetails(*u.detail, width)
			} else {
				content = u.directoryList(width, visible)
			}
		}
		footer = wrapBrowserText(u.briefHelp(), max(8, width-1))
	}
	// Separate the application title from the current view, keeping its context
	// immediately above the panel.
	for len(header) > 1 && header[len(header)-1] == "" {
		header = header[:len(header)-1]
	}
	header = append([]string{header[0], ""}, header[1:]...)
	if u.compareBase != nil {
		prefix := fmt.Sprintf("Compare base: %.8s  ", u.compareBase.id)
		header = append(header, prefix+browserPath(u.compareBaseLabel, max(1, width-1-len(prefix))))
	}
	lines := append(header, browserPanel(content, width)...)
	lines = append(lines, "")
	lines = append(lines, footer...)
	if u.message != "" {
		lines = append(lines, "", u.message)
	}
	for i, line := range lines {
		lines[i] = browserClip(line, max(8, width-1))
	}
	return strings.Join(lines, "\n") + "\n"
}

func fileDetails(f sessionFile, width int) []string {
	created := "unavailable"
	if f.metadata.Created != nil {
		created = *f.metadata.Created
	}
	lines := wrapBrowserText("Path: "+f.metadata.Path, max(8, width-5))
	lines = append(lines, fmt.Sprintf("Size: %d bytes", f.metadata.Size), "Created: "+created, "Modified: "+f.metadata.Modified, "File object: "+f.id[:8])
	if f.metadata.Error != "" {
		return append(lines, wrapBrowserText("Error: "+f.metadata.Error, max(8, width-5))...)
	}
	if f.metadata.Blob != "" {
		lines = append(lines, fmt.Sprintf("Blob object: %.8s", f.metadata.Blob))
	}
	return lines
}

func (u *browserUI) directoryList(width, visible int) []string {
	if len(u.entries) == 0 {
		return []string{"No files."}
	}
	visible = max(1, visible-1)
	table, ok := u.directoryTables[u.current]
	if !ok { // Standalone views may provide entries without a loaded tree.
		var stats []tableStats
		for _, entry := range u.entries {
			stats = append(stats, entryStats(entry))
		}
		table = newListingTable(nil, stats)
	}
	start := max(0, u.cursor-visible+1)
	end := min(len(u.entries), start+visible)
	lines := []string{"  " + table.header(false, max(8, width-7))}
	for i := start; i < end; i++ {
		entry := u.entries[i]
		name, hash := entryColumns(entry)
		if !entry.directory && entry.file != nil && u.isComparisonBase(entry.file.id, "") || entry.directory && u.ref != nil && u.isComparisonBase(u.ref.Session, strings.Trim(u.current+"/"+entry.name, "/")) {
			name = "[compare base] " + name
		}
		row := table.row(name, hash, entryStats(entry), max(8, width-7))
		lines = append(lines, selectionMarker(i == u.cursor)+row)
	}
	if len(u.entries) > visible {
		lines = append(lines, fmt.Sprintf("%d/%d", u.cursor+1, len(u.entries)))
	}
	return lines
}

func selectionMarker(selected bool) string {
	if selected {
		return "> "
	}
	return "  "
}

// Group headers have no selection index. Only visible sessions are formatted.
func (u *browserUI) sessionList(width, visible int) []string {
	if len(u.refs) == 0 {
		return []string{"No backup sessions."}
	}
	type row struct {
		text    string
		session int
	}
	var rows []row
	positions := make([]int, len(u.refs))
	headers := make(map[int]bool)
	if u.stats == nil {
		if u.store != nil {
			u.stats = u.store.referenceStats(u.refs)
		} else {
			u.stats = map[string]tableStats{}
			for _, ref := range u.refs {
				u.stats[ref.Session] = tableStats{"?", "?", "?"}
			}
		}
	}
	if u.sessionTable == nil {
		var totals []tableStats
		for _, ref := range u.refs {
			totals = append(totals, u.stats[ref.Session])
		}
		table := newListingTable(nil, totals)
		u.sessionTable = &table
	}
	table := *u.sessionTable
	visible = max(1, visible-1)
	for i, ref := range u.refs {
		if i == 0 || ref.Directory != u.refs[i-1].Directory {
			if i > 0 {
				rows = append(rows, row{session: -1})
			}
			headers[len(rows)] = true
			rows = append(rows, row{text: ref.Directory, session: -1})
		}
		positions[i] = len(rows)
		rows = append(rows, row{session: i})
	}
	selected := positions[u.cursor]
	start := 0
	var lines []string
	if visible == 1 {
		start = selected
	} else {
		if selected >= visible {
			start = max(0, selected-visible+2)
		}
		if start > 0 && !headers[start] {
			if rows[start].text == "" && rows[start].session < 0 {
				start++
			}
			if !headers[start] {
				context := 0
				for context < len(positions)-1 && positions[context] < start {
					context++
				}
				lines = append(lines, browserPath(u.refs[context].Directory, max(8, width-5)))
			}
		}
	}
	end := min(len(rows), start+visible-len(lines))
	if end <= selected {
		end = selected + 1
	}
	for _, row := range rows[start:end] {
		if row.session < 0 {
			lines = append(lines, browserPath(row.text, max(8, width-5)))
			continue
		}
		ref := u.refs[row.session]
		mark := ""
		if u.isComparisonBase(ref.Session, "") {
			mark = "[compare base] "
		}
		label := table.row(mark+sessionTime(ref), ref.Session[:8], u.stats[ref.Session], max(8, width-7))
		lines = append(lines, selectionMarker(row.session == u.cursor)+label)
	}
	if len(rows) > visible {
		lines = append(lines, fmt.Sprintf("%d/%d", u.cursor+1, len(u.refs)))
	}
	lines = append([]string{"  " + table.header(true, max(8, width-7))}, lines...)
	return lines
}

func browserPanel(lines []string, width int) []string {
	inner := max(8, width-5)
	result := []string{"┌" + strings.Repeat("─", inner+2) + "┐"}
	for _, line := range lines {
		line = browserClip(line, inner)
		padding := max(0, inner-runewidth.StringWidth(line))
		result = append(result, "│ "+line+strings.Repeat(" ", padding)+" │")
	}
	return append(result, "└"+strings.Repeat("─", inner+2)+"┘")
}
