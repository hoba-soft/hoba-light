package hobalight

import (
	"fmt"
	"path"
	"strings"

	"github.com/mattn/go-runewidth"
)

type browserPanelLine struct {
	text      string
	separator bool
}

func browserBox(lines []browserPanelLine, width int) []string {
	result := []string{"┌" + strings.Repeat("─", width+1) + "┐"}
	for _, row := range lines {
		if row.separator {
			result = append(result, "├"+strings.Repeat("─", width+1)+"┤")
			continue
		}
		line := row.text
		marker := " "
		if strings.HasPrefix(line, ">") {
			marker, line = ">", strings.TrimPrefix(line, ">")
		} else {
			line = strings.TrimPrefix(line, " ")
		}
		line = browserClip(line, width)
		result = append(result, "│"+marker+line+strings.Repeat(" ", max(0, width-runewidth.StringWidth(line)))+"│")
	}
	return append(result, "└"+strings.Repeat("─", width+1)+"┘")
}

func (u *browserUI) view(width, height int) string {
	contentWidth := max(8, width-3)
	header := []string{"Hoba Light Browser"}
	var content, footer []string
	var contentRules []int
	if u.filterInput != nil {
		header = append(header, "Filter current list", "")
		content = wrapBrowserText("> "+*u.filterInput+"█", max(8, width-5))
		footer = []string{"Enter apply · Esc cancel · empty text clears filter"}
	} else if u.datePicker != nil {
		header = append(header, "Select session date (local time) · "+u.datePicker.Format("2006-01-02"))
		content, contentRules = u.datePickerView(width, height)
		footer = u.datePickerHelp()
	} else if u.showHelp {
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
		viewerWidth, viewerHeight := max(8, width-4), max(1, height-2)
		if u.streamDiff.contentID != "" {
			viewerWidth, viewerHeight = contentWidth, height
		}
		h, c, f := u.streamDiffView(viewerWidth, viewerHeight)
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
		footer = wrapBrowserHelp(u.briefHelp(), max(8, width-3))
	} else if u.merged != nil && u.detail != nil && u.history != nil {
		header = append(header, "File version metadata", browserPath(u.history.path, max(8, width-3)))
		content = fileDetails(*u.detail, contentWidth)
		footer = []string{"← back · c content · r restore · t restore-to · o open · D date · m mode · q quit"}
	} else if u.history != nil {
		suffix := ""
		if u.history.sessions {
			suffix = " · Version: " + u.history.versions[u.history.cursor].id[:8]
		}
		header = append(header, "File history: "+browserPath(u.history.path, max(8, width-19-runewidth.StringWidth(suffix)))+suffix)
		content = u.historyList(contentWidth, u.listHeight(width, height))
		footer = wrapBrowserHelp(u.briefHelp(), max(8, width-3))
	} else if u.searchInput != nil {
		header = append(header, "Search names · All sessions", "")
		content = wrapBrowserText("> "+*u.searchInput+"█", max(8, width-5))
		footer = wrapBrowserHelp(u.briefHelp(), max(8, width-3))
	} else if u.search != nil && u.pendingDelete == nil {
		header = append(header, "Find: "+u.search.query, "Scope: "+u.searchScope(max(8, width-8)), "")
		content = u.searchList(contentWidth, u.listHeight(width, height))
		footer = wrapBrowserHelp(u.briefHelp(), max(8, width-3))
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
		footer = wrapBrowserHelp(u.briefHelp(), max(8, width-3))

	} else {
		visible := u.listHeight(width, height)
		if u.merged != nil {
			header = append(header, "Merged files", browserPath(u.mergedLocation(), max(8, width-3)))

			content = u.mergedList(contentWidth, visible)
		} else if u.ref == nil {
			if u.sources {
				header = append(header, "Sources", "")
				content = u.sourceList(contentWidth, visible)
			} else {
				header = append(header, "Sessions · "+browserPath(u.source, max(8, width-16)))
				content = u.sessionList(contentWidth, visible)
			}
		} else {
			u.prepareTree()
			label := "Sources › " + browserPath(u.ref.Directory, max(8, width/2)) + " › Session " + u.ref.Session[:8]
			if u.current != "" {
				label += " › " + u.current
			}
			if u.detail != nil {
				label += " › " + path.Base(u.detail.relative)
			}
			header = append(header, label)
			if u.detail != nil {
				content = fileDetails(*u.detail, contentWidth)
			} else {
				content = u.directoryList(contentWidth, visible)

			}
		}
		footer = wrapBrowserHelp(u.briefHelp(), max(8, width-3))
	}
	// Wrap full navigation context immediately above the panel.
	var context []string
	for _, line := range header[1:] {
		if line != "" {
			context = append(context, line)
		}
	}
	if len(context) == 0 {
		context = append(context, header[0])
	}
	breadcrumb := strings.Join(context, " › ")
	if u.filterInput == nil && u.searchInput == nil && u.restoreToInput == nil && u.pendingDelete == nil && u.pendingFileDelete == nil && u.datePicker == nil && !u.showHelp && u.streamDiff == nil {
		breadcrumb = u.browserBreadcrumb()
	}
	header = []string{"hoba-light browser · " + Version(), ""}
	if height < 14 {
		header = header[:1]
	}
	var wrappedFooter []string
	for _, line := range footer {
		wrappedFooter = append(wrappedFooter, wrapBrowserHelp(line, max(8, width-3))...)
	}
	footer = wrappedFooter
	if height < 16 && u.canFilter() && u.filterInput == nil {
		footer = []string{browserClip("↑/↓ · Enter · ← · ? help · q quit", max(8, width-3))}
	}
	if u.filter != nil {
		if u.filterInput == nil && u.canFilter() && u.filterCount() == 0 {
			content = []string{content[0], "No matching rows."}
		}
	}
	if breadcrumb != "" {
		header = append(header, wrapBrowserBreadcrumb(breadcrumb, max(8, width-3))...)
	}
	lines := browserIndented(header, width)
	panel := browserFrame(content, contentWidth, u.canFilter() && u.filterInput == nil, u.streamDiff != nil)
	for i := len(contentRules) - 1; i >= 0; i-- {
		after := contentRules[i]
		panel = append(panel[:after+1], append([]browserPanelLine{{separator: true}}, panel[after+1:]...)...)
	}
	boxed := browserBox(panel, contentWidth)
	for _, line := range boxed {
		lines = append(lines, line)
	}
	lines = append(lines, browserIndented(footer, width)...)
	if u.message != "" && (u.streamDiff == nil || u.streamDiff.contentID == "" || strings.HasPrefix(u.message, "Error:")) {
		lines = append(lines, u.message)
	}
	for i, line := range lines {
		lines[i] = browserClip(line, max(8, width))
	}
	return strings.Join(lines, "\n") + "\n"
}

