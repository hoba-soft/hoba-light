package hobalight

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const restoreDirectory = "hobalight_restore"

type restoreResult struct {
	destination string
	count       int
	target      string
}
type restoreItem struct {
	id       string
	file     File
	relative string
	err      error
}

func (s *Store) restore(prefix, dest, selected string) error {
	return s.restoreTo(prefix, dest, selected, os.Stdout)
}
func (s *Store) restoreTo(prefix, dest, selected string, output io.Writer) error {
	id, err := s.resolve(prefix, 'S')
	if err != nil {
		return err
	}
	_, err = s.restoreResolved(id, selected, dest, output)
	return err
}

// Plan from original metadata, preserving failures so intact files remain recoverable.
func (s *Store) restoreItems(id, selected, selectedFileID string) ([]restoreItem, error) {
	if validID(id, 'F') {
		if selected != "" {
			return nil, errors.New("a file hash does not accept a session path")
		}
		var file File
		if err := s.read(id, 'F', &file); err != nil {
			return nil, err
		}
		name := path.Base(file.Path)
		if !validRelative(name) {
			return nil, errors.New("invalid original file name")
		}
		return []restoreItem{{id: id, file: file, relative: name}}, nil
	}
	var session Session
	if err := s.read(id, 'S', &session); err != nil {
		return nil, err
	}
	// A browser file selection already has an object ID. Validate it against
	// the session before reading just that file's authoritative metadata.
	if selectedFileID != "" {
		found := false
		for _, fileID := range session.Files {
			if fileID == selectedFileID {
				found = true
				break
			}
		}
		if !found || selected == "" {
			return nil, errors.New("selected file is not part of this session")
		}
		session.Files = []string{selectedFileID}
	}
	items := make([]restoreItem, 0, len(session.Files))
	for i, fileID := range session.Files {
		if i > 0 && i%64 == 0 {
			s.report("Restore", i, -1, selected)
		}
		item := restoreItem{id: fileID}
		if item.err = s.read(fileID, 'F', &item.file); item.err == nil {
			item.relative, item.err = relative(session.Directory, item.file.Path)
			if selectedFileID != "" && item.err == nil && item.relative != selected {
				return nil, errors.New("selected file path does not match its metadata")
			}
			if item.err == nil && selected != "" && item.relative != selected && !strings.HasPrefix(item.relative, selected+"/") {
				continue
			}
		}
		items = append(items, item)
	}
	if selected != "" && len(items) == 0 {
		return nil, errors.New("path not found in session")
	}
	return items, nil
}

func restoreDestination(id, destination string) (string, error) {
	if destination == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		destination = filepath.Join(cwd, restoreDirectory, id[:8])
		if err := safeParents(cwd, destination); err != nil {
			return "", err
		}
		return destination, nil
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return "", err
	}
	return canonical(absolute), nil
}

// The CLI and browser share selection, destination layout, conflict handling and progress.
func (s *Store) restoreResolved(id, selected, destination string, output io.Writer) (restoreResult, error) {
	return s.restoreResolvedSelection(id, selected, destination, "", output)
}

func (s *Store) restoreResolvedSelection(id, selected, destination, selectedFileID string, output io.Writer) (restoreResult, error) {
	var result restoreResult
	selected, err := userRelative(selected)
	if err != nil {
		return result, errors.New("restore path must be relative to the session directory")
	}
	s.report("Restore", 0, -1, selected)
	items, err := s.restoreItems(id, selected, selectedFileID)
	if err != nil {
		return result, err
	}
	result.destination, err = restoreDestination(id, destination)
	if err != nil {
		return result, err
	}
	fmt.Fprintf(output, "Restoring to: %s\n", browserText(result.destination))
	failed := 0
	var firstError error
	s.report("Restore", 0, len(items), result.destination)
	for i, item := range items {
		if item.err == nil {
			item.err = s.restoreFile(item.file, result.destination, item.relative)
		}
		if item.err != nil {
			failed++
			if firstError == nil {
				firstError = item.err
			}
			fmt.Fprintf(output, "Error %.8s F %s: %v\n", item.id, browserText(item.relative), item.err)
		} else {
			result.count++
			result.target = filepath.Join(result.destination, filepath.FromSlash(item.relative))
			fmt.Fprintf(output, "Restored %s F  %s B  %s\n", item.id[:8], item.file.Blob[:8], item.relative)
		}
		s.report("Restore", i+1, len(items), item.relative)
	}
	label := "Session"
	if validID(id, 'F') {
		label = "File"
	}
	fmt.Fprintf(output, "%s %s %c: restored %d files\n", label, id[:8], id[64], result.count)
	if failed > 0 {
		return result, fmt.Errorf("restore incomplete: %d files failed: %w", failed, firstError)
	}
	return result, nil
}

// restoreFile writes one file below dest without replacing anything that exists.
func (s *Store) restoreFile(f File, dest, rel string) error {
	if f.Error != "" {
		return fmt.Errorf("file was not backed up: %s", f.Error)
	}
	if f.Size < 0 {
		return errors.New("invalid file size in metadata")
	}
	native, err := nativeRelative(rel)
	if err != nil {
		return err
	}
	target := filepath.Join(dest, native)
	if !within(dest, target) {
		return errors.New("unsafe restore path")
	}
	if err = safeParents(dest, filepath.Dir(target)); err != nil {
		return err
	}
	if !validID(f.Blob, 'B') {
		return errors.New("invalid blob reference")
	}
	modified, err := time.Parse(time.RFC3339Nano, f.Modified)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("restored path is not a regular file")
		}
		input, err := os.Open(target)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if hex.EncodeToString(h.Sum(nil)) != f.Blob[:64] {
			return errors.New("restored file exists with different content; it has changed, move or remove it before restoring again")
		}
		if info.Size() != f.Size {
			return errors.New("file metadata size does not match restored content")
		}
		if info.ModTime().Equal(modified) {
			return nil
		}
		return os.Chtimes(target, modified, modified)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	zr, err := s.openArchive(objectKey(f.Blob))
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) != 1 {
		return errors.New("blob must contain exactly one file")
	}
	if zr.File[0].UncompressedSize64 != uint64(f.Size) {
		return errors.New("blob size does not match file metadata")
	}
	input, err := zr.File[0].Open()
	if err != nil {
		return err
	}
	defer input.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(out, h), input)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil && written != f.Size {
		err = errors.New("restored size does not match file metadata")
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != f.Blob[:64] {
		err = errors.New("blob checksum mismatch")
	}
	if err == nil {
		err = os.Chtimes(target, modified, modified)
	}
	if err != nil {
		os.Remove(target)
	}
	return err
}

func safeParents(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	current := root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.Mkdir(current, 0700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("restore parent is not a real directory")
		}
	}
	return nil
}
