package hobalight

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
	"golang.org/x/term"
)

type browserPosition struct {
	directory string
	cursor    int
}
type browserUI struct {
	filterInput                  *string
	filter                       *browserFilter
	merged                       *browserMerged
	datePicker                   *time.Time
	dateReferences               map[string][]Reference
	dateRootOffset, dateRootPage int
	dateRootsFocus               bool
	sources                      bool
	source                       string
	sourceCursor                 int
	allRefs                      []Reference
	stats                        map[string]tableStats
	showHelp                     bool
	helpOffset                   int
	streamDiff                   *browserDiff
	compareBase                  *diffTarget
	compareBaseLabel             string
	history                      *browserHistory
	pendingFileDelete            *fileDeletion
	deleteOffset                 int
	searchInput                  *string
	search                       *browserSearch
	searchHistory                *browserHistory
	searchParent                 *browserSearch

	tree            map[string][]browserEntry
	directoryTables map[string]listingTable
	sessionTable    *listingTable
	directorySizes  map[string]int64
	directoryCounts map[string]int
	directoryErrors map[string]int

	redraw func()
	browser

	refs                  []Reference
	ref                   *Reference
	files                 []sessionFile
	entries               []browserEntry
	current               string
	cursor, sessionCursor int
	parents               []browserPosition
	detail                *sessionFile
	pendingDelete         *Reference
	pendingDeletePath     string
	restoreToInput        *string
	message               string

	diffOffset   int
	diffColumn   int
	diffPageSize int
	diffWidth    int
}

func (s *Store) browse(input io.Reader, output io.Writer, opener func(string) error) error {
	refs, err := s.sessionReferences()
	if err != nil {
		return err
	}
	refs, err = filterSessionDates(refs, s.sessionDate)
	if err != nil {
		return err
	}
	ui := browserUI{sources: true, browser: browser{store: s, open: opener}, refs: groupedReferences(refs), stats: s.referenceStats(refs)}
	return ui.run(input, output, false)
}

func (ui *browserUI) run(input io.Reader, output io.Writer, standalone bool) error {
	defer ui.closeDiff()
	terminal := false
	if in, ok := input.(*os.File); ok && term.IsTerminal(int(in.Fd())) {
		state, err := term.MakeRaw(int(in.Fd()))
		if err != nil {
			return err
		}
		defer term.Restore(int(in.Fd()), state)
		if out, ok := output.(*os.File); ok && term.IsTerminal(int(out.Fd())) {
			restore, err := enableBrowserOutput(out)
			if err != nil {
				return err
			}
			defer restore()
			terminal = true
			fmt.Fprint(output, "\x1b[?1049h\x1b[?25l")
			defer fmt.Fprint(output, "\x1b[?25h\x1b[?1049l")
		}
	}
	ui.redraw = func() {
		if terminal {
			w, h, err := term.GetSize(int(output.(*os.File).Fd()))
			if err != nil {
				w, h = 100, 24
			}
			fmt.Fprint(output, "\x1b[H\x1b[2J", strings.ReplaceAll(ui.view(w, h), "\n", "\r\n"))
		} else {
			fmt.Fprintln(output, ui.message)
		}
	}
	keys := bufio.NewReader(input)
	type keyResult struct {
		key string
		err error
	}
	type keyMode struct{ editing, calendar bool }
	requests := make(chan keyMode)
	results := make(chan keyResult)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case mode := <-requests:
				var key string
				var err error
				if mode.calendar {
					key, err = readCalendarKey(keys)
				} else if mode.editing {
					key, err = readDestinationKey(keys)
				} else {
					key, err = readBrowserKey(keys)
				}
				select {
				case results <- keyResult{key, err}:
				case <-stop:
					return
				}
			}
		}
	}()
	ticker := time.NewTicker(40 * time.Millisecond)
	defer ticker.Stop()
	for {
		width, height := 100, 24
		if terminal {
			if w, h, err := term.GetSize(int(output.(*os.File).Fd())); err == nil {
				width, height = w, h
			}
		}
		frame := ui.view(width, height)
		if terminal {
			fmt.Fprint(output, "\x1b[H\x1b[2J", strings.ReplaceAll(frame, "\n", "\r\n"))
		} else {
			fmt.Fprint(output, frame)
		}
		editing := ui.restoreToInput != nil || ui.searchInput != nil || ui.filterInput != nil || (ui.streamDiff != nil && ui.streamDiff.input != nil)
		// Scripted input should see the requested page before its next key.
		if !terminal && ui.streamDiff != nil {
			for d := ui.streamDiff; !d.spool.done && (d.spool.count < ui.diffOffset+ui.diffPageSize+1 || d.ending || d.scanning); {
				fmt.Fprint(output, ui.view(width, height))
			}
		}
		requests <- keyMode{editing: editing, calendar: ui.datePicker != nil}
		var result keyResult
	waitKey:
		for {
			select {
			case result = <-results:
				break waitKey
			case <-ticker.C:
				if terminal {
					d := ui.streamDiff
					resized := false
					if w, h, e := term.GetSize(int(output.(*os.File).Fd())); e == nil && (w != width || h != height) {
						width, height = w, h
						resized = true
					}
					if resized || d != nil && (d.scanning || d.ending || !d.spool.done && d.spool.count < ui.diffOffset+ui.diffPageSize+1) {
						fmt.Fprint(output, "\x1b[H\x1b[2J", strings.ReplaceAll(ui.view(width, height), "\n", "\r\n"))
					}
				}
			}
		}
		key, err := result.key, result.err
		if errors.Is(err, io.EOF) {
			return ui.viewerError(standalone)
		}
		if err != nil {
			return err
		}
		if (!editing && key == "q") || key == "ctrl+c" {
			return ui.viewerError(standalone)
		}
		viewErr := ui.viewerError(standalone)
		ui.key(key)
		if standalone && ui.streamDiff == nil {
			return viewErr
		}
	}
}
func readBrowserKey(r *bufio.Reader) (string, error) {
	return readNavigationKey(r, "left")
}

