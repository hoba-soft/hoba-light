package hobalight

import (
	"sort"
	"strings"
)

type browserEntry struct {
	count     int
	errors    int
	size      int64
	name      string
	directory bool
	file      *sessionFile
}
type browser struct {
	store *Store
	open  func(string) error
}

func browserEntries(files []sessionFile, current string) []browserEntry {
	byName := map[string]browserEntry{}
	prefix := ""
	if current != "" {
		prefix = current + "/"
	}
	for i := range files {
		f := &files[i]
		if !strings.HasPrefix(f.relative, prefix) {
			continue
		}
		tail := strings.TrimPrefix(f.relative, prefix)
		name, _, directory := strings.Cut(tail, "/")
		previous := byName[name]
		byName[name] = browserEntry{name: name, directory: directory, file: f, size: previous.size + f.metadata.Size, count: previous.count + 1, errors: previous.errors + fileErrors(f.metadata)}
	}
	return sortedBrowserEntries(byName)
}

func sortedBrowserEntries(byName map[string]browserEntry) []browserEntry {
	entries := make([]browserEntry, 0, len(byName))
	for _, e := range byName {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].directory != entries[j].directory {
			return entries[i].directory
		}
		return entries[i].name < entries[j].name
	})
	return entries
}

// Build every directory once, sharing file metadata with the loaded session.
func browserTree(files []sessionFile) map[string][]browserEntry {
	groups := map[string]map[string]browserEntry{"": {}}
	for i := range files {
		f := &files[i]
		parts := strings.Split(f.relative, "/")
		current := ""
		for j, name := range parts {
			if groups[current] == nil {
				groups[current] = map[string]browserEntry{}
			}
			entry := groups[current][name]
			entry.name, entry.directory = name, j < len(parts)-1
			entry.size += f.metadata.Size
			entry.count++
			entry.errors += fileErrors(f.metadata)
			if !entry.directory {
				entry.file = f
			}
			groups[current][name] = entry
			if current == "" {
				current = name
			} else {
				current += "/" + name
			}
		}
	}
	tree := make(map[string][]browserEntry, len(groups))
	for directory, group := range groups {
		tree[directory] = sortedBrowserEntries(group)
	}
	return tree
}
