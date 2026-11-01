package hobalight

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

type progressUpdate struct {
	operation   string
	done, total int
	current     string
	errors      int
	addedBytes  int64
}

func (p progressUpdate) text() string {
	label := p.operation
	if label == "Backup" {
		label = "Progress"
	}
	if p.total < 0 && p.operation == "Verify" {
		return fmt.Sprintf("Verify: %d objects checked, %d issues", p.done, p.errors)
	}
	if p.total < 0 {
		return fmt.Sprintf("%s: counting files (%d found)", label, p.done)
	}
	percent := 100
	if p.total > 0 {
		percent = 100 * p.done / p.total
	}
	text := fmt.Sprintf("%s: %d/%d (%d%%)", label, p.done, p.total, percent)
	if p.errors > 0 {
		suffix := "errors"
		if p.errors == 1 {
			suffix = "error"
		}
		text += fmt.Sprintf("  %d %s", p.errors, suffix)
	}
	if p.operation == "Backup" {
		text += "  " + displaySize(p.addedBytes) + " added"
	}
	return text
}
func (s *Store) report(operation string, done, total int, current string) {
	if s.progress != nil {
		s.progress(progressUpdate{operation: operation, done: done, total: total, current: current})
	}
}
func elapsedText(elapsed time.Duration) string {
	seconds := max(0, int64(elapsed/time.Second))
	return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}

func progressPrinter(out io.Writer) func(progressUpdate) {
	return progressPrinterWithClock(out, time.Now)
}

func progressPrinterWithClock(out io.Writer, now func() time.Time) func(progressUpdate) {
	var last, started time.Time
	lastOperation := ""
	lastTotal := -2
	terminal := false
	if f, ok := out.(*os.File); ok {
		terminal = term.IsTerminal(int(f.Fd()))
	}
	return func(p progressUpdate) {
		currentTime := now()
		if started.IsZero() || p.operation != lastOperation || (p.total < 0 && p.done == 0) {
			started = currentTime
		}
		complete := p.total >= 0 && p.done == p.total
		phaseStart := p.done == 0 && (p.operation != lastOperation || p.total != lastTotal)
		if !phaseStart && !complete && currentTime.Sub(last) < 150*time.Millisecond {
			return
		}
		last = currentTime
		lastOperation, lastTotal = p.operation, p.total
		text := p.text() + " [" + elapsedText(currentTime.Sub(started)) + "]"
		if p.current != "" && !(p.operation == "Backup" && complete) {
			text += "  " + browserText(portable(p.current))
		}
		if terminal {
			if f, ok := out.(*os.File); ok {
				if width, _, err := term.GetSize(int(f.Fd())); err == nil {
					text = browserClip(text, max(1, width-1))
				}
			}
			fmt.Fprint(out, "\r\x1b[K", text)
			if complete || p.operation == "Restore" {
				fmt.Fprintln(out)
			}
		} else {
			fmt.Fprintln(out, text)
		}
	}
}

func (s *Store) reportErrors(operation string, done, total int, current string, failures int) {
	if s.progress != nil {
		s.progress(progressUpdate{operation: operation, done: done, total: total, current: current, errors: failures})
	}
}

func (s *Store) reportBackup(done, total int, current string, failures int, added int64) {
	if s.progress != nil {
		s.progress(progressUpdate{operation: "Backup", done: done, total: total, current: current, errors: failures, addedBytes: added})
	}
}