func readNavigationKey(r *bufio.Reader, escape string) (string, error) {
	b, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	switch b {
	case 3:
		return "ctrl+c", nil
	case 4:
		return "halfdown", nil
	case 21:
		return "halfup", nil
	case 6:
		return "pagedown", nil
	case 2:
		return "pageup", nil
	case 'G':
		return "end", nil
	case 'g':
		return "home", nil
	case '\r', '\n':
		return "right", nil
	case 8, 127:
		return "left", nil
	case 27:
		if r.Buffered() == 0 {
			return escape, nil
		}
		first, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if first != '[' && first != 'O' {
			r.UnreadByte()
			return escape, nil
		}
		sequence := make([]byte, 0, 8)
		for len(sequence) < 16 {
			c, err := r.ReadByte()
			if err != nil {
				return "", err
			}
			sequence = append(sequence, c)
			if c >= '@' && c <= '~' {
				break
			}
		}
		switch string(sequence) {
		case "A":
			return "up", nil
		case "B":
			return "down", nil
		case "C":
			return "right", nil
		case "D":
			return "left", nil
		case "H", "1~", "7~":
			return "home", nil
		case "F", "4~", "8~":
			return "end", nil
		case "5~":
			return "pageup", nil
		case "6~":
			return "pagedown", nil
		case "3~":
			return "delete", nil
		}
		return "", nil
	default:
		if b == 'X' || b == 'N' || b == 'D' {
			return string(b), nil
		}
		return strings.ToLower(string(b)), nil
	}
}
func (u *browserUI) count() int {
	if u.detail != nil {
		return 0
	}
	if u.ref == nil {
		if u.sources {
			return len(u.sourceNames())
		}
		return len(u.refs)
	}
	return len(u.entries)
}
func (u *browserUI) key(key string) {
	if u.filterKey(key) {
		return
	}
	if u.datePicker != nil {
		u.datePickerKey(key)
		return
	}
	if key == "m" && !u.showHelp && u.filterInput == nil && u.searchInput == nil && u.restoreToInput == nil && u.streamDiff == nil && u.pendingDelete == nil && u.pendingFileDelete == nil {
		u.toggleMergedMode()
		return
	}
	if key == "D" && !u.showHelp && u.filterInput == nil && u.searchInput == nil && u.restoreToInput == nil && u.streamDiff == nil && u.pendingDelete == nil && u.pendingFileDelete == nil {
		u.openDatePicker()
		return
	}
	if u.showHelp {
		switch key {
		case "?", "cancel", "left", "right", "b":
			u.showHelp = false
		case "down", "j":
			u.helpOffset++
		case "up", "k":
			u.helpOffset = max(0, u.helpOffset-1)
		case "pagedown":
			u.helpOffset += 10
		case "pageup":
			u.helpOffset = max(0, u.helpOffset-10)
		}
		return
	}
	if key == "?" && u.filterInput == nil && u.searchInput == nil && u.restoreToInput == nil && (u.streamDiff == nil || u.streamDiff.input == nil) {
		u.showHelp = true
		u.helpOffset = 0
		return
	}

	if u.streamDiff != nil {
		u.streamDiffKey(key)
		return
	}
	if u.pendingFileDelete != nil {
		switch key {
		case "y":
			u.confirmFileDelete()
		case "n", "right", "left", "b":
			u.pendingFileDelete = nil
			u.message = "Deletion cancelled"
		case "down", "j":
			u.deleteOffset++
		case "up", "k":
			u.deleteOffset = max(0, u.deleteOffset-1)
		case "pagedown":
			u.deleteOffset += 10
		case "pageup":
			u.deleteOffset = max(0, u.deleteOffset-10)
		}
		return
	}

	if u.searchInput != nil {
		u.searchInputKey(key)
		return
	}
	if u.restoreToInput != nil {
		u.destinationKey(key)
		return
	}

	if u.merged != nil && u.detail != nil && u.history != nil {
		u.mergedDetailKey(key)
		return
	}
	if u.history != nil {
		u.historyKey(key)
		return
	}
	if u.search != nil && u.pendingDelete == nil {
		u.searchKey(key)
		return
	}
	if u.pendingDelete != nil {
		switch key {
		case "y":
			u.confirmDelete()
		case "n", "right", "left", "b":
			u.pendingDelete = nil
			u.message = "Deletion cancelled"
		}
		return
	}

	if u.merged != nil {
		u.mergedKey(key)
		return
	}
	switch key {
	case "j":
		key = "down"
	case "k":
		key = "up"
	case "h":
		key = "left"
	case "l":
		key = "right"
	}
	if u.detail != nil {
		switch key {
		case "left", "b":
			u.back()
		case "r", "o":
			u.restoreSelection(key == "o")
		case "t":
			u.beginRestoreTo()
		case "x", "delete":
			u.requestDelete()
		case "X":
			u.requestFileDelete()
		case "c":
			u.showContent()
		case "d":
			u.compare()
		case "v":
			u.showHistory()
		}
		return
	}
	switch key {
	case "up":
		u.cursor = max(0, u.cursor-1)
	case "down":
		u.cursor = min(max(0, u.count()-1), u.cursor+1)
	case "home":
		u.cursor = 0
	case "end":
		u.cursor = max(0, u.count()-1)
	case "pageup":
		u.cursor = max(0, u.cursor-10)
	case "pagedown":
		u.cursor = min(max(0, u.count()-1), u.cursor+10)
	case "right":
		u.enter()
	case "left", "b":
		u.back()
	case "r", "o":
		u.restoreSelection(key == "o")
	case "t":
		u.beginRestoreTo()
	case "n":
		text := ""
		u.searchInput = &text
		u.message = ""
	case "c":
		u.showContent()
	case "d":
		u.compare()
	case "x", "delete":
		u.requestDelete()
	case "X":
		u.requestFileDelete()
	case "v":
		u.showHistory()
	case "f":
		if u.ref == nil {
			refs, err := u.store.sessionReferences()
			if err != nil {
				u.message = "Error: " + err.Error()
			} else {
				cursor := u.cursor
				u.replaceReferences(refs)
				u.cursor = min(cursor, max(0, u.count()-1))
				u.clearComparison()
				u.message = "Sessions refreshed"
			}
		}
	}
}
func (u *browserUI) enter() {
	if u.detail != nil || u.count() == 0 {
		return
	}
	if u.ref == nil {
		if u.sources {
			u.sourceCursor = u.cursor
			u.selectSource(u.sourceNames()[u.cursor])
			u.message = ""
			return
		}
		ref := u.refs[u.cursor]
		session, files, err := u.store.listingFiles(ref.Session)
		if err != nil {
			u.message = "Error: " + err.Error()
			return
		}
		ref.Comment = session.Comment
		u.sessionCursor = u.cursor
		u.ref = &ref
		u.files = files
		u.current = ""
		u.cursor = 0
		u.parents = nil
		u.prepareTree()
		u.entries = u.tree[""]
		u.message = ""
		if len(session.Errors) > 0 {
			u.message = fmt.Sprintf("Incomplete backup: %d errors", len(session.Errors))
		}
		return
	}
	entry := u.entries[u.cursor]
	if entry.directory {
		u.parents = append(u.parents, browserPosition{u.current, u.cursor})
		u.current = path.Join(u.current, entry.name)
		u.cursor = 0
		u.prepareTree()
		u.entries = u.tree[u.current]
	} else {
		var metadata File
		if err := u.store.read(entry.file.id, 'F', &metadata); err != nil {
			u.message = "Error: " + err.Error()
			return
		}
		full := *entry.file
		full.metadata = metadata
		u.detail = &full
	}
}
func (u *browserUI) back() {
	if u.detail != nil {
		u.detail = nil
		return
	}
	if u.ref == nil {
		if u.source != "" {
			u.refs = u.allRefs
			u.allRefs = nil
			u.source = ""
			u.sources = true
			u.cursor = min(u.sourceCursor, max(0, u.count()-1))
			u.sessionTable = nil
		}
		u.clearComparison()
		u.message = ""
		return
	}
	if len(u.parents) > 0 {
		p := u.parents[len(u.parents)-1]
		u.parents = u.parents[:len(u.parents)-1]
		u.current = p.directory
		u.cursor = p.cursor
		u.prepareTree()
		u.entries = u.tree[u.current]
		return
	}
	u.ref = nil
	u.files = nil
	u.tree = nil
	u.directorySizes = nil
	u.directoryTables = nil
	u.entries = nil
	u.cursor = u.sessionCursor
	u.message = ""
}
func (u *browserUI) compare() { u.compareSelection() }

