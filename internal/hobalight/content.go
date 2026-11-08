package hobalight

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// The content viewer shares the disk-backed pager with comparisons, but reads
// only one blob and emits its contents without diff markers.
func (s *Store) content(ctx context.Context, id, mode string, w io.Writer) error {
	f, err := s.diffReadFile(id)
	if err != nil {
		return err
	}
	header := []string{fmt.Sprintf("File: %.8s  %s", id, f.Path), "Size: " + displaySize(f.Size)}
	pinned, isViewer := w.(interface{ setContentHeader([]string) error })
	if isViewer {
		err = pinned.setContentHeader(header)
	} else {
		_, err = fmt.Fprintln(w, strings.Join(header, "\n"))
	}
	if err != nil {
		return err
	}
	input, err := s.diffBlob(ctx, f)
	if err != nil {
		return err
	}
	defer input.Close()
	r := bufio.NewReaderSize(input, diffChunk)
	if mode == "auto" {
		if looksText(r) {
			mode = "text"
		} else {
			mode = "hex"
		}
	}
	header = append(header, fmt.Sprintf("Content (%s):", mode))
	if isViewer {
		err = pinned.setContentHeader(header)
	} else {
		_, err = fmt.Fprintln(w, header[len(header)-1])
	}
	if err != nil {
		return err
	}
	records := 0
	for offset := int64(0); ; {
		if err = ctx.Err(); err != nil {
			return err
		}
		if mode == "hex" {
			var b [16]byte
			n, e := io.ReadFull(r, b[:])
			if e != nil && e != io.EOF && e != io.ErrUnexpectedEOF {
				return e
			}
			if n == 0 {
				break
			}
			if err = hexRecord(w, "", offset, b[:n]); err != nil {
				return err
			}
			offset += int64(n)
		} else {
			record, e := nextContentRecord(r)
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if isViewer {
				_, err = fmt.Fprintln(w, strings.TrimSuffix(record.data, "\n"))
			} else {
				err = writeContentRecord(w, record)
			}
			if err != nil {
				return err
			}
		}
		records++
	}
	if records == 0 && !isViewer {
		_, err = fmt.Fprintln(w, "(empty file)")
	}
	return err
}
func (u *browserUI) selectedContentID() string {
	switch {
	case u.history != nil:
		m, ok := u.history.selected()
		if ok {
			return m.file.id
		}
	case u.search != nil:
		if len(u.search.matches) > 0 {
			return u.search.matches[u.search.cursor].file.id
		}
	case u.detail != nil:
		return u.detail.id
	case u.ref != nil && len(u.entries) > 0:
		if f := u.entries[u.cursor].file; f != nil {
			return f.id
		}
	}
	return ""
}
func (u *browserUI) showContent() {
	id := u.selectedContentID()
	if id == "" {
		u.message = "Select a file to view its content"
		return
	}
	u.startContent(id, "auto")
}
func (u *browserUI) startContent(id, mode string) {
	spool, err := newDiffSpool(func(ctx context.Context, w io.Writer) error { return u.store.content(ctx, id, mode, w) })
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	u.closeDiff()
	u.streamDiff = &browserDiff{spool: spool, mode: mode, contentID: id}
	u.diffOffset, u.diffColumn = 0, 0
	u.message = ""
}
func (s *Store) viewCommand(args []string, input io.Reader, output io.Writer) error {
	mode := "auto"
	var paths []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--mode" {
			i++
			if i == len(args) {
				return errors.New("usage: view [--mode auto|text|hex] <file-hash> | <session> <file-path>")
			}
			mode = args[i]
		} else if strings.HasPrefix(args[i], "--mode=") {
			mode = strings.TrimPrefix(args[i], "--mode=")
		} else {
			paths = append(paths, args[i])
		}
	}
	if mode != "auto" && mode != "text" && mode != "hex" {
		return errors.New("view mode must be auto, text or hex")
	}
	if len(paths) < 1 || len(paths) > 2 {
		return errors.New("usage: view [--mode auto|text|hex] <file-hash> | <session> <file-path>")
	}
	target := diffTarget{id: paths[0]}
	if len(paths) == 2 {
		target.path = paths[1]
	}
	index, err := s.buildDiffIndex(context.Background(), target)
	if err != nil {
		return err
	}
	defer index.close()
	if index.file == nil {
		return errors.New("view requires a file, not a session or directory")
	}
	ui := browserUI{browser: browser{store: s}}
	ui.startContent(index.target.id, mode)
	if ui.streamDiff == nil {
		return errors.New(ui.message)
	}
	return ui.run(input, output, true)
}

// Leave room for formatting and keep a UTF-8 code point within one pager row.
func nextContentRecord(r *bufio.Reader) (diffRecord, error) {
	b, err := r.Peek(diffChunk - 16)
	if err != nil && err != io.EOF {
		return diffRecord{}, err
	}
	if len(b) == 0 {
		return diffRecord{}, io.EOF
	}
	n := len(b)
	fragment := err == nil
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		n = i + 1
		fragment = false
	} else if fragment {
		start := n - 1
		for start > 0 && n-start < 4 && !utf8.RuneStart(b[start]) {
			start--
		}
		if !utf8.FullRune(b[start:n]) {
			n = start
		}
	}
	record := diffRecord{data: string(b[:n]), fragment: fragment}
	_, err = r.Discard(n)
	return record, err
}
func writeContentRecord(w io.Writer, r diffRecord) error {
	if _, err := fmt.Fprintln(w, strings.TrimSuffix(r.data, "\n")); err != nil {
		return err
	}
	if r.fragment {
		_, err := fmt.Fprintln(w, "\\ Line continues (text fragment)")
		return err
	}
	if !strings.HasSuffix(r.data, "\n") {
		_, err := fmt.Fprintln(w, "\\ No newline at end of file")
		return err
	}
	return nil
}
func (u *browserUI) viewerError(standalone bool) error {
	if standalone && u.streamDiff != nil {
		return u.streamDiff.spool.err
	}
	return nil
}
