package hobalight

import (
	"errors"
	"io"
)

func (s *Store) restoreCommand(args []string, to bool, output io.Writer) error {
	usage := "usage: restore <session-or-file-hash> [relative/path]"
	if to {
		usage = "usage: restore-to <session-hash> <relative/path-or-dot> <destination> | restore-to <file-hash> <destination>"
	}
	if (!to && (len(args) < 1 || len(args) > 2)) || (to && (len(args) < 2 || len(args) > 3)) {
		return errors.New(usage)
	}
	id, err := s.resolveKinds(args[0], "SF")
	if err != nil {
		return err
	}
	selected, destination := "", ""
	if to {
		if validID(id, 'F') {
			if len(args) != 2 {
				return errors.New(usage)
			}
			destination = args[1]
		} else {
			if len(args) != 3 {
				return errors.New(usage)
			}
			selected, destination = args[1], args[2]
		}
		if destination == "" {
			return errors.New("restore destination must not be empty")
		}
	} else if len(args) == 2 {
		if validID(id, 'F') {
			return errors.New("a file hash does not accept a session path")
		}
		selected = args[1]
	}
	_, err = s.restoreResolved(id, selected, destination, output)
	return err
}
