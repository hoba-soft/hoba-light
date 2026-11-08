package hobalight

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func identity(name string, info os.FileInfo) Identity {
	if info == nil {
		return Identity{Path: portable(name)}
	}
	return Identity{Path: portable(name), Size: info.Size(), Created: creationTime(name, info), Modified: timestamp(info.ModTime())}
}

// Failed companions retain known metadata and include the error in their identity.
func (s *Store) failedFile(name string, info os.FileInfo, cause error) (string, error) {
	ident := identity(name, info)
	ident.Error = cause.Error()
	key, err := encode(ident)
	if err != nil {
		return "", fmt.Errorf("save failure metadata: %v: %w", cause, err)
	}
	data, err := encode(File{Identity: ident})
	if err != nil {
		return "", fmt.Errorf("save failure metadata: %v: %w", cause, err)
	}
	id := hash(key) + "F"
	if err := s.put(id, data); err != nil {
		return "", fmt.Errorf("save failure metadata: %v: %w", cause, err)
	}
	return id, cause
}

// Successful companions are found directly from path and metadata, without sessions.
func (s *Store) backupFile(name string, info os.FileInfo, force bool) (string, error) {
	return s.backupFileTracked(name, info, force, &backupFileWrites{})
}
func (s *Store) backupFileTracked(name string, info os.FileInfo, force bool, writes *backupFileWrites) (id string, failure error) {
	defer func() {
		if failure != nil {
			cause := failure
			id, failure = s.failedFile(name, info, cause)
			if !isSourceFailure(cause) && id != "" {
				failure = cause
			}
		}
	}()
	ident := identity(name, info)
	identityBytes, err := encode(ident)
	if err != nil {
		return "", err
	}
	id = hash(identityBytes) + "F"
	s.logger.Printf("scan path=%q identity=%s size=%d modified=%s", ident.Path, id, ident.Size, ident.Modified)
	var cached File
	if err := s.read(id, 'F', &cached); err == nil {
		if !force {
			if info, err := s.backend.Inspect(objectKey(cached.Blob)); err == nil && info.Mode().IsRegular() {
				s.logger.Printf("reuse path=%q file=%s blob=%s reason=unchanged_metadata", ident.Path, id, cached.Blob)
				writes.reused = true
				return id, nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		s.logger.Printf("reread path=%q file=%s reason=damaged_metadata error=%v", ident.Path, id, err)
	}
	input, err := os.Open(name)
	if err != nil {
		return "", sourceFailure(err)
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil {
		return "", sourceFailure(err)
	}
	if !sameSourceFile(info, opened) {
		return "", sourceFailure(errors.New("source file changed after scanning; retry backup"))
	}
	compressed, err := newStorageStage(s.backend, ".blob-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(compressed.Name())
	defer compressed.Close()
	zw := zip.NewWriter(compressed)
	entry, err := zw.Create("data")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	written, err := io.Copy(io.MultiWriter(entry, h), sourceReader{input})
	if err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			return "", fmt.Errorf("close blob after %v: %w", err, closeErr)
		}
		return "", err
	}
	after, err := input.Stat()
	if err != nil {
		if closeErr := zw.Close(); closeErr != nil {
			return "", closeErr
		}
		return "", sourceFailure(err)
	}
	if written != ident.Size || !sameSourceFile(opened, after) {
		if closeErr := zw.Close(); closeErr != nil {
			return "", closeErr
		}
		return "", sourceFailure(errors.New("source file changed while reading; retry backup"))
	}
	if err = zw.Close(); err != nil {
		return "", err
	}
	if err = compressed.Sync(); err != nil {
		return "", err
	}
	compressedInfo, err := compressed.Stat()
	if err != nil {
		return "", err
	}
	if err = compressed.Close(); err != nil {
		return "", err
	}
	blob := hex.EncodeToString(h.Sum(nil)) + "B"
	target := objectKey(blob)
	stored, statErr := s.backend.Inspect(target)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if statErr == nil && !stored.Mode().IsRegular() && !force {
		return "", fmt.Errorf("publish blob %s: existing object is not a regular file", blob)
	}
	// A forced backup replaces existing blobs, repairing any damaged copy.
	if force || statErr != nil {
		err = publishStorageStage(s.backend, target, compressed.Name(), false)
		if err != nil {
			// Without --force a concurrent writer may have published the same
			// blob first; a forced backup must actually replace it.
			if info, statErr := s.backend.Inspect(target); force || statErr != nil || !info.Mode().IsRegular() {
				return "", fmt.Errorf("publish blob %s: %w", blob, err)
			}
			writes.reused = true
		} else {
			writes.publications++
			writes.bytes += compressedInfo.Size()
			if errors.Is(statErr, os.ErrNotExist) {
				writes.addedID = blob
			}
		}
	} else {
		writes.reused = true
	}
	metadata, err := encode(File{Identity: ident, Blob: blob})
	if err != nil {
		return "", err
	}
	if err = s.put(id, metadata); err != nil {
		return "", err
	}
	s.logger.Printf("backup path=%q file=%s blob=%s forced=%t", ident.Path, id, blob, force)
	return id, nil
}

// Scanning and reading can be separated by other files and workers.
func sameSourceFile(before, after os.FileInfo) bool {
	return before != nil && after != nil && before.Mode().IsRegular() && after.Mode().IsRegular() &&
		os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func (s *Store) backup(dir string, force bool) (string, error) {
	return s.backupWithSummary(dir, force, nil)
}
func (s *Store) backupWithSummary(dir string, force bool, summary *backupSummary) (string, error) {
	return s.backupWithComment(dir, force, summary, "")
}

func (s *Store) backupWithComment(dir string, force bool, summary *backupSummary, comment string) (string, error) {
	startedAt := time.Now()
	if summary != nil {
		*summary = backupSummary{}
		defer func() { summary.elapsed = time.Since(startedAt) }()
	}
	release, err := s.beginBackup()
	if err != nil {
		return "", err
	}
	defer release()

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	linkInfo, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("backup source must not be a symlink")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", errors.New("backup source must be a directory or regular file")
	}
	root := abs
	if !info.IsDir() {
		root = filepath.Dir(abs)
	}
	inStorage, err := storageExclusion(s.backend.LocalRoot(), abs)
	if err != nil {
		return "", err
	}
	var previous map[string]File
	if summary != nil {
		summary.baseline, previous, summary.comparisonError = s.previousBackup(portable(abs))
	}
	session := Session{Comment: comment, Version: Version(), Platform: Platform(), Directory: portable(root), Started: timestamp(time.Now()), Files: []string{}}
	if !info.IsDir() {
		session.Source = portable(abs)
	}
	fail := func(name string, err error) {
		msg := fmt.Sprintf("%s: %v", portable(name), err)
		session.Errors = append(session.Errors, msg)
		s.logger.Printf("error %s", msg)
	}
	type sourceFile struct {
		name string
		info os.FileInfo
		err  error
	}
	var sources []sourceFile
	s.report("Backup", 0, -1, abs)
	err = filepath.WalkDir(abs, func(name string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if d != nil && d.IsDir() {
				fail(name, walkErr)
			} else {
				sources = append(sources, sourceFile{name: name, err: walkErr})
			}
			return nil
		}
		if name != abs || !info.IsDir() {
			rel, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			if excluded(s.excludes, portable(rel), d.IsDir()) {
				s.logger.Printf("skip path=%q reason=excluded", portable(name))
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if inStorage(name, d) {
			s.logger.Printf("skip path=%q reason=storage", portable(name))
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			s.logger.Printf("directory path=%q", portable(name))
			return nil
		}
		info, err := os.Stat(name)
		if err != nil {
			sources = append(sources, sourceFile{name: name, err: err})
			return nil
		}
		if !info.Mode().IsRegular() {
			s.logger.Printf("skip path=%q reason=not_regular_file", portable(name))
			return nil
		}
		sources = append(sources, sourceFile{name: name, info: info})
		s.report("Backup", len(sources), -1, name)
		return nil
	})
	if err != nil {
		return "", err
	}
	current := abs
	if len(sources) > 0 {
		current = sources[0].name
	}
	s.reportBackup(0, len(sources), current, len(session.Errors), 0)
	type result struct {
		writes backupFileWrites
		id     string
		err    error
	}
	results := make([]result, len(sources))
	var progressMu sync.Mutex
	completed := 0
	added := map[string]bool{}
	var addedBytes int64
	errorCount := len(session.Errors)
	parallelFiles(len(sources), s.workers, func(i int) {
		file := sources[i]
		progressMu.Lock()
		s.reportBackup(completed, len(sources), file.name, errorCount, addedBytes)
		progressMu.Unlock()
		if file.err != nil {
			results[i].id, results[i].err = s.failedFile(file.name, file.info, sourceFailure(file.err))
		} else {
			results[i].id, results[i].err = s.backupFileTracked(file.name, file.info, force, &results[i].writes)
		}
	}, func(i, done int) {
		progressMu.Lock()
		completed = done
		if results[i].err != nil {
			errorCount++
		}
		if w := results[i].writes; w.addedID != "" && !added[w.addedID] {
			added[w.addedID] = true
			addedBytes += w.bytes
		}
		s.reportBackup(done, len(sources), sources[i].name, errorCount, addedBytes)
		progressMu.Unlock()
	})
	if summary != nil {
		summary.otherErrors = len(session.Errors)
		summary.addedBytes = addedBytes
	}
	// Preserve traversal order regardless of worker completion order.
	for i, result := range results {
		if summary != nil {
			summary.writes += result.writes.publications
			summary.writtenBytes += result.writes.bytes
			if result.err != nil {
				summary.failed++
			} else {
				if result.writes.reused {
					summary.reused++
				}
				if sources[i].info != nil {
					summary.logicalBytes += sources[i].info.Size()
				}
				var current File
				readErr := s.read(result.id, 'F', &current)
				if readErr != nil && summary.comparisonError == nil {
					summary.comparisonError = readErr
				}
				if summary.comparisonError != nil {
					summary.unclassified++
				} else if old, exists := previous[current.Path]; !exists {
					summary.newFiles++
				} else if sameFileMetadata(old, current) {
					summary.unchanged++
				} else {
					summary.modified++
				}
			}
		}
		if result.err != nil {
			fail(sources[i].name, result.err)
		}
		if result.id != "" {
			session.Files = append(session.Files, result.id)
		}
	}
	if summary != nil && summary.comparisonError != nil {
		summary.unclassified += summary.newFiles + summary.modified + summary.unchanged
	}
	session.Status = sessionCompleted
	if len(session.Errors) > 0 {
		session.Status = sessionCompletedWithErrors
	}
	for _, result := range results {
		if result.err != nil && (!isSourceFailure(result.err) || result.id == "") {
			session.Status = sessionFailed
		}
	}
	data, err := encode(session)
	if err != nil {
		return "", err
	}
	id := hash(data) + "S"
	reference, err := s.referenceFromSession(id, session)
	if err != nil {
		return s.saveFailedBackup(session, err)
	}
	ref, err := encode(reference)
	if err != nil {
		return "", err
	}
	started, _ := time.Parse(time.RFC3339Nano, session.Started)
	name := sessionReferenceName(started, id)
	if err = s.put(id, data); err != nil {
		return s.saveFailedBackup(session, fmt.Errorf("save session: %w", err))
	}
	if err = writeStorageBytes(s.backend, "sessions/"+name, ref, false); err != nil {
		// A failed reference publication must not leave a successful root that
		// refresh could later rediscover as a completed backup.
		cause := fmt.Errorf("save session reference: %w", err)
		s.logger.Printf("session failed: %v", cause)
		if removeErr := s.backend.Remove(objectKey(id)); removeErr != nil {
			return "", errors.Join(cause, fmt.Errorf("retire unpublished session %s: %w", id, removeErr))
		}
		return s.saveFailedBackup(session, cause)
	}
	s.sessionListings = nil
	s.logger.Printf("session=%s directory=%q files=%d errors=%d", id, session.Directory, len(session.Files), len(session.Errors))
	if session.Status == sessionFailed {
		return id, fmt.Errorf("session failed: %d errors", len(session.Errors))
	}
	if session.Status == sessionCompletedWithErrors {
		return id, &partialBackupError{fmt.Errorf("session incomplete: %d file errors", len(session.Errors))}
	}
	return id, nil
}
