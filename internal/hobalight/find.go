package hobalight

import (
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

type searchMatch struct {
	ref  Reference
	file sessionFile
}

// Search uses display metadata; original file objects are read only for legacy
// references or subsequent operations such as restore and details.
func (s *Store) findFiles(query, session, directory string) ([]searchMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("search text must not be empty")
	}
	refs, err := s.sessionReferences()
	if err != nil {
		return nil, err
	}
	if session != "" {
		id, err := s.resolve(session, 'S')
		if err != nil {
			return nil, err
		}
		var object Session
		if err := s.read(id, 'S', &object); err != nil {
			return nil, err
		}
		refs = []Reference{{Session: id, Directory: object.Directory, Started: object.Started}}
	}
	refs = groupedReferences(refs)
	query = strings.ToLower(query)
	var matches []searchMatch
	prefix := ""
	if directory != "" {
		prefix = directory + "/"
	}
	foundDirectory := directory == ""
	for _, ref := range refs {
		_, files, err := s.listingFiles(ref.Session)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", ref.Session[:8], err)
		}
		sort.Slice(files, func(i, j int) bool { return files[i].relative < files[j].relative })
		for _, file := range files {
			if !strings.HasPrefix(file.relative, prefix) {
				continue
			}
			foundDirectory = true
			if strings.Contains(strings.ToLower(path.Base(file.relative)), query) {
				matches = append(matches, searchMatch{ref, file})
			}
		}
	}
	if !foundDirectory {
		return nil, errors.New("directory not found in session")
	}
	return matches, nil
}

func (s *Store) find(args []string, output io.Writer) error {
	if len(args) < 1 || len(args) > 3 {
		return errors.New("usage: find <name> | find <session> [relative/directory] <name>")
	}
	session, directory := "", ""
	if len(args) > 1 {
		session = args[0]
	}
	if len(args) > 2 {
		var err error
		directory, err = userRelative(args[1])
		if err != nil {
			return err
		}
	}
	matches, err := s.findFiles(args[len(args)-1], session, directory)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		fmt.Fprintln(output, "No matching files.")
		return nil
	}
	var names, sizes []string
	for _, m := range matches {
		names = append(names, browserText(markedFileName(m.file.relative, m.file.metadata)))
		sizes = append(sizes, displaySize(m.file.metadata.Size))
	}
	width := sizeColumnWidth(names, sizes) + 10
	column := sizeWidth(sizes)
	previousDirectory, previousSession := "", ""
	for i, m := range matches {
		if i == 0 || m.ref.Directory != previousDirectory {
			if i > 0 {
				fmt.Fprintln(output)
			}
			fmt.Fprintln(output, browserText(m.ref.Directory))
		}
		if m.ref.Session != previousSession {
			fmt.Fprintf(output, "  Session %s  %s\n", m.ref.Session[:8], sessionTime(m.ref))
		}
		fmt.Fprintln(output, "    "+hashSizeColumn(names[i], m.file.id[:8], sizes[i], width, column))
		previousDirectory, previousSession = m.ref.Directory, m.ref.Session
	}
	return nil
}
