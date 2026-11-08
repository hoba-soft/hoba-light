package hobalight

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// Search leaf names in every session, never source roots or session labels.
// Directories are inferred once per session from the saved relative paths.
func (s *Store) findBrowserNames(query string) ([]searchMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("search text must not be empty")
	}
	refs, err := s.sessionReferences()
	if err != nil {
		return nil, err
	}
	refs, err = filterSessionDates(refs, s.sessionDate)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	var matches []searchMatch
	for _, ref := range groupedReferences(refs) {
		object, files, err := s.listingFiles(ref.Session)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", ref.Session[:8], err)
		}
		ref.Comment = object.Comment
		directories := map[string]bool{}
		var found []searchMatch
		for _, file := range files {
			if strings.Contains(strings.ToLower(path.Base(file.relative)), query) {
				found = append(found, searchMatch{ref: ref, file: file})
			}
			for dir := path.Dir(file.relative); dir != "." && dir != "/"; dir = path.Dir(dir) {
				directories[dir] = true
			}
		}
		for dir := range directories {
			if strings.Contains(strings.ToLower(path.Base(dir)), query) {
				found = append(found, searchMatch{ref: ref, file: sessionFile{relative: dir}, directory: true})
			}
		}
		sort.Slice(found, func(i, j int) bool {
			if found[i].file.relative == found[j].file.relative {
				return found[i].directory && !found[j].directory
			}
			return found[i].file.relative < found[j].file.relative
		})
		matches = append(matches, found...)
	}
	return matches, nil
}

func (u *browserUI) rebuildSearchParents() {
	u.parents = nil
	parent := ""
	for _, component := range strings.Split(u.current, "/") {
		cursor := 0
		for i, entry := range u.tree[parent] {
			if entry.directory && entry.name == component {
				cursor = i
				break
			}
		}
		u.parents = append(u.parents, browserPosition{parent, cursor})
		parent = path.Join(parent, component)
	}
}