func fileDetails(f sessionFile, width int) []string {
	created := "unavailable"
	if f.metadata.Created != nil {
		created = displayTimestamp(*f.metadata.Created)
	}
	lines := wrapBrowserText("Path: "+f.metadata.Path, max(8, width-5))
	lines = append(lines, fmt.Sprintf("Size: %d bytes", f.metadata.Size), "Created: "+created, "Modified: "+displayTimestamp(f.metadata.Modified), "File object: "+f.id[:8])
	if f.metadata.Error != "" {
		return append(lines, wrapBrowserText("Error: "+f.metadata.Error, max(8, width-5))...)
	}
	if f.metadata.Blob != "" {
		lines = append(lines, fmt.Sprintf("Blob object: %.8s", f.metadata.Blob))
	}
	return lines
}

func (u *browserUI) directoryList(width, visible int) []string {
	visible = max(1, visible-1)
	table, ok := u.directoryTables[u.current]
	if !ok { // Standalone views may provide entries without a loaded tree.
		var stats []tableStats
		for _, entry := range u.entries {
			stats = append(stats, entryStats(entry))
		}
		table = newListingTable(browserEntryNames(u.entries), stats)
	}
	start := max(0, u.cursor-visible+1)
	end := min(len(u.entries), start+visible)
	if u.compareBase != nil {
		table.name += len("[compare base] ")
	}
	lines := []string{" " + table.header(false, width)}
	if len(u.entries) == 0 {
		return append(lines, "No files.")
	}
	for i := start; i < end; i++ {
		entry := u.entries[i]
		name, hash := entryColumns(entry)
		if !entry.directory && entry.file != nil && u.isComparisonBase(entry.file.id, "") || entry.directory && u.ref != nil && u.isComparisonBase(u.ref.Session, strings.Trim(u.current+"/"+entry.name, "/")) {
			name = "[compare base] " + name
		}
		row := table.row(name, hash, entryStats(entry), width)
		lines = append(lines, selectionMarker(i == u.cursor)+row)
	}

	return lines
}

func selectionMarker(selected bool) string {
	if selected {
		return ">"
	}
	return " "
}

