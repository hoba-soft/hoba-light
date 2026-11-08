package hobalight

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type diffPacket struct {
	row    []byte
	header []string
	err    error
}
type diffRowWriter struct {
	ctx     context.Context
	rows    chan<- diffPacket
	pending []byte
}

func (w *diffRowWriter) flush() error {
	if len(w.pending) == 0 {
		return nil
	}
	select {
	case <-w.ctx.Done():
		return w.ctx.Err()
	case w.rows <- diffPacket{row: w.pending}:
		w.pending = nil
		return nil
	}
}

// Headers travel beside body records and never occupy a searchable row.
func (w *diffRowWriter) setContentHeader(header []string) error {
	if err := w.flush(); err != nil {
		return err
	}
	select {
	case <-w.ctx.Done():
		return w.ctx.Err()
	case w.rows <- diffPacket{header: append([]string(nil), header...)}:
		return nil
	}
}

func (w *diffRowWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		n := min(len(p), diffChunk-len(w.pending))
		if i := bytes.IndexByte(p[:n], '\n'); i >= 0 {
			n = i + 1
		}
		w.pending = append(w.pending, p[:n]...)
		p = p[n:]
		if len(w.pending) == diffChunk || w.pending[len(w.pending)-1] == '\n' {
			if err := w.flush(); err != nil {
				return total - len(p), err
			}
		}
	}
	return total, nil
}

// Both rows and their offsets live on disk. A bounded channel provides
// backpressure; a timed receive keeps slow I/O/computation off the UI thread.
type diffSpool struct {
	dir         string
	data, index *os.File
	rows        <-chan diffPacket
	cancel      context.CancelFunc
	finished    chan struct{}
	count       int
	header      []string
	offset      int64
	done        bool
	err         error
}

func newDiffSpool(produce func(context.Context, io.Writer) error) (*diffSpool, error) {
	dir, err := os.MkdirTemp("", "hobalight-diff-*")
	if err != nil {
		return nil, err
	}
	d := &diffSpool{dir: dir}
	d.data, err = os.Create(filepath.Join(dir, "rows"))
	if err != nil {
		d.Close()
		return nil, err
	}
	d.index, err = os.Create(filepath.Join(dir, "index"))
	if err != nil {
		d.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	rows := make(chan diffPacket, 2)
	d.rows = rows
	d.finished = make(chan struct{})
	go func() {
		defer close(d.finished)
		defer close(rows)
		w := &diffRowWriter{ctx: ctx, rows: rows}
		err := produce(ctx, w)
		if e := w.flush(); err == nil {
			err = e
		}
		if err != nil {
			select {
			case rows <- diffPacket{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return d, nil
}
func (d *diffSpool) Close() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.finished != nil {
		<-d.finished
	}
	if d.data != nil {
		d.data.Close()
	}
	if d.index != nil {
		d.index.Close()
	}
	if d.dir != "" {
		os.RemoveAll(d.dir)
	}
}
func (d *diffSpool) ensure(n int) {
	timer := time.NewTimer(20 * time.Millisecond)
	defer timer.Stop()
	for !d.done && d.count < n {
		var p diffPacket
		var ok bool
		select {
		case <-timer.C:
			return
		case p, ok = <-d.rows:
		}
		if !ok || p.err != nil {
			d.done = true
			d.err = p.err
			return
		}
		if p.header != nil {
			d.header = p.header
			continue
		}
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64(d.offset))
		if _, err := d.index.Write(offset[:]); err != nil {
			d.err = err
			d.done = true
			d.cancel()
			return
		}
		if _, err := d.data.Write(p.row); err != nil {
			d.err = err
			d.done = true
			d.cancel()
			return
		}
		d.offset += int64(len(p.row))
		d.count++
	}
}
func (d *diffSpool) row(n int) string {
	if n < 0 || n >= d.count {
		return ""
	}
	var offsets [16]byte
	size := 8
	if n+1 < d.count {
		size = 16
	}
	if _, err := d.index.ReadAt(offsets[:size], int64(n)*8); err != nil {
		d.err = err
		return ""
	}
	start, end := int64(binary.LittleEndian.Uint64(offsets[:8])), d.offset
	if size == 16 {
		end = int64(binary.LittleEndian.Uint64(offsets[8:]))
	}
	if end-start > diffChunk || end < start {
		d.err = errors.New("invalid diff row index")
		return ""
	}
	b := make([]byte, int(end-start))
	if _, err := d.data.ReadAt(b, start); err != nil {
		d.err = err
		return ""
	}
	return strings.TrimSuffix(string(b), "\n")
}
