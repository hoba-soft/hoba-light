package hobalight

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	edges := make(map[string][]string)
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
		edges[id] = references
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

	expired, err := s.expiredSessions(order, time.Now())
	if err != nil {
		return fmt.Errorf("gc retention aborted before moving objects: %w", err)
	}
	deleting := map[string]bool{}
	for _, session := range expired {
		deleting[session.id] = true
	}
	if len(expired) > 0 {
		reachable = make(map[string]bool)
		queue = nil
		for _, id := range order {
			if validID(id, 'S') && !deleting[id] {
				reachable[id] = true
				queue = append(queue, id)
			}
		}
		for head := 0; head < len(queue); head++ {
			for _, child := range edges[queue[head]] {
				if !reachable[child] {
					reachable[child] = true
					queue = append(queue, child)
				}
			}
		}
	}
	// Collect sizes before any mutation, including the session roots to retire.
	sizes := map[string]int64{}
	for _, id := range order {
		info, err := s.backend.Stat(objectKey(id))
		if err != nil {
			return err
		}
		sizes[id] = info.Size()
	}
	for _, session := range expired {
		if dryRun {
			fmt.Fprintf(output, "Would delete session %s  %s  %q  retention\n", session.id, displayTimestamp(timestamp(session.started)), session.source)
			continue
		}
		if err := s.trashObject(session.id, "deleted"); err != nil {
			return fmt.Errorf("gc retention stopped: %w", err)
		}
		s.sessionListings = nil
		for _, name := range session.references {
			if err := s.backend.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("session %s retired; reference cleanup failed: %w", session.id, err)
			}
		}
		s.sessionListings = nil
		fmt.Fprintf(output, "Deleted session %s  retention\n", session.id)
	}
	moved := 0
	var movedBytes, retainedBytes int64
	for _, id := range order {
		if reachable[id] {
			retainedBytes += sizes[id]
			continue
		}
		movedBytes += sizes[id]
		if deleting[id] {
			moved++
			continue
		}
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