// Every session is one selectable table row, including its comment.
func (u *browserUI) sessionList(width, visible int) []string {
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
	rowWidth := width
	// Trailing columns stay together; only Started expands with the terminal.
	commentWidth := len("Comment")
	commentMinimum := len("Comment")
	for _, ref := range u.refs {
		commentWidth = max(commentWidth, runewidth.StringWidth(u.sessionComment(ref)))
		if u.isComparisonBase(ref.Session, "") {
			commentMinimum = len("[compare base]")
		}
	}
	suffixWidth := 8 + 2 + table.files + 2 + table.errors + 2 + table.size
	commentWidth = min(commentWidth, 32, max(commentMinimum, rowWidth-26-2-suffixWidth-2))
	baseWidth := rowWidth - commentWidth - 2
	heading := table.header(true, baseWidth)
	if rowWidth > baseWidth+2 {
		heading += "  " + browserRight("Comment", rowWidth-baseWidth-2)
	}
	lines := []string{" " + heading}
	if len(u.refs) == 0 {
		return append(lines, "No backup sessions.")
	}
	visible = max(1, visible-1)
	start := max(0, u.cursor-visible+1)
	for i := start; i < min(len(u.refs), start+visible); i++ {
		ref := u.refs[i]
		comment := u.sessionComment(ref)
		row := table.row(sessionTime(ref), ref.Session[:8], u.stats[ref.Session], baseWidth)
		if rowWidth > baseWidth+2 {
			row += "  " + browserRight(browserComment(comment, rowWidth-baseWidth-2), rowWidth-baseWidth-2)
		}
		lines = append(lines, selectionMarker(i == u.cursor)+row)
	}

	return lines
}

func (u *browserUI) sessionComment(ref Reference) string {
	comment := browserText(ref.Comment)
	if u.source == "" {
		comment = ref.Directory + commentSuffix(ref.Comment)
	}
	if u.isComparisonBase(ref.Session, "") {
		comment = "[compare base] " + comment
	}
	return comment
}

func (u *browserUI) listHeight(width, height int) int {
	footer := len(wrapBrowserHelp(u.briefHelp(), max(8, width-3)))
	message := 0
	if u.message != "" {
		message = 1
	}
	crumbRows := 0
	if crumb := u.browserBreadcrumb(); crumb != "" {
		crumbRows = len(wrapBrowserBreadcrumb(crumb, max(8, width-3)))
	}
	return max(1, height-6-footer-message-crumbRows)
}

// Keep each shortcut together instead of breaking words across terminal rows.
func wrapBrowserHelp(help string, width int) []string {
	var lines []string
	for _, action := range strings.Split(help, " · ") {
		if len(lines) > 0 && runewidth.StringWidth(lines[len(lines)-1]+" · "+action) <= width {
			lines[len(lines)-1] += " · " + action
			continue
		}
		lines = append(lines, wrapBrowserText(action, width)...)
	}
	return lines
}

// Rows carry a selection marker displayed in the first column inside the frame.
func browserFrame(content []string, width int, table, viewer bool) []browserPanelLine {
	var lines []browserPanelLine
	for i, line := range content {
		if !(table || viewer) || (!strings.HasPrefix(line, " ") && !strings.HasPrefix(line, ">")) {
			line = " " + line
		}
		lines = append(lines, browserPanelLine{text: browserClip(line, max(8, width+1))})
		if table && i == 0 {
			lines = append(lines, browserPanelLine{separator: true})
		}
	}
	return lines
}

func browserIndented(lines []string, width int) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			line = browserClip(line, max(8, width))
		}
		result = append(result, line)
	}
	return result
}

func browserTextColumns(name, value string, width, valueWidth int) string {
	nameWidth := max(1, width-valueWidth-2)
	name = browserPath(name, nameWidth)
	return name + strings.Repeat(" ", max(0, nameWidth-runewidth.StringWidth(name))) + "  " + browserRight(value, valueWidth)
}

func browserRight(text string, width int) string {
	text = browserClip(text, width)
	return strings.Repeat(" ", max(0, width-runewidth.StringWidth(text))) + text
}

func browserComment(text string, width int) string {
	const marker = "[compare base]"
	if strings.HasPrefix(text, marker+" ") {
		if width <= len(marker) {
			return browserClip(marker, width)
		}
		return marker + " " + browserPath(strings.TrimPrefix(text, marker+" "), width-len(marker)-1)
	}
	return browserPath(text, width)
}
