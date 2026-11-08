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
	comment := ""
	var selected []string
	options := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if options && (arg == "--comment" || arg == "-c") {
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires text", arg)
			}
			i++
			comment = args[i]
			continue
		}
		if options && strings.HasPrefix(arg, "-c=") {
			comment = strings.TrimPrefix(arg, "-c=")
			continue
		}
		if options && strings.HasPrefix(arg, "--comment=") {
			comment = strings.TrimPrefix(arg, "--comment=")
			continue
		}
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
		return errors.New("backup accepts one file or directory")
	}
	if len(selected) == 1 {
		dirs = selected
	}
	if len(dirs) == 0 {
		return errors.New("no backup sources configured: specify backup <path> or set directories in the config")
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
		id, err := s.backupWithComment(dir, force, &summary, comment)
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
