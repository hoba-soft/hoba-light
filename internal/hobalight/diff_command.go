package hobalight

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

type diffTarget struct {
	id, path string
	empty    bool
}

const diffUsage = "usage: diff [--mode auto|text|hex] <old-hash> <new-hash> [path] | <old-session> <old-path> <new-session> <new-path>"

func (s *Store) diffCommand(args []string, w io.Writer) error {
	mode := "auto"
	var positional []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--mode" {
			i++
			if i == len(args) {
				return errors.New(diffUsage)
			}
			mode = args[i]
		} else if strings.HasPrefix(args[i], "--mode=") {
			mode = strings.TrimPrefix(args[i], "--mode=")
		} else {
			positional = append(positional, args[i])
		}
	}
	if mode != "auto" && mode != "text" && mode != "hex" {
		return errors.New("diff mode must be auto, text or hex")
	}
	var a, b diffTarget
	switch len(positional) {
	case 2:
		a.id, b.id = positional[0], positional[1]
	case 3:
		a.id, b.id = positional[0], positional[1]
		a.path, b.path = positional[2], positional[2]
	case 4:
		a.id, a.path, b.id, b.path = positional[0], positional[1], positional[2], positional[3]
	default:
		return errors.New(diffUsage)
	}
	return s.diffTargets(context.Background(), a, b, mode, w)
}
func (s *Store) diffTargets(ctx context.Context, a, b diffTarget, mode string, w io.Writer) error {
	old, err := s.buildDiffIndex(ctx, a)
	if err != nil {
		return err
	}
	defer old.close()
	next, err := s.buildDiffIndex(ctx, b)
	if err != nil {
		return err
	}
	defer next.close()
	a, b = old.target, next.target
	if (old.file == nil) != (next.file == nil) {
		return errors.New("cannot compare a file with a directory or session")
	}
	if old.file != nil {
		x, y := *old.file, *next.file
		if _, err = fmt.Fprintf(w, "--- %.8s  %s\n+++ %.8s  %s\nSize: %s -> %s\n", a.id, x.Path, b.id, y.Path, displaySize(x.Size), displaySize(y.Size)); err != nil {
			return err
		}
		for _, detail := range modificationDetails(x, y) {
			if _, err = fmt.Fprintln(w, detail); err != nil {
				return err
			}
		}
		if x.Error != "" || y.Error != "" {
			return fmt.Errorf("content unavailable: before error %q; after error %q", x.Error, y.Error)
		}
		if x.Blob == y.Blob {
			_, err = fmt.Fprintln(w, "No content changes.")
			return err
		}
		return s.diffFileContent(ctx, x, y, mode, w)
	}
	if _, err = fmt.Fprintf(w, "--- %.8s  %s  %s\n+++ %.8s  %s  %s\n", a.id, old.session.Started, strings.TrimRight(old.session.Directory, "/")+pathSuffix(a.path), b.id, next.session.Started, strings.TrimRight(next.session.Directory, "/")+pathSuffix(b.path)); err != nil {
		return err
	}
	changes := 0
	// Three sequential merge passes preserve removed/added/modified ordering
	// without collecting changes in memory.
	for _, kind := range []string{"removed", "added", "modified"} {
		err = walkDiffIndexes(ctx, old, next, func(x, y *diffEntry) error {
			var f *diffEntry
			mark := ""
			if x == nil && kind == "added" {
				f, mark = y, "+"
			}
			if y == nil && kind == "removed" {
				f, mark = x, "-"
			}
			if x != nil && y != nil && kind == "modified" && !sameFileMetadata(x.File, y.File) {
				f, mark = y, "~"
			}
			if f == nil {
				return nil
			}
			changes++
			if _, e := fmt.Fprintf(w, "%s %s  %s\n", mark, f.Path, displaySize(f.File.Size)); e != nil {
				return e
			}
			if mark == "~" {
				for _, detail := range modificationDetails(x.File, y.File) {
					if _, e := fmt.Fprintln(w, "    "+detail); e != nil {
						return e
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if changes == 0 {
		if _, err = fmt.Fprintln(w, "No file changes."); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "Total: %s, %s -> %s, %s\n", displayFileCounts(old.count, old.failed), displaySize(old.size), displayFileCounts(next.count, next.failed), displaySize(next.size))
	return err
}
func pathSuffix(p string) string {
	if p == "" {
		return ""
	}
	return "/" + p
}
