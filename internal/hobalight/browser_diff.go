package hobalight

import (
	"context"
	"fmt"
	"io"
	"strings"
)

type browserDiff struct {
	contentID                string
	parent                   *browserDiff
	savedOffset, savedColumn int
	backward                 bool
	spool                    *diffSpool
	a, b                     diffTarget
	mode, query              string
	input                    *string
	// Long scans advance in fixed row batches; the user can cancel between them.
	scanning bool
	ending   bool
}

func (u *browserUI) closeDiff() {
	if u.streamDiff != nil {
		for d := u.streamDiff; d != nil; d = d.parent {
			d.spool.Close()
		}
		u.streamDiff = nil
	}
}
func (u *browserUI) startDiff(a, b diffTarget, mode string) {
	spool, err := newDiffSpool(func(ctx context.Context, w io.Writer) error { return u.store.diffTargets(ctx, a, b, mode, w) })
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	u.closeDiff()
	u.streamDiff = &browserDiff{spool: spool, a: a, b: b, mode: mode}
	u.diffOffset, u.diffColumn = 0, 0
	u.message = ""
}
func (u *browserUI) compareSelection() {
	id, p, _, err := u.selectedRestore()
	if err != nil {
		u.message = err.Error()
		return
	}
	next := diffTarget{id: id, path: p}
	switch {
	case u.history != nil:
		m, ok := u.history.selected()
		if ok {
			next = diffTarget{id: m.file.id}
		}
	case u.search != nil && len(u.search.matches) > 0:
		next = diffTarget{id: u.search.matches[u.search.cursor].file.id}
	case u.detail != nil:
		next = diffTarget{id: u.detail.id}
	case u.ref != nil && len(u.entries) > 0 && !u.entries[u.cursor].directory:
		next = diffTarget{id: u.entries[u.cursor].file.id}
	}
	if u.compareBase == nil {
		u.compareBase = &next
		u.compareBaseLabel = next.path
		if validID(next.id, 'F') {
			var f File
			if u.store.read(next.id, 'F', &f) == nil {
				u.compareBaseLabel = f.Path
			}
		} else {
			var session Session
			if u.store.read(next.id, 'S', &session) == nil {
				u.compareBaseLabel = strings.TrimRight(session.Directory, "/") + "/" + next.path
			}
		}

		u.message = "Comparison base selected; choose another item and press d"
		return
	}
	if *u.compareBase == next {
		u.clearComparison()
		u.message = "Comparison selection cleared"
		return
	}
	a := *u.compareBase
	u.clearComparison()
	u.startDiff(a, next, "auto")
}
func (u *browserUI) streamDiffKey(key string) {
	d := u.streamDiff
	if d.input != nil {
		switch key {
		case "cancel":
			d.input = nil
		case "clear":
			*d.input = ""
		case "backspace":
			r := []rune(*d.input)
			if len(r) > 0 {
				*d.input = string(r[:len(r)-1])
			}
		case "enter":
			d.query = *d.input
			d.input = nil
			d.scanning = d.query != ""
			d.backward = false
			u.diffSearch(false)
		default:
			if len(*d.input) < 1024 {
				*d.input += key
			}
		}
		return
	}
	if (key == "left" || key == "b") && (d.scanning || d.ending) {
		d.scanning, d.ending = false, false
		u.message = "Scan cancelled"
		return
	}
	d.scanning, d.ending = false, false
	page := max(1, u.diffPageSize)
	switch key {
	case "left", "b":
		parent := d.parent
		d.parent = nil
		u.closeDiff()
		u.streamDiff = parent
		if parent != nil {
			u.diffOffset, u.diffColumn = parent.savedOffset, parent.savedColumn
		}
		u.message = ""
	case "up", "k":
		u.diffOffset = max(0, u.diffOffset-1)
	case "down", "j":
		u.diffOffset++
	case "pageup":
		u.diffOffset = max(0, u.diffOffset-page)
	case "pagedown":
		u.diffOffset += page
	case "halfup":
		u.diffOffset = max(0, u.diffOffset-max(1, page/2))
	case "halfdown":
		u.diffOffset += max(1, page/2)
	case "home":
		u.diffOffset = 0
	case "end":
		d.ending = true
	case "h":
		u.diffColumn = max(0, u.diffColumn-4)
	case "l":
		u.diffColumn += 4
	case "/":
		text := ""
		d.input = &text
	case "n":
		d.scanning = d.query != ""
		d.backward = false
		u.diffSearch(false)
	case "N":
		d.scanning = d.query != ""
		d.backward = true
		u.diffSearch(true)
	case "a", "t", "x":
		mode := map[string]string{"a": "auto", "t": "text", "x": "hex"}[key]
		parent := d.parent
		d.parent = nil
		if d.contentID != "" {
			u.startContent(d.contentID, mode)
		} else {
			u.startDiff(d.a, d.b, mode)
		}
		u.streamDiff.parent = parent
	case "right":
		u.openDiffRow()
	}
}
func (u *browserUI) diffSearch(back bool) {
	d := u.streamDiff
	if d.query == "" {
		return
	}
	start := u.diffOffset + 1
	step := 1
	if back {
		start = u.diffOffset - 1
		step = -1
	}
	limit := start + 256
	if !back {
		d.spool.ensure(limit)
		limit = min(limit, d.spool.count)
	}
	for n, i := 0, start; n < 256 && i >= 0 && i < d.spool.count; n, i = n+1, i+step {
		if !back && i >= limit {
			break
		}
		if strings.Contains(strings.ToLower(d.spool.row(i)), strings.ToLower(d.query)) {
			u.diffOffset = i
			d.scanning = false
			u.message = "Match: " + d.query
			return
		}
		u.diffOffset = i
	}
	if back && u.diffOffset == 0 || !back && d.spool.done && u.diffOffset >= d.spool.count-1 {
		d.scanning = false
		u.message = "No further matches"
	}

}
func (u *browserUI) streamDiffView(width, height int) ([]string, []string, []string) {
	d := u.streamDiff
	page := max(1, height-10)
	u.diffPageSize = page
	u.diffWidth = max(8, width-5)
	if d.ending {
		d.spool.ensure(d.spool.count + 256)
		u.diffOffset = max(0, d.spool.count-page)
		if d.spool.done {
			u.message = "End of comparison"
			if d.contentID != "" {
				u.message = "End of file"
			}
			d.ending = false
		}
	}
	if d.scanning {
		u.diffSearch(d.backward)
	}
	d.spool.ensure(u.diffOffset + page + 1)
	if d.spool.done {
		u.diffOffset = min(u.diffOffset, max(0, d.spool.count-1))
	}
	end := min(d.spool.count, u.diffOffset+page)
	total := fmt.Sprint(d.spool.count)
	if !d.spool.done {
		total += "+"
	}
	title := "Comparison"
	if d.contentID != "" {
		title = "Content"
	}
	header := []string{fmt.Sprintf(title+" · %s · lines %d-%d/%s", d.mode, u.diffOffset+1, end, total), ""}
	var rows []string
	for i := u.diffOffset; i < end; i++ {
		rows = append(rows, selectionMarker(i == u.diffOffset)+browserSkipColumns(d.spool.row(i), u.diffColumn))
	}
	if d.spool.err != nil {
		u.message = "Error: " + d.spool.err.Error()
	}
	if d.input != nil {
		header = append(header, "Search: "+*d.input+"█")
	}
	if d.scanning || d.ending {
		u.message = "Scanning in batches · Esc cancels"
	}

	return header, rows, []string{"↑/↓ scroll · / search · ← back · ? help · q quit"}
}
func (u *browserUI) openDiffRow() {
	d := u.streamDiff
	line := d.spool.row(u.diffOffset)
	if d.parent != nil || !validID(d.a.id, 'S') || !validID(d.b.id, 'S') || len(line) < 3 || !strings.Contains("~-+", line[:1]) || line[1] != ' ' {
		return
	}
	cut := strings.LastIndex(line, "  ")
	if cut < 2 {
		return
	}
	name := line[2:cut]
	// A directory diff row is resolved against each selected root. Missing files
	// are reported explicitly rather than accidentally comparing another path.
	a, b := d.a, d.b
	if a.path != "" {
		a.path += "/"
	}
	if b.path != "" {
		b.path += "/"
	}
	a.path += name
	b.path += name
	if line[0] == '+' {
		a.empty = true
	}
	if line[0] == '-' {
		b.empty = true
	}
	d.savedOffset, d.savedColumn = u.diffOffset, u.diffColumn
	u.streamDiff = nil
	u.startDiff(a, b, d.mode)
	if u.streamDiff == nil {
		u.streamDiff = d
	} else {
		u.streamDiff.parent = d
	}
}

func (u *browserUI) isComparisonBase(id, p string) bool {
	return u.compareBase != nil && u.compareBase.id == id && u.compareBase.path == p
}

func (u *browserUI) clearComparison() {
	u.compareBase = nil
	u.compareBaseLabel = ""
}