func browserText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func browserClip(s string, width int) string {
	return runewidth.Truncate(browserText(s), max(0, width), "...")
}

// Skip whole grapheme clusters so horizontal scrolling cannot split a name's accents.
func browserSkipColumns(s string, columns int) string {
	s = browserText(s)
	clusters := uniseg.NewGraphemes(s)
	skipped := 0
	for clusters.Next() {
		start, _ := clusters.Positions()
		if skipped >= columns {
			return s[start:]
		}
		skipped += runewidth.StringWidth(clusters.Str())
	}
	return ""
}

func (u *browserUI) restoreProgress(done, total int, current string) {
	u.message = progressUpdate{operation: "Restore", done: done, total: total}.text()
	if current != "" {
		u.message += "  " + current
	}
	if u.redraw != nil {
		u.redraw()
	}
}

func wrapBrowserText(s string, width int) []string {
	var lines []string
	var line strings.Builder
	used := 0
	clusters := uniseg.NewGraphemes(browserText(s))
	for clusters.Next() {
		cluster := clusters.Str()
		size := runewidth.StringWidth(cluster)
		if used+size > max(1, width) && line.Len() > 0 {
			lines = append(lines, line.String())
			line.Reset()
			used = 0
		}
		line.WriteString(cluster)
		used += size
	}
	return append(lines, line.String())
}

