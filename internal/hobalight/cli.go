package hobalight

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// Run executes the command-line interface.
func Run(args []string) error {
	// Go's flag parser accepts single-dash long names and implicit -h; reject
	// those spellings while leaving option values and command operands intact.
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			break
		}
		if (!strings.HasPrefix(arg, "--") && arg != "-c" && !strings.HasPrefix(arg, "-c=")) || arg == "--h" {
			return fmt.Errorf("unknown option %q: use long options beginning with --", arg)
		}
		if arg == "--config" || arg == "--exclude" || arg == "-c" || arg == "--comment" {
			i++
		}
	}

	global := flag.NewFlagSet("hobalight", flag.ContinueOnError)
	configName := os.Getenv("HOBALIGHT_CONFIG")
	showVersion := global.Bool("version", false, "Print binary version and platform")
	comment := global.String("comment", "", "Session comment for backup")
	global.StringVar(comment, "c", "", "Session comment for backup")
	config := global.String("config", configName, "YAML configuration file (overrides HOBALIGHT_CONFIG; relative to working directory)")
	var excludes []string
	excludesSet := false
	global.Func("exclude", "Exclude pattern (repeatable; replaces environment/config list; empty value clears the list)", func(value string) error {
		excludesSet = true
		if value == "" {
			excludes = nil
		} else {
			excludes = append(excludes, value)
		}
		return nil
	})
	global.Usage = func() {
		fmt.Fprintln(global.Output(), commandHelp)
		fmt.Fprintln(global.Output(), "  --help             Show help\n  --version          Show version and platform\n  --config file      Configuration file (overrides HOBALIGHT_CONFIG)\n  --exclude pattern  Repeatable exclusion; replaces environment/config list\n  -c, --comment text Session comment for backup")
	}
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	args = global.Args()
	commentSet := false
	global.Visit(func(f *flag.Flag) {
		if f.Name == "c" || f.Name == "comment" {
			commentSet = true
		}
	})
	withComment := func(args []string) []string {
		if !commentSet {
			return args
		}
		return append([]string{"--comment", *comment}, args...)
	}
	if *showVersion {
		fmt.Printf("hobalight %s %s\n", Version(), Platform())
		return nil
	}
	if len(args) == 0 {
		global.SetOutput(os.Stdout)
		global.Usage()
		return nil
	}
	explicit := configName != ""
	global.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicit = true
		}
	})
	if explicit && *config == "" {
		return errors.New("config path must not be empty")
	}
	c, err := loadConfig(*config)
	if err != nil {
		return err
	}
	if excludesSet {
		c.Exclude = excludes
		c.sources["exclude"] = "--exclude"
		if _, err := compileExcludes(c.Exclude); err != nil {
			return err
		}
	}
	if commentSet && args[0] != "backup" {
		if len(args) != 1 {
			return errors.New("--comment/-c is only available for backup")
		}
		for _, command := range []string{"info", "browse", "restore", "restore-to", "delete", "verify", "gc", "refresh", "history", "find", "list", "view", "diff"} {
			if args[0] == command {
				return errors.New("--comment/-c is only available for backup")
			}
		}
	}
	if args[0] == "info" {
		if len(args) != 1 {
			return errors.New("usage: info")
		}
		return printInfo(c, os.Stdout)
	}

	date := ""
	switch args[0] {
	case "list", "find", "history", "browse":
		var positional []string
		positional, date, err = sessionDateArgs(args[1:])
		if err != nil {
			return err
		}
		args = append([]string{args[0]}, positional...)
	}
	if args[0] == "browse" && len(args) != 1 {
		return errors.New("usage: browse")
	}
	var s *Store
	if args[0] == "history" || args[0] == "diff" || args[0] == "view" || args[0] == "verify" {
		s, err = readStoreMode(c.Storage, true)
	} else if len(args) > 0 && (args[0] == "refresh" || args[0] == "gc" || args[0] == "delete") {
		if args[0] == "gc" {
			for _, arg := range args[1:] {
				if arg != "--dry-run" && arg != "--verbose" {
					return errors.New("usage: gc [--dry-run] [--verbose]")
				}
			}
		}
		if args[0] != "delete" && args[0] != "gc" && len(args) != 1 {
			return fmt.Errorf("usage: %s", args[0])
		}
		s, err = readStoreMode(c.Storage, true)
	} else {
		s, err = openStore(c.Storage)
	}
	if err != nil {
		return err
	}
	s.sessionDate = date
	if args[0] == "browse" {
		return s.browse(os.Stdin, os.Stdout, openFile)
	}
	s.excludes, err = compileExcludes(c.Exclude)
	if err != nil {
		return err
	}
	s.workers = c.Workers
	s.trashRetentionDays = c.TrashRetentionDays
	s.retention = c.Retention
	s.progress = progressPrinter(os.Stderr)
	switch args[0] {
	case "backup":
		return s.backupCommand(withComment(args[1:]), c.Directories, os.Stdout, os.Stderr)
	case "restore", "restore-to":
		return s.restoreCommand(args[1:], args[0] == "restore-to", os.Stdout)
	case "delete":
		return s.deleteCommand(args[1:], os.Stdin, os.Stdout)
	case "verify":
		return s.verify(args[1:], os.Stdout)
	case "gc":
		dryRun, verbose := false, false
		for _, arg := range args[1:] {
			if arg == "--dry-run" {
				dryRun = true
			} else {
				verbose = true
			}
		}
		return s.gcOptions(os.Stdout, dryRun, verbose)
	case "refresh":
		return s.refresh(os.Stdout)
	case "history":
		return s.history(args[1:], os.Stdout)
	case "find":
		return s.find(args[1:], os.Stdout)
	case "list":
		return s.list(args[1:], os.Stdout)
	case "view":
		return s.viewCommand(args[1:], os.Stdin, os.Stdout)
	case "diff":
		return s.diffCommand(args[1:], os.Stdout)
	default:
		if len(args) == 1 {
			_, pathErr := os.Stat(args[0])
			if pathErr == nil || args[0] == "." || args[0] == ".." || strings.ContainsAny(args[0], `/\`) {
				return s.backupCommand(withComment([]string{"--", args[0]}), c.Directories, os.Stdout, os.Stderr)
			}
		}
		return fmt.Errorf("unknown command %q", args[0])
	}
}
