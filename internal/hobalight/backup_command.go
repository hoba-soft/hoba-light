package hobalight

import (
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
)

func (s *Store) backupCommand(args, directories []string, output, status io.Writer) error {
	dirs := directories
	force := false
	var selected []string
	options := true
	for _, arg := range args {
		if options && arg == "--" {
			options = false
			continue
		}
		if options && arg == "--force" {
			force = true
		} else {
			if options && strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown backup option %q", arg)
			}
			selected = append(selected, arg)
		}
	}
	if len(selected) > 1 {
		return errors.New("backup accepts one directory")
	}
	if len(selected) == 1 {
		dirs = selected
	}
	if len(dirs) == 0 {
		return errors.New("no backup directories configured: specify a directory with backup <directory> or set directories in the config")
	}
	lf, err := s.backend.NewLog()
	if err != nil {
		return err
	}
	defer lf.Close()
	s.logger = log.New(lf, "", log.Ldate|log.Ltime|log.Lmicroseconds|log.LUTC)
	var failures []error
	for i, dir := range dirs {
		if i > 0 {
			fmt.Fprintln(status)
		}
		source, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		display := newBackupDisplay(status, source)
		previousProgress := s.progress
		s.progress = display.update
		var summary backupSummary
		id, err := s.backupWithSummary(dir, force, &summary)
		s.progress = previousProgress
		display.finish(id, err)
		if id != "" {
			if printErr := summary.print(output); printErr != nil {
				return printErr
			}
		}
		if err != nil {
			s.logger.Printf("backup failed directory=%q error=%v", dir, err)
			failures = append(failures, fmt.Errorf("%s: %w", dir, err))
		}
	}
	return errors.Join(failures...)
}
