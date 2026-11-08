package hobalight

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

type fileDeletion struct {
	id       string
	file     File
	sessions []Reference
}

// All active S objects are roots, including those with missing reference files.
// Validate affected sessions before beginning any replacement.
func (s *Store) planFileDeletion(id string) (fileDeletion, error) {
	plan := fileDeletion{id: id}
	if err := s.read(id, 'F', &plan.file); err != nil {
		return plan, err
	}
	err := s.walkObjects(func(root string, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !validID(root, 'S') {
			return nil
		}
		var session Session
		if err := s.read(root, 'S', &session); err != nil {
			return err
		}
		for _, file := range session.Files {
			if file == id {
				if _, err := s.referenceFromSession(root, session); err != nil {
					return err
				}
				if _, err := s.referencesForDeletion(root); err != nil {
					return err
				}
				plan.sessions = append(plan.sessions, Reference{Session: root, Directory: session.Directory, Started: session.Started})
				break
			}
		}
		return nil
	})
	sort.Slice(plan.sessions, func(i, j int) bool { return plan.sessions[i].Session < plan.sessions[j].Session })
	return plan, err
}

func (s *Store) executeFileDeletion(plan fileDeletion, output io.Writer) error {
	for i, ref := range plan.sessions {
		selected, err := relative(ref.Directory, plan.file.Path)
		if err != nil {
			return err
		}
		if _, err := s.deletePathLocked(ref.Session, selected, plan.id, output); err != nil {
			return fmt.Errorf("file deletion stopped after %d of %d sessions: %w", i, len(plan.sessions), err)
		}
	}
	fmt.Fprintf(output, "Removed file %s from %d sessions. Run gc to collect unused objects.\n", plan.id, len(plan.sessions))
	return nil
}

func fileDeletionDescription(plan fileDeletion) []string {
	lines := []string{"Remove this file object from ALL sessions?", "File: " + plan.id, fmt.Sprintf("Path: %q", plan.file.Path), "Size: " + displaySize(plan.file.Size), fmt.Sprintf("Affected sessions: %d", len(plan.sessions))}
	for _, ref := range plan.sessions {
		lines = append(lines, fmt.Sprintf("%s  %s  %q", ref.Session, displayTimestamp(ref.Started), ref.Directory))
	}
	return append(lines, "Affected session hashes will change; old sessions move to trash.", "Other file versions remain. File/blob objects stay until gc.")
}

func confirmDeletion(input io.Reader, output io.Writer) (bool, error) {
	fmt.Fprint(output, "Confirm deletion? [y/N] ")
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && (err != io.EOF || answer == "") {
		return false, errors.New("confirmation input unavailable; use --force to delete without confirmation")
	}
	return strings.EqualFold(strings.TrimSpace(answer), "y"), nil
}

func (s *Store) deleteCommand(args []string, input io.Reader, output io.Writer) error {
	force := false
	var positional []string
	options := true
	for _, arg := range args {
		if options && arg == "--" {
			options = false
			continue
		}
		if options && arg == "--force" {
			force = true
			continue
		}
		if options && strings.HasPrefix(arg, "-") {
			return fmt.Errorf("unknown delete option %q", arg)
		}
		positional = append(positional, arg)
	}
	if len(positional) < 1 || len(positional) > 2 {
		return errors.New("usage: delete [--force] <session-or-file-hash> [relative/path]")
	}
	release, err := s.beginMaintenance("delete")
	if err != nil {
		return err
	}
	defer release()
	kinds := "SF"
	if len(positional) == 2 {
		if validID(positional[0], 'F') {
			return errors.New("a path can only be selected within a session")
		}
		kinds = "S"
	}
	id, err := s.resolveKinds(positional[0], kinds)
	if err != nil {
		return err
	}
	var plan fileDeletion
	if validID(id, 'F') {
		plan, err = s.planFileDeletion(id)
		if err != nil {
			return err
		}
		if len(plan.sessions) == 0 {
			fmt.Fprintln(output, "No sessions reference this file. Run gc to collect unused objects.")
			return nil
		}
		for _, line := range fileDeletionDescription(plan) {
			if force && line == "Remove this file object from ALL sessions?" {
				line = "Removing this file object from ALL sessions."
			}
			fmt.Fprintln(output, line)
		}
	} else {
		var session Session
		if err := s.read(id, 'S', &session); err != nil {
			return err
		}
		fmt.Fprintf(output, "Session: %s\nStarted: %s\nSource: %q\nFiles: %d\n", id, displayTimestamp(session.Started), session.Directory, len(session.Files))
		if len(positional) == 2 {
			selected, err := userRelative(positional[1])
			if err != nil {
				return err
			}
			_, files, err := s.sessionFiles(id)
			if err != nil {
				return err
			}
			count := filesCount(files, selected)
			if count == 0 {
				return errors.New("path not found in session")
			}
			fmt.Fprintf(output, "Remove path: %q (%s)\nSession hash will change; old session moves to trash.\n", positional[1], displayCount(count))
		} else if force {
			fmt.Fprintln(output, "Deleting this entire session and its references.")
		} else {
			fmt.Fprintln(output, "Delete this entire session and its references?")
		}
		fmt.Fprintln(output, "File/blob objects stay until gc; shared data remains reachable from other sessions.")
	}
	if !force {
		confirmed, err := confirmDeletion(input, output)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(output, "Deletion cancelled.")
			return nil
		}
	}
	if validID(id, 'F') {
		return s.executeFileDeletion(plan, output)
	}
	if len(positional) == 2 {
		_, err := s.deletePathLocked(id, positional[1], "", output)
		return err
	}
	return s.deleteSessionLocked(id, output)
}

// Revalidate the exact set shown in the browser under an exclusive lock.
func (s *Store) deleteFileConfirmed(shown fileDeletion, output io.Writer) error {
	release, err := s.beginMaintenance("delete")
	if err != nil {
		return err
	}
	defer release()
	current, err := s.planFileDeletion(shown.id)
	if err != nil {
		return err
	}
	if len(current.sessions) != len(shown.sessions) {
		return errors.New("affected sessions changed; confirm deletion again")
	}
	for i := range current.sessions {
		if current.sessions[i].Session != shown.sessions[i].Session {
			return errors.New("affected sessions changed; confirm deletion again")
		}
	}
	return s.executeFileDeletion(current, output)
}
