package hobalight

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/term"
)

// Keep two clipped rows on a terminal; ordinary streams receive plain log lines.
type backupDisplay struct {
	out           io.Writer
	source        string
	root          string
	terminal      bool
	fd            int
	started, last time.Time
	progress      progressUpdate
}

func newBackupDisplay(out io.Writer, source string) *backupDisplay {
	d := &backupDisplay{out: out, root: source, source: browserText(portable(source)), started: time.Now(), progress: progressUpdate{operation: "Backup", total: -1}}
	if info, err := os.Stat(source); err == nil && info.Mode().IsRegular() {
		d.root = filepath.Dir(source)
	}
	if f, ok := out.(*os.File); ok {
		d.fd = int(f.Fd())
		d.terminal = term.IsTerminal(d.fd)
	}
	d.draw("Backing up: "+d.source, false)
	return d
}
func (d *backupDisplay) update(p progressUpdate) {
	previous := d.progress
	d.progress = p
	if p.total >= 0 && p.done == p.total {
		return
	} // Finalize only after publishing the session.
	if time.Since(d.last) < 150*time.Millisecond && previous.total == p.total {
		return
	}
	d.draw("Backing up: "+d.source, false)
}
func (d *backupDisplay) finish(id string, err error) {
	label := "Backup finished"
	if err != nil {
		label = "Backup finished with errors"
	}
	if id == "" || (err != nil && ExitCode(err) == 1) {
		label = "Backup failed"
	}
	header := label + ": " + d.source
	if id != "" {
		header += fmt.Sprintf(" (%.8s)", id)
	}
	d.progress.current = ""
	d.draw(header, true)
}
func (d *backupDisplay) draw(header string, final bool) {
	row := d.progress.text() + " [" + elapsedText(time.Since(d.started)) + "]"
	if !final && d.progress.current != "" {
		current := d.progress.current
		if relative, err := filepath.Rel(d.root, current); err == nil {
			current = relative
		}
		row += "  " + browserText(portable(current))
	}
	if d.terminal {
		if width, _, err := term.GetSize(d.fd); err == nil {
			header = browserClip(header, max(1, width-1))
			row = browserClip(row, max(1, width-1))
		}
		if !d.last.IsZero() {
			fmt.Fprint(d.out, "\r\x1b[1A")
		}
		fmt.Fprintf(d.out, "\r\x1b[K%s\n\r\x1b[K%s", header, row)
		if final {
			fmt.Fprintln(d.out)
		}
	} else {
		if d.last.IsZero() || final {
			fmt.Fprintln(d.out, header)
		}
		fmt.Fprintln(d.out, row)
	}
	d.last = time.Now()
}
