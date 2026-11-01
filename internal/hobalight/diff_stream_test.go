package hobalight

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamingTextAlignment(t *testing.T) {
	for _, tc := range []struct{ a, b, want string }{
		{"one\ntwo\nthree\n", "one\nnew\ntwo\nthree\n", " one\n+new\n two\n three\n"},
		{"one\nold\nthree\n", "one\nthree\n", " one\n-old\n three\n"},
		{"old\n", "new\n", "-old\n+new\n"},
		{"last", "last\n", "-last\n\\ No newline at end of file\n+last\n"},
		{"", "added\n", "+added\n"},
		{"deleted\n", "", "-deleted\n"},
		{"a\r\n", "a\n", "-a\r\n+a\n"},
		{"Привет\n", "Пока\n", "-Привет\n+Пока\n"},
	} {
		var out strings.Builder
		must(t, streamTextDiff(context.Background(), bufio.NewReaderSize(strings.NewReader(tc.a), diffChunk), bufio.NewReaderSize(strings.NewReader(tc.b), diffChunk), &out))
		if out.String() != tc.want {
			t.Fatalf("%q -> %q: %q", tc.a, tc.b, out.String())
		}
	}
}
func TestStreamingLongLinesPreserveBytes(t *testing.T) {
	before := strings.Repeat("a", diffChunk*80) + "\n"
	after := strings.Repeat("b", diffChunk*80) + "\n"
	var out strings.Builder
	must(t, streamTextDiff(context.Background(), bufio.NewReaderSize(strings.NewReader(before), diffChunk), bufio.NewReaderSize(strings.NewReader(after), diffChunk), &out))
	if !strings.Contains(out.String(), "Line continues") {
		t.Fatal("missing fragment explanation")
	}
	var a, b strings.Builder
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "-") {
			a.WriteString(line[1:])
		}
		if strings.HasPrefix(line, "+") {
			b.WriteString(line[1:])
		}
	}
	if a.String() != strings.TrimSuffix(before, "\n") || b.String() != strings.TrimSuffix(after, "\n") {
		t.Fatal("long-line bytes lost")
	}
}

type countingDiffReader struct{ n int64 }

func (r *countingDiffReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	r.n += int64(len(p))
	return len(p), nil
}

type rejectingDiffWriter struct{}