func (u *browserUI) help() string {
	if u.filterInput != nil {
		return "Enter filter · Esc cancel · Backspace erase · Ctrl+U clear"
	}
	if u.pendingFileDelete != nil {
		return "y delete from all sessions · n/Enter cancel · ↑/↓ scroll · q quit"
	}
	if u.searchInput != nil {
		return "Enter search · Esc cancel · Backspace erase · Ctrl+U clear"
	}
	if u.restoreToInput != nil {
		return "Enter restore · Esc cancel · Backspace erase · Ctrl+U clear"
	}

	if u.pendingDelete != nil {
		return "y delete · n/Enter cancel · q quit"
	}
	if (u.search == nil || u.merged != nil) && u.detail != nil && (u.history == nil || u.merged != nil) {
		if u.merged != nil {
			return "← back · c content · d compare · r restore · t restore-to · o open · D date · m sessions mode · q quit"
		}
		return "← back · r restore · t restore-to · o restore & open · x/Del delete · X delete file globally · c content · d compare · v history · q quit"
	}
	if u.history != nil {
		if u.merged != nil {
			if u.history.sessions {
				return "↑/↓ select · Enter session · s versions · c content · r restore · t restore-to · o open · ← back · / filter · n search · D date · m sessions mode · q quit"
			}
			return "↑/↓ select · Enter metadata · s sessions · c content · r restore · t restore-to · o open · ← back · / filter · n search · D date · m mode · q quit"
		}
		return "↑/↓ select · Enter sessions/locate · c content · d compare · r restore · t restore-to · o open · ← back · / filter · n search · q quit"
	}
	if u.search != nil && u.pendingDelete == nil {
		return "↑/↓ select · Enter locate · r restore · t restore-to · o open · x/Del delete · X delete file globally · c content · d compare · v history · / filter · n search · Esc/← back · q quit"
	}

	if u.merged != nil {
		return "↑/↓ select · Enter folder/versions · ← back · / filter · n search · D date · m sessions mode · f refresh · q quit"
	}
	if u.sources {
		return "↑/↓ select · Enter sessions · / filter · n search · D date · m files mode · f refresh · q quit"
	}
	var keys []string
	if u.count() > 1 {
		keys = append(keys, "↑/↓ select")
	}
	if u.count() > 0 {
		action := "enter"
		if u.ref != nil && !u.entries[u.cursor].directory {
			action = "details"
		}
		keys = append(keys, "→/Enter "+action)
	}
	if u.ref != nil {
		keys = append(keys, "← back")
	}
	if u.count() > 0 {
		keys = append(keys, "r restore", "t restore-to", "d compare")
		if u.ref != nil {
			keys = append(keys, "x/Del delete")
		}
	}
	if u.ref != nil && len(u.entries) > 0 && !u.entries[u.cursor].directory {
		keys = append(keys, "o restore & open", "X delete file globally", "v history", "c content")
	}
	if u.ref == nil {
		if len(u.refs) > 0 {
			keys = append(keys, "x/Del delete")
		}
		keys = append(keys, "f refresh")
	}
	return strings.Join(append(keys, "/ filter · n search", "D date", "m mode", "q quit"), " · ")
}

