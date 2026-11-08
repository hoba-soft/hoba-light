package hobalight

import (
	"fmt"
	"path"
	"strings"

	"github.com/mattn/go-runewidth"
)

func (u *browserUI) contentFooter(width int) []string {
	if u.streamDiff.input != nil {
		return []string{browserClip("/"+*u.streamDiff.input+"█", width), "Enter search · Esc cancel"}
	}
	return []string{"↑/↓ move · / search · ← back · ? help · q quit"}
}

func (u *browserUI) contentContext(width int) string {
	d := u.streamDiff
	name := d.contentID[:min(8, len(d.contentID))]
	label := "Content (" + d.mode + "): "
	if len(d.spool.header) > 0 {
		if _, filename, ok := strings.Cut(d.spool.header[0], "  "); ok {
			name = path.Base(strings.ReplaceAll(filename, `\`, "/"))
		}
	}
	if len(d.spool.header) > 2 {
		label = d.spool.header[2] + " "
	}
	total := fmt.Sprint(d.spool.count)
	if !d.spool.done {
		total += "+"
	}
	position := fmt.Sprintf(" · line %d/%s", u.diffOffset+1, total)
	if d.spool.count == 0 {
		position = " · loading..."
		if d.spool.done {
			position = " · 0 lines"
		}
	}
	if u.message != "" && !strings.HasPrefix(u.message, "Error:") {
		room := width - runewidth.StringWidth(label+position) - 8
		if room > 4 {
			position += " · " + browserClip(u.message, room-3)
		}
	}
	name = browserPath(name, max(1, width-runewidth.StringWidth(label+position)))
	return browserClip(label+name+position, width)
}

// The cursor and viewport are independent: moving inside the window never
// discards its earlier rows. Only crossing an edge scrolls the window.
func (u *browserUI) contentView(width, height int) ([]string, []string, []string) {
	d := u.streamDiff
	d.spool.ensure(u.diffOffset + 1)
	footer := u.contentFooter(width)
	footerRows := 0
	for _, line := range footer {
		footerRows += len(wrapBrowserHelp(line, width))
	}
	titleRows := 2
	if height < 14 {
		titleRows = 1
	}
	// One context row, two frame edges, and the terminal's final newline.
	page := max(1, height-titleRows-footerRows-4)
	if d.spool.err != nil {
		page = max(1, page-1)
	}
	u.diffPageSize, u.diffWidth = page, width
	if d.ending {
		d.spool.ensure(d.spool.count + 256)
		u.diffOffset = max(0, d.spool.count-1)
		if d.spool.done {
			d.ending = false
			u.message = "End of file"
		}
	}
	if d.scanning {
		u.diffSearch(d.backward)
	}
	d.spool.ensure(max(d.contentTop+page, u.diffOffset+1) + 1)
	if d.spool.done {
		u.diffOffset = min(u.diffOffset, max(0, d.spool.count-1))
	}
	if u.diffOffset < d.contentTop {
		d.contentTop = u.diffOffset
	}
	if u.diffOffset >= d.contentTop+page {
		d.contentTop = u.diffOffset - page + 1
	}
	if d.spool.done {
		d.contentTop = min(d.contentTop, max(0, d.spool.count-page))
	}
	var rows []string
	for i := d.contentTop; i < min(d.spool.count, d.contentTop+page); i++ {
		rows = append(rows, selectionMarker(i == u.diffOffset)+browserSkipColumns(d.spool.row(i), u.diffColumn))
	}
	if d.spool.count == 0 && d.spool.err == nil {
		empty := "(loading...)"
		if d.spool.done {
			empty = "(empty file)"
		}
		rows = append(rows, " "+empty)
	}
	for len(rows) < page {
		rows = append(rows, " ")
	}
	if d.spool.err != nil {
		u.message = "Error: " + d.spool.err.Error()
	}
	if d.scanning || d.ending {
		u.message = "Scanning in batches · Esc cancels"
	}
	return []string{u.contentContext(width)}, rows, footer
}
