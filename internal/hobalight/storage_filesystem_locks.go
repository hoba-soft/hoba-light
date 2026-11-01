package hobalight

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Writers publish session roots or references. Mark the entire operation so
// exclusive maintenance cannot race with an unpublished session or reference.
func (s *filesystemStorage) BeginWrite(operation string) (func(), error) {
	marker, err := os.CreateTemp(s.root, ".backup-*")
	if err != nil {
		return nil, err
	}
	name := marker.Name()
	cleanup := func() { _ = os.Remove(name) }
	if err := marker.Close(); err != nil {
		cleanup()
		return nil, err
	}
	lock := filepath.Join(s.root, ".gc.lock")
	if _, err := os.Lstat(lock); !errors.Is(err, os.ErrNotExist) {
		cleanup()
		if err != nil {
			return nil, fmt.Errorf("%s refused: cannot check maintenance lock %q: %w", operation, lock, err)
		}
		return nil, maintenanceLockError(operation, lock)
	}
	return cleanup, nil
}

func (s *filesystemStorage) BeginMaintenance(operation string) (func(), error) {
	lock := filepath.Join(s.root, ".gc.lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, maintenanceLockError(operation, lock)
		}
		return nil, fmt.Errorf("cannot acquire %s lock: %w", operation, err)
	}
	cleanup := func() { _ = os.Remove(lock) }
	entries, err := os.ReadDir(s.root)
	if err != nil {
		cleanup()
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".backup-") {
			cleanup()
			return nil, fmt.Errorf("%s refused: a backup or refresh is active, or marker %q remains from an interrupted run; if no backup or refresh is running against this storage, remove this marker file and retry", operation, filepath.Join(s.root, entry.Name()))
		}
	}
	return cleanup, nil
}

func maintenanceLockError(operation, lock string) error {
	return fmt.Errorf("%s refused: storage maintenance is active or lock %q remains from an interrupted run; if no maintenance operation is running against this storage, remove this lock directory and retry", operation, lock)
}

func (s *filesystemStorage) Activity() (storageActivity, error) {
	var result storageActivity
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".backup-") {
			result.writers++
		}
		if entry.Name() == ".gc.lock" {
			result.maintenance = true
		}
	}
	return result, nil
}