func (u *browserUI) prepareTree() {
	if u.tree != nil {
		return
	}
	u.tree = browserTree(u.files)
	u.directorySizes = make(map[string]int64, len(u.tree))
	u.directoryCounts = make(map[string]int)
	u.directoryErrors = make(map[string]int)
	u.directoryTables = make(map[string]listingTable, len(u.tree))
	for directory, entries := range u.tree {
		var total int64
		var values []tableStats
		for _, entry := range entries {
			total += entry.size
			u.directoryCounts[directory] += entry.count
			u.directoryErrors[directory] += entry.errors
			values = append(values, entryStats(entry))
		}
		u.directorySizes[directory] = total
		u.directoryTables[directory] = newListingTable(browserEntryNames(entries), values)
	}
}

// Preserve both the beginning and end, giving the file name more room.
// Work in terminal columns and whole graphemes, including emoji and accents.
func browserPath(s string, width int) string {
	s = browserText(s)
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	var clusters []string
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		clusters = append(clusters, g.Str())
	}
	left, used := 0, 0
	for left < len(clusters) {
		size := runewidth.StringWidth(clusters[left])
		if used+size > (width-1)/3 {
			break
		}
		used += size
		left++
	}
	right := len(clusters)
	remaining := width - 1 - used
	for right > left {
		size := runewidth.StringWidth(clusters[right-1])
		if size > remaining {
			break
		}
		remaining -= size
		right--
	}
	return strings.Join(clusters[:left], "") + "…" + strings.Join(clusters[right:], "")
}

func (u *browserUI) briefHelp() string {
	if u.pendingDelete != nil || u.pendingFileDelete != nil || u.filterInput != nil || u.searchInput != nil || u.restoreToInput != nil {
		return u.help()
	}
	if (u.search == nil || u.merged != nil) && u.detail != nil && (u.history == nil || u.merged != nil) {
		return "← back · c content · r restore · t restore-to · ? help · q quit"
	}
	if u.history != nil {
		if u.merged != nil && !u.history.sessions {
			return "↑/↓ select · Enter metadata · s sessions · ← back · / filter · n search · ? help · q quit"
		}
		return "↑/↓ select · Enter sessions/locate · ← back · / filter · n search · ? help · q quit"
	}
	if u.search != nil {
		return "↑/↓ select · Enter locate · ← back · / filter · n search · ? help · q quit"
	}
	if u.merged != nil {
		return "↑/↓ select · Enter folder/versions · ← back · / filter · n search · D date · m sessions mode · f refresh · ? help · q quit"
	}
	if u.sources {
		return "↑/↓ select · Enter sessions · / filter · n search · D date · m files mode · f refresh · ? help · q quit"
	}
	if u.ref == nil {
		return "↑/↓ select · Enter session · ← roots · / filter · n search · D date · m files mode · ? help · q quit"
	}
	if len(u.entries) > 0 && !u.entries[u.cursor].directory {
		return "↑/↓ select · Enter metadata · ← back · / filter · n search · ? help · q quit"
	}
	return "↑/↓ select · Enter folder · ← back · / filter · n search · ? help · q quit"
}
