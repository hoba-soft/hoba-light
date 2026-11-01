package hobalight

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const diffMetadataLimit = 1 << 20

// A bounded, external merge sort avoids loading all F objects or session paths
// into maps. At most two runs are open while merging; the run list is on disk.
type diffEntry struct {
	Path, ID string
	File     File
}
type diffIndex struct {
	dir           string
	session       Session
	target        diffTarget
	file          *File
	count, failed int
	size          int64
}

func (d *diffIndex) close() {
	if d.dir != "" {
		os.RemoveAll(d.dir)
	}
}
func (d *diffIndex) name(n int) string { return filepath.Join(d.dir, fmt.Sprintf("run-%d", n)) }
func (s *Store) diffReadFile(id string) (File, error) {
	var f File
	if !validID(id, 'F') {
		return f, errors.New("invalid file reference")
	}
	r, err := s.backend.Open(objectKey(id))
	if err != nil {
		return f, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, diffMetadataLimit+1))
	if err != nil {
		return f, err
	}
	if len(data) > diffMetadataLimit {
		return f, errors.New("file metadata exceeds diff limit (1 MiB)")
	}
	if err = yaml.Unmarshal(data, &f); err != nil {
		return f, err
	}
	identity, err := encode(f.Identity)
	if err != nil {
		return f, err
	}
	if hash(identity) != id[:64] && hash(data) != id[:64] {
		return f, errors.New("file metadata checksum mismatch")
	}
	if f.Error == "" && !validID(f.Blob, 'B') || f.Error != "" && f.Blob != "" {
		return f, errors.New("invalid blob reference")
	}
	return f, nil
}

