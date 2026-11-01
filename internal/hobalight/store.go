package hobalight

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// storeMarker identifies an initialized store. Commands refuse to run without
// it, so an unmounted drive or a mistyped path is never treated as a new store.
const (
	storeMarker = "store.yaml"
	storeFormat = 1
)

type storeInfo struct {
	Format int `yaml:"format"`
}

func storeAt(root string) *Store {
	return storeWithBackend(&filesystemStorage{root: root})
}

func storeWithBackend(backend storageBackend) *Store {
	return &Store{trashRetentionDays: defaultTrashRetentionDays, backend: backend, logger: log.New(io.Discard, "", 0)}
}

// openStore creates a store in a missing or empty directory, or accepts an
// existing store. A non-empty executable directory also supports portable stores.
func openStore(root string) (*Store, error) {
	if _, err := os.Stat(filepath.Join(root, storeMarker)); err == nil || !errors.Is(err, os.ErrNotExist) {
		return readStore(root)
	}
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(entries) > 0 && !(len(entries) == 1 && entries[0].Name() == "hobalight.conf" && entries[0].Type().IsRegular()) {
		executable, execErr := os.Executable()
		rootInfo, rootErr := os.Stat(root)
		var binaryInfo os.FileInfo
		if execErr == nil {
			binaryInfo, execErr = os.Stat(filepath.Dir(executable))
		}
		if execErr != nil || rootErr != nil || !os.SameFile(rootInfo, binaryInfo) {
			return nil, fmt.Errorf("storage %s is not empty and is not a hobalight store", root)
		}
		// Existing names may belong to another application; do not adopt them.
		for _, entry := range entries {
			switch strings.ToLower(entry.Name()) {
			case "objects", "sessions", "logs", "trash", "broken":
				return nil, fmt.Errorf("cannot initialize storage: %s already exists", filepath.Join(root, entry.Name()))
			}
		}
	}
	for _, d := range []string{"objects", "sessions", "logs"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0700); err != nil {
			return nil, err
		}
	}
	marker, err := encode(storeInfo{Format: storeFormat})
	if err != nil {
		return nil, err
	}
	if err = publish(filepath.Join(root, storeMarker), marker); err != nil {
		return nil, err
	}
	return readStore(root)
}

func readStore(root string) (*Store, error) { return readStoreMode(root, false) }

func readStoreMode(root string, allowMissingReferences bool) (*Store, error) {
	b, err := os.ReadFile(filepath.Join(root, storeMarker))
	if errors.Is(err, os.ErrNotExist) {
		if _, statErr := os.Stat(root); errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("storage %s is not available: check the storage path", root)
		}
		return nil, fmt.Errorf("storage %s is not an initialized hobalight store: check the storage path", root)
	}
	if err != nil {
		return nil, fmt.Errorf("storage %s is not available: %w", root, err)
	}
	var info storeInfo
	if err = yaml.Unmarshal(b, &info); err != nil {
		return nil, fmt.Errorf("storage %s: %s: %w", root, storeMarker, err)
	}
	if info.Format != storeFormat {
		return nil, fmt.Errorf("storage %s uses unsupported format %d", root, info.Format)
	}
	for _, d := range []string{"objects", "sessions"} {
		if info, err := os.Stat(filepath.Join(root, d)); err != nil {
			if allowMissingReferences && d == "sessions" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("storage %s is damaged: %w", root, err)
		} else if !info.IsDir() {
			return nil, fmt.Errorf("storage %s is damaged: %s is not a directory", root, d)
		}
	}
	return storeAt(root), nil
}
