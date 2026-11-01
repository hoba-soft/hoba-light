package hobalight

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	hashpkg "hash"
	"io"
	"strings"
	"unicode/utf8"
)

// Neither a long line nor a large file may grow the content comparison buffers.
const diffChunk = 4096
const diffLookahead = 32

type checkedBlob struct {
	io.ReadCloser
	archive  *storageArchive
	sum      hashpkg.Hash
	expected string
	ctx      context.Context
}

func (b *checkedBlob) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.ReadCloser.Read(p)
	b.sum.Write(p[:n])
	if err == io.EOF && hex.EncodeToString(b.sum.Sum(nil)) != b.expected {
		err = errors.New("blob checksum mismatch")
	}
	return n, err
}
func (b *checkedBlob) Close() error { return errors.Join(b.ReadCloser.Close(), b.archive.Close()) }
func (s *Store) diffBlob(ctx context.Context, f File) (io.ReadCloser, error) {
	if f.Blob == "" && f.Error == "" {
		return io.NopCloser(strings.NewReader("")), nil
	}
	if f.Error != "" {
		return nil, fmt.Errorf("file was not backed up: %s", f.Error)
	}
	if !validID(f.Blob, 'B') {
		return nil, errors.New("invalid blob reference")
	}
	z, err := s.openArchive(objectKey(f.Blob))
	if err != nil {
		return nil, err
	}
	if len(z.File) != 1 {
		z.Close()
		return nil, errors.New("blob must contain exactly one file")
	}
	r, err := z.File[0].Open()
	if err != nil {
		z.Close()
		return nil, err
	}
	return &checkedBlob{ReadCloser: r, archive: z, sum: sha256.New(), expected: f.Blob[:64], ctx: ctx}, nil
}

// A record is at most 4 KiB. Very long lines are compared as explicitly labelled
// fragments, including their original bytes; no content is silently discarded.
type diffRecord struct {
	data     string
	fragment bool
}

func nextRecord(r *bufio.Reader) (diffRecord, error) {
	b, err := r.ReadSlice('\n')
	if err == bufio.ErrBufferFull {
		return diffRecord{string(b), true}, nil
	}
	if len(b) > 0 && err == io.EOF {
		err = nil
	}
	return diffRecord{data: string(b)}, err
}
func fillRecords(r *bufio.Reader, q *[]diffRecord, done *bool) error {
	for !*done && len(*q) < diffLookahead {
		v, err := nextRecord(r)
		if err == io.EOF {
			*done = true
			break
		}
		if err != nil {
			return err
		}
		*q = append(*q, v)
	}
	return nil
}
func textRecord(w io.Writer, mark string, r diffRecord) error {
	text := r.data
	if strings.HasSuffix(text, "\n") {
		text = strings.TrimSuffix(text, "\n")
	}
	if _, err := fmt.Fprintf(w, "%s%s\n", mark, text); err != nil {
		return err
	}
	if r.fragment {
		_, err := fmt.Fprintln(w, "\\ Line continues (4096-byte fragment)")
		return err
	}
	if !strings.HasSuffix(r.data, "\n") {
		_, err := fmt.Fprintln(w, "\\ No newline at end of file")
		return err
	}
	return nil
}

// Bounded lookahead aligns nearby insertions/deletions. It is intentionally not
// a globally minimal diff: finding that alignment can require unbounded history.
func streamTextDiff(ctx context.Context, a, b *bufio.Reader, w io.Writer) error {
	var x, y []diffRecord
	var xd, yd bool
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fillRecords(a, &x, &xd); err != nil {
			return err
		}
		if err := fillRecords(b, &y, &yd); err != nil {
			return err
		}
		if len(x) == 0 && len(y) == 0 {
			return nil
		}
		if len(x) > 0 && len(y) > 0 && x[0] == y[0] {
			if err := textRecord(w, " ", x[0]); err != nil {
				return err
			}
			x = x[1:]
			y = y[1:]
			continue
		}
		ai, bi := -1, -1
		for i := range x {
			for j := range y {
				if x[i] == y[j] && (ai < 0 || i+j < ai+bi) {
					ai, bi = i, j
				}
			}
		}
		if ai < 0 {
			ai, bi = min(1, len(x)), min(1, len(y))
		}
		for _, r := range x[:ai] {
			if err := textRecord(w, "-", r); err != nil {
				return err
			}
		}
		for _, r := range y[:bi] {
			if err := textRecord(w, "+", r); err != nil {
				return err
			}
		}
		x = x[ai:]
		y = y[bi:]
	}
}
func hexRecord(w io.Writer, mark string, offset int64, b []byte) error {
	var ascii strings.Builder
	for _, v := range b {
		if v >= 32 && v < 127 {
			ascii.WriteByte(v)
		} else {
			ascii.WriteByte('.')
		}
	}
	_, err := fmt.Fprintf(w, "%s %08x  %-47s  |%s|\n", mark, offset, fmt.Sprintf("% x", b), ascii.String())
	return err
}
func streamHexDiff(ctx context.Context, a, b io.Reader, w io.Writer) error {
	var x, y [16]byte
	for offset := int64(0); ; offset += 16 {
		if err := ctx.Err(); err != nil {
			return err
		}
		nx, ex := io.ReadFull(a, x[:])
		ny, ey := io.ReadFull(b, y[:])
		for _, err := range []error{ex, ey} {
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return err
			}
		}
		if nx == 0 && ny == 0 {
			return nil
		}
		if bytes.Equal(x[:nx], y[:ny]) {
			if err := hexRecord(w, " ", offset, x[:nx]); err != nil {
				return err
			}
		} else {
			if nx > 0 {
				if err := hexRecord(w, "-", offset, x[:nx]); err != nil {
					return err
				}
			}
			if ny > 0 {
				if err := hexRecord(w, "+", offset, y[:ny]); err != nil {
					return err
				}
			}
		}
	}
}
func looksText(r *bufio.Reader) bool {
	sample, _ := r.Peek(diffChunk)
	// Only an incomplete code point at the sample boundary is acceptable.
	for tail := sample; len(tail) > 0; {
		r, n := utf8.DecodeRune(tail)
		if r == utf8.RuneError && n == 1 {
			if !utf8.FullRune(tail) && len(sample) == diffChunk {
				break
			}
			return false
		}
		tail = tail[n:]
	}
	for _, b := range sample {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return true
}
func (s *Store) diffFileContent(ctx context.Context, a, b File, mode string, w io.Writer) error {
	ar, err := s.diffBlob(ctx, a)
	if err != nil {
		return err
	}
	defer ar.Close()
	br, err := s.diffBlob(ctx, b)
	if err != nil {
		return err
	}
	defer br.Close()
	x, y := bufio.NewReaderSize(ar, diffChunk), bufio.NewReaderSize(br, diffChunk)
	if mode == "auto" {
		if looksText(x) && looksText(y) {
			mode = "text"
		} else {
			mode = "hex"
		}
	}
	if _, err = fmt.Fprintf(w, "Content (%s):\n", mode); err != nil {
		return err
	}
	if mode == "hex" {
		return streamHexDiff(ctx, x, y, w)
	}
	return streamTextDiff(ctx, x, y, w)
}
