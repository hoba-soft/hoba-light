package hobalight

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Preserve exact old bytes before replacement. If archival fails, leave the
// active object untouched. The backend reserves a unique key for each copy.
func (s *Store) archiveBroken(id string, data []byte) error {
	key, err := s.reserveTrash(id, "broken")
	if err != nil {
		return err
	}
	if err = writeStorageBytes(s.backend, key, data, false); err != nil {
		_ = s.backend.Remove(key)
		return fmt.Errorf("archive object %s: %w", id, err)
	}
	s.logger.Printf("archive object=%s path=%q", id, key)
	return nil
}

func (s *Store) trashObject(id, reason string) error {
	target, err := s.reserveTrash(id, reason)
	if err != nil {
		return err
	}
	if err := s.backend.Move(objectKey(id), target); err != nil {
		_ = s.backend.Remove(target)
		return fmt.Errorf("move %s to trash: %w", id, err)
	}
	return nil
}

func (s *Store) reserveTrash(id, reason string) (string, error) {
	return s.backend.Reserve("trash", id+"-trashed-"+time.Now().UTC().Format(trashTimeFormat)+"-*."+reason)
}

const trashTimeFormat = "20060102T150405Z"
const defaultTrashRetentionDays = 30

type trashCleanup struct {
	objects int
	bytes   int64
}

// Legacy names have no trustworthy removal date: start their retention period
// on first collection instead of using the original object's modification time.
func (s *Store) expireTrash(now time.Time) (int, error) {
	result, err := s.expireTrashMode(now, false, false, io.Discard)
	return result.objects, err
}

func (s *Store) expireTrashMode(now time.Time, dryRun, verbose bool, output io.Writer) (result trashCleanup, err error) {
	dir := "trash"
	entries, err := storageEntries(s.backend, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	days := s.trashRetentionDays
	if days < 1 {
		days = defaultTrashRetentionDays
	}
	cutoff := now.UTC().AddDate(0, 0, -days)
	for _, entry := range entries {
		name := entry.Name()
		if len(name) < 67 || !validObjectID(name[:65]) || name[65] != '-' || !entry.Type().IsRegular() {
			continue
		}
		tail := strings.TrimPrefix(name[66:], "trashed-")
		if !strings.HasPrefix(name[66:], "trashed-") {
			if dryRun {
				if verbose {
					fmt.Fprintf(output, "Would start trash retention: %s\n", browserText(name))
				}
				continue
			}
			target := name[:66] + "trashed-" + now.UTC().Format(trashTimeFormat) + "-" + name[66:]
			if err := s.backend.Move(path.Join(dir, name), path.Join(dir, target)); err != nil {
				return result, err
			}
			continue
		}
		if len(tail) < 17 || tail[16] != '-' {
			continue
		}
		date, err := time.Parse(trashTimeFormat, tail[:16])
		if err != nil || !date.Before(cutoff) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return result, err
		}
		if dryRun {
			result.bytes += info.Size()
			result.objects++
			if verbose {
				fmt.Fprintf(output, "Would delete trash/%s\n", browserText(name))
			}
			continue
		}
		if err := s.backend.Remove(path.Join(dir, name)); err != nil {
			return result, err
		}
		result.objects++
		result.bytes += info.Size()
		if verbose {
			fmt.Fprintf(output, "Deleted trash/%s\n", browserText(name))
		}
	}
	return result, nil
}
