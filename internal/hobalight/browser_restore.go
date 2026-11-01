package hobalight

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path"
	"unicode"
)

// Destination text is read separately from navigation: preserve case, spaces,
// Unicode and literal command letters (including q, r and t).
func readDestinationKey(reader *bufio.Reader) (string, error) {
	r, _, err := reader.ReadRune()
	if err != nil {
		return "", err
	}
	switch r {
	case '\r', '\n':
		return "enter", nil
	case 8, 127:
		return "backspace", nil
	case 3:
		return "ctrl+c", nil
	case 21:
		return "clear", nil
	case 27:
		// Discard an already buffered escape sequence so arrow keys cannot leak
		// navigation commands after cancelling the prompt.
		if reader.Buffered() > 0 {
			next, _ := reader.Peek(1)
			if next[0] == '[' || next[0] == 'O' {
				_, _ = reader.ReadByte()
				for n := 0; n < 16 && reader.Buffered() > 0; n++ {
					b, _ := reader.ReadByte()
					if b >= '@' && b <= '~' {
						break
					}
				}
			}
		}
		return "cancel", nil
	}
	if unicode.IsControl(r) {
		return "", nil
	}
	return string(r), nil
}

func (u *browserUI) selectedRestore() (id, selected string, file bool, err error) {
	if u.history != nil {
		m, ok := u.history.selected()
		if !ok {
			return "", "", false, errors.New("no file versions to restore")
		}
		return m.ref.Session, m.file.relative, true, nil
	}
	if u.search != nil {
		if len(u.search.matches) == 0 {
			return "", "", false, errors.New("no files to restore")
		}
		m := u.search.matches[u.search.cursor]
		return m.ref.Session, m.file.relative, true, nil
	}
	if u.ref == nil {
		if len(u.refs) == 0 {
			return "", "", false, errors.New("no sessions to restore")
		}
		return u.refs[u.cursor].Session, "", false, nil
	}
	if u.detail != nil {
		return u.ref.Session, u.detail.relative, true, nil
	}
	if len(u.entries) == 0 {
		return "", "", false, errors.New("no files to restore")
	}
	entry := u.entries[u.cursor]
	if entry.directory {
		return u.ref.Session, path.Join(u.current, entry.name), false, nil
	}
	return u.ref.Session, entry.file.relative, true, nil
}

func (u *browserUI) restoreSelection(open bool) { u.restoreSelectionTo("", open) }

func (u *browserUI) restoreSelectionTo(destination string, open bool) {
	id, selected, isFile, err := u.selectedRestore()
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	if open && !isFile {
		u.message = "Select a file to open"
		return
	}
	previous := u.store.progress
	u.store.progress = func(p progressUpdate) { u.restoreProgress(p.done, p.total, p.current) }
	defer func() { u.store.progress = previous }()
	result, err := u.store.restoreResolved(id, selected, destination, io.Discard)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	u.message = fmt.Sprintf("Restored %d files to %s", result.count, result.destination)
	if open {
		if err := u.open(result.target); err != nil {
			u.message = "Error: open file: " + err.Error()
		} else {
			u.message = "Opened " + result.target
		}
	}
}

func (u *browserUI) beginRestoreTo() {
	if _, _, _, err := u.selectedRestore(); err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	input := ""
	u.restoreToInput = &input
	u.message = ""
}

func (u *browserUI) destinationKey(key string) {
	switch key {
	case "cancel":
		u.restoreToInput = nil
		u.message = "Restore cancelled"
	case "enter":
		destination := *u.restoreToInput
		u.restoreToInput = nil
		if destination == "" {
			u.message = "Restore cancelled"
			return
		}
		u.restoreSelectionTo(destination, false)
	case "backspace":
		text := []rune(*u.restoreToInput)
		if len(text) > 0 {
			*u.restoreToInput = string(text[:len(text)-1])
		}
	case "clear":
		*u.restoreToInput = ""
	default:
		*u.restoreToInput += key
	}
}
