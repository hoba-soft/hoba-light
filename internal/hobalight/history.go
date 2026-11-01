package hobalight

import (
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
)

type fileVersion struct {
	id       string
	file     File
	sessions []searchMatch
	earliest time.Time
}

// Interpret foreign Windows paths even when browsing a Windows backup on Unix.
func historyPath(name string) (string, error) {
	if strings.ContainsRune(name, 0) {
		return "", errors.New("invalid file path")
	}
	drive := len(name) >= 3 && ((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) && name[1] == ':' && (name[2] == '/' || name[2] == '\\')
	unc := strings.HasPrefix(name, `\\`) || strings.HasPrefix(name, "//")
	if drive || unc {
		name = strings.ReplaceAll(name, `\`, "/")
	}
	if !drive && !strings.HasPrefix(name, "/") {
		return "", errors.New("history requires an absolute source path or a session and relative file path")
	}
	if drive {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	if unc {
		return "//" + strings.TrimLeft(path.Clean(name), "/"), nil
	}
	return path.Clean(name), nil
}

func (s *Store) historyTarget(args []string) (string, error) {
	if len(args) == 1 {
		return historyPath(args[0])
	}
	if len(args) != 2 {
		return "", errors.New("usage: history <absolute-path> | history <session> <relative-file-path>")
	}
	id, err := s.resolve(args[0], 'S')
	if err != nil {
		return "", err
	}
	selected, err := userRelative(args[1])
	if err != nil {
		return "", err
	}
	session, files, err := s.listingFiles(id)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file.relative == selected {
			return historyPath(strings.TrimRight(session.Directory, "/") + "/" + file.relative)
		}
	}
	return "", errors.New("file not found in session")
}

// Sessions are roots even without derived references. Never read blob contents.
func (s *Store) historyVersions(fullPath string) ([]fileVersion, error) {
	target, err := historyPath(fullPath)
	if err != nil {
		return nil, err
	}
	byID := map[string]*fileVersion{}
	metadata := map[string]File{}
	err = s.walkObjects(func(id string, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !validID(id, 'S') {
			return nil
		}
		session, files, err := s.listingFiles(id)
		if err != nil {
			return err
		}
		started, err := time.Parse(time.RFC3339Nano, session.Started)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, listed := range files {
			fid := listed.id
			if seen[fid] {
				continue
			}
			seen[fid] = true
			normalized, err := historyPath(strings.TrimRight(session.Directory, "/") + "/" + listed.relative)
			if err != nil {
				return err
			}
			if normalized != target {
				continue
			}
			f, ok := metadata[fid]
			if !ok {
				if err := s.read(fid, 'F', &f); err != nil {
					return err
				}
				metadata[fid] = f
			}
			normalized, err = historyPath(f.Path)
			if err != nil {
				return err
			}
			if normalized != target {
				return errors.New("session listing path does not match file object")
			}
			rel, err := relative(session.Directory, f.Path)
			if err != nil {
				return err
			}
			version := byID[fid]
			if version == nil {
				version = &fileVersion{id: fid, file: f}
				byID[fid] = version
			}
			version.sessions = append(version.sessions, searchMatch{ref: Reference{Session: id, Directory: session.Directory, Started: session.Started}, file: sessionFile{id: fid, relative: rel, metadata: f}})
			if version.earliest.IsZero() || started.Before(version.earliest) {
				version.earliest = started
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	versions := make([]fileVersion, 0, len(byID))
	for _, version := range byID {
		sort.Slice(version.sessions, func(i, j int) bool {
			a, _ := time.Parse(time.RFC3339Nano, version.sessions[i].ref.Started)
			b, _ := time.Parse(time.RFC3339Nano, version.sessions[j].ref.Started)
			if a.Equal(b) {
				return version.sessions[i].ref.Session < version.sessions[j].ref.Session
			}
			return a.After(b)
		})
		versions = append(versions, *version)
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].earliest.Equal(versions[j].earliest) {
			return versions[i].id < versions[j].id
		}
		return versions[i].earliest.After(versions[j].earliest)
	})
	return versions, nil
}

func (s *Store) history(args []string, output io.Writer) error {
	target, err := s.historyTarget(args)
	if err != nil {
		return err
	}
	versions, err := s.historyVersions(target)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "History: %s\n", browserText(target))
	if len(versions) == 0 {
		fmt.Fprintln(output, "No file versions found.")
		return nil
	}
	for _, v := range versions {
		label := markedFileName(v.id[:8], v.file)
		fmt.Fprintf(output, "\n%s  First backed up: %s  %s  %s\n", label, v.earliest.UTC().Format(time.RFC3339Nano), historySessionCount(len(v.sessions)), displaySize(v.file.Size))
		for _, m := range v.sessions {
			fmt.Fprintf(output, "  Session %s  %s  %s\n", m.ref.Session[:8], m.ref.Started, browserText(m.ref.Directory))
		}
	}
	return nil
}

func historySessionCount(count int) string {
	if count == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", count)
}
