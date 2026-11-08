package hobalight

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

func (u *browserUI) applySessionDate(day string) {
	refs, err := u.store.sessionReferences()
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	refs, err = filterSessionDates(refs, day)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	wasMerged := u.merged != nil
	u.store.sessionDate = day
	u.refs = groupedReferences(refs)
	u.stats = u.store.referenceStats(refs)
	u.sources = true
	u.source = ""
	u.allRefs = nil
	u.ref = nil
	u.files = nil
	u.entries = nil
	u.tree = nil
	u.directoryTables = nil
	u.cursor = 0
	u.sessionCursor = 0
	u.sourceCursor = 0
	u.parents = nil
	u.detail = nil
	u.search = nil
	u.history = nil
	u.searchHistory = nil
	u.searchParent = nil
	u.sessionTable = nil
	u.closeDatePicker()
	u.clearComparison()
	if wasMerged {
		merged, err := u.store.mergedBrowser()
		if err != nil {
			u.merged = nil
			u.message = "Error: " + err.Error()
			return
		}
		u.merged = merged
	}
	u.message = "Date filter cleared"
	if day != "" {
		u.message = "Session date: " + day + " (local time)"
	}
}
func shiftPickerMonth(day time.Time, months int) time.Time {
	first := time.Date(day.Year(), day.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(day.Day(), last)-1)
}
func (u *browserUI) datePickerKey(key string) {
	day := *u.datePicker
	if key == "toggle-date-list" {
		u.dateRootsFocus = !u.dateRootsFocus
		return
	}
	if u.dateRootsFocus {
		switch key {
		case "up", "k":
			u.dateRootOffset = max(0, u.dateRootOffset-1)
			return
		case "down", "j":
			u.dateRootOffset++
			return
		case "pageup":
			u.dateRootOffset = max(0, u.dateRootOffset-max(1, u.dateRootPage))
			return
		case "pagedown":
			u.dateRootOffset += max(1, u.dateRootPage)
			return
		case "home":
			u.dateRootOffset = 0
			return
		case "end":
			u.dateRootOffset = len(browserSourceTree(browserSourceNames(u.dateReferences[day.Format(time.DateOnly)])))
			return
		}
	}
	switch key {
	case "cancel", "D":
		u.closeDatePicker()
		return
	case "right", "l":
		day = day.AddDate(0, 0, 1)
	case "left", "h":
		day = day.AddDate(0, 0, -1)
	case "up", "k":
		day = day.AddDate(0, 0, -7)
	case "down", "j":
		day = day.AddDate(0, 0, 7)
	case "pageup":
		day = shiftPickerMonth(day, -1)
	case "pagedown":
		day = shiftPickerMonth(day, 1)
	case "home":
		day = localCalendarDay()
	case "c":
		u.applySessionDate("")
		return
	// Enter is normalized to right by the navigation decoder. A distinct key
	// is handled by readBrowserKey when the calendar is open.
	case "apply-date":
		u.applySessionDate(day.Format(time.DateOnly))
		return
	}
	if day.Year() >= 1 && day.Year() <= 9999 {
		if !day.Equal(*u.datePicker) {
			u.dateRootOffset = 0
		}
		u.datePicker = &day
	}
}
func calendarLines(day time.Time) []string {
	first := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	offset := (int(first.Weekday()) + 6) % 7
	days := first.AddDate(0, 1, -1).Day()
	lines := []string{day.Format("January 2006"), " Mo  Tu  We  Th  Fr  Sa  Su"}
	var row strings.Builder
	for cell := 0; cell < ((offset+days+6)/7)*7; cell++ {
		date := cell - offset + 1
		if date < 1 || date > days {
			row.WriteString("    ")
		} else if date == day.Day() {
			fmt.Fprintf(&row, "[%2d]", date)
		} else {
			fmt.Fprintf(&row, " %2d ", date)
		}
		if cell%7 == 6 {
			lines = append(lines, row.String())
			row.Reset()
		}
	}
	return append(lines, "", day.Format(time.DateOnly)+" (local time)")
}