// Sessions written by hobalight have a top-level block list of F IDs. Read that
// list a record at a time, retaining only the small header. Hash the exact bytes
// and reject malformed/unsupported encodings rather than silently omitting IDs.
func (s *Store) scanDiffSession(ctx context.Context, id string, visit func(string) error) (Session, error) {
	var session Session
	r, err := s.backend.Open(objectKey(id))
	if err != nil {
		return session, err
	}
	defer r.Close()
	sum := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(r, sum))
	scanner.Buffer(make([]byte, 4096), diffMetadataLimit)
	var header strings.Builder
	section := ""
	seenFiles := false
	for scanner.Scan() {
		if err = ctx.Err(); err != nil {
			return session, err
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "files:") {
			if seenFiles {
				return session, errors.New("duplicate session files field")
			}
			seenFiles = true
			section = "files"
			tail := strings.TrimSpace(strings.TrimPrefix(line, "files:"))
			if tail != "" && tail != "[]" {
				return session, errors.New("unsupported session files encoding")
			}
			continue
		}
		if strings.HasPrefix(line, "errors:") {
			section = "errors"
			continue
		}
		if section == "files" && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			item := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			if !validID(item, 'F') {
				return session, errors.New("invalid file reference in session")
			}
			if err = visit(item); err != nil {
				return session, err
			}
			continue
		}
		if section == "errors" && (line == "" || line[0] == ' ' || line[0] == '\t' || strings.HasPrefix(line, "- ")) {
			continue
		}
		if section == "files" && strings.TrimSpace(line) != "" && (line[0] == ' ' || line[0] == '\t') {
			return session, errors.New("unsupported session file entry")
		}
		section = ""
		if header.Len()+len(line) > diffMetadataLimit {
			return session, errors.New("session header exceeds diff limit (1 MiB)")
		}
		header.WriteString(line)
		header.WriteByte('\n')
	}
	if err = scanner.Err(); err != nil {
		return session, err
	}
	if hex.EncodeToString(sum.Sum(nil)) != id[:64] {
		return session, errors.New("session checksum mismatch")
	}
	if err = yaml.Unmarshal([]byte(header.String()), &session); err != nil {
		return session, err
	}
	return session, nil
}
func (s *Store) buildDiffIndex(ctx context.Context, t diffTarget) (result *diffIndex, err error) {
	if t.empty {
		return &diffIndex{target: diffTarget{id: "(absent)"}, file: &File{Identity: Identity{Path: "(absent)"}}}, nil
	}
	t.id, err = s.resolveKinds(t.id, "SF")
	if err != nil {
		return nil, err
	}
	t.path, err = userRelative(t.path)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "hobalight-diff-index-*")
	if err != nil {
		return nil, err
	}
	d := &diffIndex{dir: dir, target: t}
	defer func() {
		if err != nil {
			d.close()
		}
	}()
	if validID(t.id, 'F') {
		if t.path != "" {
			return nil, errors.New("a file hash does not accept a session path")
		}
		f, e := s.diffReadFile(t.id)
		if e != nil {
			return nil, e
		}
		d.file = &f
		return d, nil
	}
	// First pass verifies the session and obtains its source root without retaining IDs.
	d.session, err = s.scanDiffSession(ctx, t.id, func(string) error { return nil })
	if err != nil {
		return nil, err
	}
	var chunk []diffEntry
	bytesUsed, runs := 0, 0
	flush := func() error {
		sort.Slice(chunk, func(i, j int) bool { return chunk[i].Path < chunk[j].Path })
		f, e := os.Create(d.name(runs))
		if e != nil {
			return e
		}
		writer := bufio.NewWriter(f)
		enc := json.NewEncoder(writer)
		for _, v := range chunk {
			if e = enc.Encode(v); e != nil {
				break
			}
		}
		e = errors.Join(e, writer.Flush(), f.Close())
		if e != nil {
			return e
		}
		runs++
		chunk = nil
		bytesUsed = 0
		return nil
	}
	_, err = s.scanDiffSession(ctx, t.id, func(id string) error {
		f, e := s.diffReadFile(id)
		if e != nil {
			return e
		}
		rel, e := relative(d.session.Directory, f.Path)
		if e != nil {
			return e
		}
		if t.path != "" {
			if rel == t.path {
				d.file = &f
				d.target = diffTarget{id: id}
				return nil
			}
			if !strings.HasPrefix(rel, t.path+"/") {
				return nil
			}
			rel = strings.TrimPrefix(rel, t.path+"/")
		}
		d.count++
		d.size += f.Size
		if f.Error != "" {
			d.failed++
		}
		chunk = append(chunk, diffEntry{rel, id, f})
		bytesUsed += len(rel) + len(f.Path) + len(f.Error) + len(f.Modified) + len(f.Blob) + 256
		if f.Created != nil {
			bytesUsed += len(*f.Created)
		}
		if bytesUsed >= diffMetadataLimit {
			return flush()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if d.file != nil && d.count > 0 {
		return nil, errors.New("session path is both a file and a directory")
	}
	if d.file != nil {
		return d, nil
	}
	if t.path != "" && d.count == 0 {
		return nil, errors.New("path not found in session")
	}
	if len(chunk) > 0 || runs == 0 {
		if err = flush(); err != nil {
			return nil, err
		}
	}
	// Pairwise rounds keep memory and descriptor use independent of run count.
	for runs > 1 {
		next := 0
		for i := 0; i < runs; i += 2 {
			temp := filepath.Join(dir, "merge")
			if i+1 == runs {
				err = os.Rename(d.name(i), temp)
			} else {
				err = mergeDiffRuns(ctx, d.name(i), d.name(i+1), temp)
				os.Remove(d.name(i))
				os.Remove(d.name(i + 1))
			}
			if err != nil {
				return nil, err
			}
			if err = os.Rename(temp, d.name(next)); err != nil {
				return nil, err
			}
			next++
		}
		runs = next
	}
	return d, nil
}
func mergeDiffRuns(ctx context.Context, a, b, out string) error {
	x, err := os.Open(a)
	if err != nil {
		return err
	}
	defer x.Close()
	y, err := os.Open(b)
	if err != nil {
		return err
	}
	defer y.Close()
	z, err := os.Create(out)
	if err != nil {
		return err
	}
	defer z.Close()
	dx, dy := json.NewDecoder(bufio.NewReader(x)), json.NewDecoder(bufio.NewReader(y))
	w := bufio.NewWriter(z)
	enc := json.NewEncoder(w)
	var ax, by diffEntry
	ex, ey := dx.Decode(&ax), dy.Decode(&by)
	for ex == nil || ey == nil {
		if err = ctx.Err(); err != nil {
			return err
		}
		if ex == nil && (ey != nil || ax.Path <= by.Path) {
			err = enc.Encode(ax)
			ex = dx.Decode(&ax)
		} else {
			err = enc.Encode(by)
			ey = dy.Decode(&by)
		}
		if err != nil {
			return err
		}
	}
	if ex != io.EOF {
		return ex
	}
	if ey != io.EOF {
		return ey
	}
	return w.Flush()
}
func walkDiffIndexes(ctx context.Context, a, b *diffIndex, visit func(*diffEntry, *diffEntry) error) error {
	x, err := os.Open(a.name(0))
	if err != nil {
		return err
	}
	defer x.Close()
	y, err := os.Open(b.name(0))
	if err != nil {
		return err
	}
	defer y.Close()
	dx, dy := json.NewDecoder(bufio.NewReader(x)), json.NewDecoder(bufio.NewReader(y))
	var ax, by diffEntry
	ex, ey := dx.Decode(&ax), dy.Decode(&by)
	for ex == nil || ey == nil {
		if err = ctx.Err(); err != nil {
			return err
		}
		switch {
		case ex == nil && ey == nil && ax.Path == by.Path:
			err = visit(&ax, &by)
			ex, ey = dx.Decode(&ax), dy.Decode(&by)
		case ex == nil && (ey != nil || ax.Path < by.Path):
			err = visit(&ax, nil)
			ex = dx.Decode(&ax)
		default:
			err = visit(nil, &by)
			ey = dy.Decode(&by)
		}
		if err != nil {
			return err
		}
	}
	if ex != io.EOF {
		return ex
	}
	if ey != io.EOF {
		return ey
	}
	return nil
}
