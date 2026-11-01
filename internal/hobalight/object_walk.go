package hobalight

import (
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
)

func validObjectID(name string) bool {
	return validID(name, 'S') || validID(name, 'F') || validID(name, 'B')
}

// Both maintenance commands use the same inventory rules. Visitors choose
// whether to stop on an error (GC) or report it and continue (refresh).
func (s *Store) walkObjects(visit func(id string, err error) error) error {
	root := "objects"
	info, err := s.backend.Inspect(root)
	if err != nil {
		return visit(root, err)
	}
	if !info.IsDir() {
		return visit(root, fmt.Errorf("object storage is not a real directory: %s", root))
	}
	return s.backend.Walk(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return visit(name, walkErr)
		}
		id := entry.Name()
		if validObjectID(id) {
			if name != objectKey(id) || !entry.Type().IsRegular() {
				if err := visit(id, fmt.Errorf("invalid object location or type: %s", id)); err != nil {
					return err
				}
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			return visit(id, nil)
		}
		if path.Dir(name) == root && len(id) == 2 {
			if _, err := hex.DecodeString(id); err == nil && !entry.IsDir() {
				return visit(name, fmt.Errorf("object bucket is not a real directory: %s", id))
			}
		}
		return nil
	})
}