func (rejectingDiffWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestStreamingBackpressure(t *testing.T) {
	a, b := &countingDiffReader{}, &countingDiffReader{}
	err := streamTextDiff(context.Background(), bufio.NewReaderSize(a, diffChunk), bufio.NewReaderSize(b, diffChunk), rejectingDiffWriter{})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	for _, r := range []*countingDiffReader{a, b} {
		if r.n > diffChunk*(diffLookahead+1) {
			t.Fatalf("unbounded read: %d", r.n)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = streamHexDiff(ctx, a, b, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestHexDiffAndDetection(t *testing.T) {
	var out strings.Builder
	must(t, streamHexDiff(context.Background(), strings.NewReader("abc\x00"), strings.NewReader("abX\x00plus"), &out))
	for _, want := range []string{"- 00000000", "61 62 63 00", "|abc.|", "+ 00000000", "|abX.plus|"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	for _, tc := range []struct {
		s    string
		text bool
	}{{"Привет\n", true}, {"hello\tworld", true}, {"\x00", false}, {"\xff", false}, {strings.Repeat("a", 4095) + "Ж", true}, {"a\xc0", false}} {
		if got := looksText(bufio.NewReaderSize(strings.NewReader(tc.s), diffChunk)); got != tc.text {
			t.Fatalf("detection %q: %v", tc.s, got)
		}
	}
}
func drainDiff(t *testing.T, d *diffSpool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !d.done {
		if time.Now().After(deadline) {
			t.Fatal("diff did not complete")
		}
		d.ensure(d.count + 256)
	}
}
func TestDiffSpoolBoundedAndCleanup(t *testing.T) {
	var written atomic.Int64
	d, err := newDiffSpool(func(ctx context.Context, w io.Writer) error {
		for i := 0; i < 10000; i++ {
			if _, e := fmt.Fprintf(w, "row %05d\n", i); e != nil {
				return e
			}
			written.Add(1)
		}
		return nil
	})
	must(t, err)
	dir := d.dir
	defer d.Close()
	for d.count < 20 && !d.done {
		d.ensure(20)
	}
	if d.count != 20 || written.Load() > 23 {
		t.Fatalf("read ahead: %d/%d", d.count, written.Load())
	}
	drainDiff(t, d)
	if d.err != nil {
		t.Fatal(d.err)
	}
	if d.count != 10000 || d.row(9999) != "row 09999" || d.row(0) != "row 00000" {
		t.Fatal("disk index incorrect")
	}
	stat, err := d.index.Stat()
	must(t, err)
	if stat.Size() != 80000 {
		t.Fatal(stat.Size())
	}
	d.Close()
	if _, err = os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary files remain")
	}
}
func TestDiffSpoolSlowProducerAndFailure(t *testing.T) {
	d, err := newDiffSpool(func(ctx context.Context, w io.Writer) error { <-ctx.Done(); return ctx.Err() })
	must(t, err)
	start := time.Now()
	d.ensure(10)
	if time.Since(start) > time.Second {
		t.Fatal("slow producer blocked UI")
	}
	d.Close()
	d, err = newDiffSpool(func(ctx context.Context, w io.Writer) error {
		fmt.Fprintln(w, "partial")
		return errors.New("test failure")
	})
	must(t, err)
	defer d.Close()
	drainDiff(t, d)
	if d.err == nil || d.err.Error() != "test failure" || d.row(0) != "partial" {
		t.Fatal(d.err)
	}
}
func TestDiffIndexesExternalMergeAndIntegrity(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	session := Session{Directory: "/source", Started: "2026-10-31T00:00:00Z"}
	// More than one bounded run; reverse input order exercises the disk merge.
	for i := 1200; i >= 0; i-- {
		f := File{Identity: Identity{Path: fmt.Sprintf("/source/dir/%04d-%s", i, strings.Repeat("x", 2000)), Size: int64(i)}, Blob: strings.Repeat("b", 64) + "B"}
		data, e := encode(f)
		must(t, e)
		id := hash(data) + "F"
		must(t, s.put(id, data))
		session.Files = append(session.Files, id)
	}
	data, err := encode(session)
	must(t, err)
	id := hash(data) + "S"
	must(t, s.put(id, data))
	d, err := s.buildDiffIndex(context.Background(), diffTarget{id: id, path: "dir"})
	must(t, err)
	defer d.close()
	if d.count != 1201 {
		t.Fatal(d.count)
	}
	count := 0
	last := ""
	must(t, walkDiffIndexes(context.Background(), d, d, func(a, b *diffEntry) error {
		if a == nil || b == nil || a.Path <= last {
			t.Fatal("bad merge order")
		}
		last = a.Path
		count++
		return nil
	}))
	if count != 1201 {
		t.Fatal(count)
	}
	must(t, os.WriteFile(s.object(id), append(data, ' '), 0600))
	if _, err = s.buildDiffIndex(context.Background(), diffTarget{id: id}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal(err)
	}
}
func TestDiffBlobChecksumAndCancellation(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	s := newStore(t, filepath.Join(root, "store"))
	writeFile(t, filepath.Join(source, "a"), []byte("first"))
	id, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(id)
	must(t, err)
	f := files[0].metadata
	r, err := s.diffBlob(context.Background(), f)
	must(t, err)
	got, err := io.ReadAll(r)
	must(t, err)
	r.Close()
	if string(got) != "first" {
		t.Fatal(string(got))
	}
	// A structurally valid ZIP under the wrong hash must fail at EOF.
	fake := strings.Repeat("c", 64) + "B"
	must(t, os.MkdirAll(filepath.Dir(s.object(fake)), 0700))
	data, err := os.ReadFile(s.object(f.Blob))
	must(t, err)
	must(t, os.WriteFile(s.object(fake), data, 0600))
	f.Blob = fake
	r, err = s.diffBlob(context.Background(), f)
	must(t, err)
	_, err = io.Copy(io.Discard, r)
	r.Close()
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = s.diffBlob(ctx, f)
	must(t, err)
	_, err = r.Read(make([]byte, 16))
	r.Close()
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestBrowserStreamingSearchAndBack(t *testing.T) {
	d, err := newDiffSpool(func(ctx context.Context, w io.Writer) error {
		for i := 0; i < 1000; i++ {
			if _, e := fmt.Fprintf(w, "row %d\n", i); e != nil {
				return e
			}
		}
		return nil
	})
	must(t, err)
	u := browserUI{streamDiff: &browserDiff{spool: d}}
	defer u.closeDiff()
	u.view(100, 24)
	u.key("/")
	for _, r := range "row 900" {
		u.key(string(r))
	}
	u.key("enter")
	deadline := time.Now().Add(5 * time.Second)
	for u.streamDiff.scanning {
		u.view(100, 24)
		if time.Now().After(deadline) {
			t.Fatal("search stalled")
		}
	}
	if u.diffOffset != 900 {
		t.Fatal(u.diffOffset, u.message)
	}
	u.key("home")
	u.view(100, 24)
	if u.diffOffset != 0 {
		t.Fatal("home")
	}
	u.key("end")
	for u.streamDiff.ending {
		u.view(100, 24)
	}
	if u.diffOffset != 1000-u.diffPageSize {
		t.Fatal(u.diffOffset)
	}
	u.key("/")
	u.key("cancel")
	if u.streamDiff.input != nil {
		t.Fatal("search not cancelled")
	}
	u.key("left")
	if u.streamDiff != nil {
		t.Fatal("viewer not closed")
	}
}
func TestStreamingWriterError(t *testing.T) {
	var a bytes.Buffer
	a.WriteString("one\n")
	if err := streamHexDiff(context.Background(), &a, strings.NewReader("two"), rejectingDiffWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}

func TestDiffMetadataLimitsAndEmptySession(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	session := Session{Directory: "/source", Started: "2026-10-31T00:00:00Z"}
	data, err := encode(session)
	must(t, err)
	id := hash(data) + "S"
	must(t, s.put(id, data))
	var out strings.Builder
	must(t, s.diff(id, id, &out))
	if !strings.Contains(out.String(), "No file changes") || !strings.Contains(out.String(), "0 files") {
		t.Fatal(out.String())
	}
	f := File{Identity: Identity{Path: "/source/file", Error: strings.Repeat("x", diffMetadataLimit)}, Blob: ""}
	data, err = encode(f)
	must(t, err)
	fid := hash(data) + "F"
	must(t, s.put(fid, data))
	if _, err = s.diffReadFile(fid); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatal(err)
	}
	bad := []byte("directory: /source\nfiles: [bad]\n")
	bid := hash(bad) + "S"
	must(t, s.put(bid, bad))
	if _, err = s.buildDiffIndex(context.Background(), diffTarget{id: bid}); err == nil || !strings.Contains(err.Error(), "encoding") {
		t.Fatal(err)
	}
}
func TestDiffSpoolDiskError(t *testing.T) {
	d, err := newDiffSpool(func(ctx context.Context, w io.Writer) error { _, e := fmt.Fprintln(w, "row"); return e })
	must(t, err)
	defer d.Close()
	must(t, d.data.Close())
	drainDiff(t, d)
	if d.err == nil {
		t.Fatal("disk write failure ignored")
	}
}
