package hobalight

import (
	"fmt"
	"io"
	"time"
)

// All S objects are roots, even when their derived session references are absent.
// Finish graph validation before moving any unreachable objects.
func (s *Store) gc(output io.Writer) error { return s.gcOptions(output, false, false) }

func (s *Store) gcOptions(output io.Writer, dryRun, verbose bool) error {
	release, err := s.beginGC()
	if err != nil {
		return err
	}
	defer release()
	objects := make(map[string]bool)
	var order []string
	err = s.walkObjects(func(id string, err error) error {
		if err != nil {
			return err
		}
		objects[id] = true
		order = append(order, id)
		return nil
	})
	if err != nil {
		return fmt.Errorf("gc aborted before moving objects: %w", err)
	}
	reachable := make(map[string]bool)
	var queue []string
	for _, id := range order {
		if validID(id, 'S') {
			queue = append(queue, id)
			reachable[id] = true
		}
	}
	for head := 0; head < len(queue); head++ {
		id := queue[head]
		references, err := s.objectReferences(id)
		if err != nil {
			return fmt.Errorf("gc aborted before moving objects: object %s: %w", id, err)
		}
		for _, reference := range references {
			if !objects[reference] {
				return fmt.Errorf("gc aborted before moving objects: object %s references missing object %s", id, reference)
			}
			if !reachable[reference] {
				reachable[reference] = true
				queue = append(queue, reference)
			}
		}
	}
	moved := 0
	var movedBytes, retainedBytes int64
	for _, id := range order {
		info, err := s.backend.Stat(objectKey(id))
		if err != nil {
			return err
		}
		if reachable[id] {
			retainedBytes += info.Size()
			continue
		}
		movedBytes += info.Size()
		if dryRun {
			moved++
			if verbose {
				fmt.Fprintf(output, "Would trash %s  orphan\n", id)
			}
			continue
		}
		if err := s.trashObject(id, "orphan"); err != nil {
			return fmt.Errorf("gc stopped after moving %d objects: %w", moved, err)
		}
		moved++
		if verbose {
			fmt.Fprintf(output, "Trashed %s  orphan\n", id)
		}
	}
	action := "moved to trash"
	if dryRun {
		action = "would move to trash"
	}
	fmt.Fprintf(output, "Objects: %d retained (%s), %d %s (%s)\n", len(reachable), displaySize(retainedBytes), moved, action, displaySize(movedBytes))
	cleanup, err := s.expireTrashMode(time.Now(), dryRun, verbose, output)
	if err != nil {
		return fmt.Errorf("trash cleanup stopped after deleting %d objects: %w", cleanup.objects, err)
	}
	action = "deleted"
	if dryRun {
		action = "would be deleted"
	}
	fmt.Fprintf(output, "Trash: %d expired objects %s (%s)\n", cleanup.objects, action, displaySize(cleanup.bytes))
	return nil
}

// Object graph edges are defined here independently of collection and trashing.
func (s *Store) objectReferences(id string) ([]string, error) {
	if !validObjectID(id) {
		return nil, fmt.Errorf("invalid object reference: %q", id)
	}
	switch id[len(id)-1] {
	case 'S':
		var session Session
		if err := s.read(id, 'S', &session); err != nil {
			return nil, err
		}
		for _, reference := range session.Files {
			if !validID(reference, 'F') {
				return nil, fmt.Errorf("invalid file reference %q", reference)
			}
		}
		return session.Files, nil
	case 'F':
		var file File
		if err := s.read(id, 'F', &file); err != nil {
			return nil, err
		}
		if file.Error != "" {
			return nil, nil
		}
		return []string{file.Blob}, nil
	case 'B':
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported object type: %s", id)
	}
}