func readCalendarKey(reader *bufio.Reader) (string, error) {
	next, err := reader.Peek(1)
	if err != nil {
		return "", err
	}
	if next[0] == '\t' {
		_, err = reader.ReadByte()
		return "toggle-date-list", err
	}
	if next[0] == '\r' || next[0] == '\n' {
		_, err = reader.ReadByte()
		return "apply-date", err
	}
	return readNavigationKey(reader, "cancel")
}

func (u *browserUI) openDatePicker() {
	refs, err := u.store.sessionReferences()
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	grouped, err := groupCalendarReferences(refs)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	day := localCalendarDay()
	if u.store.sessionDate != "" {
		day, _ = parseSessionDate(u.store.sessionDate)
	}
	u.datePicker = &day
	u.dateReferences = grouped
	u.dateRootOffset, u.dateRootPage = 0, 1
	u.dateRootsFocus = false
	u.message = ""
}

func groupCalendarReferences(refs []Reference) (map[string][]Reference, error) {
	grouped := map[string][]Reference{}
	for _, ref := range groupedReferences(refs) {
		at, err := time.Parse(time.RFC3339Nano, ref.Started)
		if err != nil {
			return nil, fmt.Errorf("invalid session start time: %w", err)
		}
		day := at.In(time.Local).Format(time.DateOnly)
		grouped[day] = append(grouped[day], ref)
	}
	return grouped, nil
}

func (u *browserUI) closeDatePicker() {
	u.datePicker = nil
	u.dateReferences = nil
	u.dateRootOffset, u.dateRootPage = 0, 0
	u.dateRootsFocus = false
}

func (u *browserUI) datePickerHelp() []string {
	navigation := "←/→ day · ↑/↓ week · PgUp/PgDn month · Tab roots"
	actions := "Enter apply · c clear filter · Esc cancel · Home today · q quit"
	if u.dateRootsFocus {
		navigation = "↑/↓ scroll roots · PgUp/PgDn page · Tab calendar"
		actions = "Enter apply · c clear filter · Esc cancel · q quit"
	}
	return []string{navigation, actions}
}

// Calendar rows and the root table share one frame. Rule indices are explicit
// so a root name cannot accidentally become a separator.
func (u *browserUI) datePickerView(width, height int) ([]string, []int) {
	day := *u.datePicker
	calendar := calendarLines(day)
	calendar = calendar[:len(calendar)-2]
	footerRows := 0
	for _, help := range u.datePickerHelp() {
		footerRows += len(wrapBrowserHelp(help, max(8, width-3)))
	}
	headerRows := 2 + len(wrapBrowserBreadcrumb("Select session date (local time) · "+day.Format(time.DateOnly), max(8, width-3)))
	if height < 14 {
		headerRows--
	}
	// Keep a useful tree viewport even when the terminal is short.
	if height-headerRows-2-footerRows-len(calendar)-3-1 < 2 {
		for _, row := range calendar[2:] {
			if strings.Contains(row, "[") {
				calendar = []string{calendar[0], calendar[1], row}
				break
			}
		}
	}
	visible := max(1, height-headerRows-2-footerRows-len(calendar)-3-1)
	u.dateRootPage = visible
	refs := u.dateReferences[day.Format(time.DateOnly)]
	preview := browserUI{refs: refs, cursor: -1}
	table, _ := preview.sourceTreeTable(max(8, width-3))
	if len(refs) == 0 {
		table[1] = "No backups on this date."
	}
	u.dateRootOffset = min(u.dateRootOffset, max(0, len(table)-1-visible))
	first := 1 + u.dateRootOffset
	end := min(len(table), first+visible)
	lines := append(calendar, strings.TrimPrefix(table[0], " "))
	for _, row := range table[first:end] {
		lines = append(lines, strings.TrimPrefix(row, " "))
	}
	return lines, []int{len(calendar) - 1, len(calendar)}
}
