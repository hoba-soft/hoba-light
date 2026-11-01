package hobalight

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func printInfo(c Config, out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "hobalight %s %s\n\nPaths:\n  executable: %q\n  working_directory: %q\n  config: %q\n", Version(), Platform(), binary, cwd, c.path)
	fmt.Fprintf(out, "\nEffective settings:\n  storage: %q (%s)\n  directories: %q (%s)\n  workers: %d (%s)\n  exclude: %q (%s)\n  trash_retention_days: %d (%s)\n", c.Storage, c.sources["storage"], c.Directories, c.sources["directories"], c.Workers, c.sources["workers"], c.Exclude, c.sources["exclude"], c.TrashRetentionDays, c.sources["trash_retention_days"])
	fmt.Fprintln(out, "\nStorage paths:")
	for _, name := range []string{storeMarker, "objects", "sessions", "trash", "logs"} {
		fmt.Fprintf(out, "  %s: %q\n", name, filepath.Join(c.Storage, name))
	}
	fmt.Fprintf(out, "  restore_default: %q\n", filepath.Join(cwd, "hobalight_restore"))
	if _, err := os.Stat(c.Storage); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(out, "\nStorage: not created or unavailable")
		return nil
	} else if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(c.Storage, storeMarker)); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(out, "\nStorage: not initialized")
		return nil
	}
	s, err := readStoreMode(c.Storage, true)
	if err != nil {
		return err
	}
	stats, err := s.backendStats()
	if err != nil {
		return err
	}
	writers, maintenance := stats.writers, stats.maintenance
	fmt.Fprintf(out, "\nStorage status: initialized (format %d)\n  Maintenance lock: %t\n  Writer markers: %d\n", storeFormat, maintenance, writers)
	sessions, objects, trash := stats.sessions, stats.objects, stats.trash
	activeBytes, trashBytes, totalBytes := stats.activeBytes, stats.trashBytes, stats.totalBytes
	fmt.Fprintf(out, "\nStorage:\n  Sessions: %d\n  Active objects: %d, %s\n  Trash: %d files, %s\n  Total stored: %s\n", sessions, objects, displaySize(activeBytes), trash, displaySize(trashBytes), displaySize(totalBytes))
	return nil
}

type storageStats struct {
	writers, sessions, objects, trash   int
	maintenance                         bool
	activeBytes, trashBytes, totalBytes int64
}

func (s *Store) backendStats() (storageStats, error) {
	var result storageStats
	activity, err := s.backend.Activity()
	if err != nil {
		return result, err
	}
	result.writers, result.maintenance = activity.writers, activity.maintenance

	var sessions, objects, trash int
	var activeBytes, trashBytes, totalBytes int64
	err = s.backend.Walk(".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		totalBytes += info.Size()
		parts := strings.Split(name, "/")
		switch parts[0] {
		case "objects":
			if len(parts) == 3 && validObjectID(entry.Name()) && parts[1] == entry.Name()[:2] {
				objects++
				activeBytes += info.Size()
				if validID(entry.Name(), 'S') {
					sessions++
				}
			}
		case "trash":
			trash++
			trashBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	result.sessions, result.objects, result.trash = sessions, objects, trash
	result.activeBytes, result.trashBytes, result.totalBytes = activeBytes, trashBytes, totalBytes
	return result, nil
}
