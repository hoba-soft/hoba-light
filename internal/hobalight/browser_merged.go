package hobalight

import (
	"errors"
	"sort"
	"strings"
	"time"
)

type mergedNode struct {
	name, fullPath string
	children       map[string]*mergedNode
	file           bool
	size           int64
	latest         time.Time
}
type mergedEntry struct {
	node      *mergedNode
	directory bool
}
type mergedPosition struct {
	node   *mergedNode
	cursor int
}
type browserMerged struct {
	root, home, current *mergedNode
	parents             []mergedPosition
	cursor              int
	errors              int
	filter              string
}

func mergedPathParts(full string) ([]string, error) {
	normalized, err := historyPath(full)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(normalized, "//") {
		return append([]string{"//"}, strings.Split(strings.TrimPrefix(normalized, "//"), "/")...), nil
	}
	if strings.HasPrefix(normalized, "/") {
		return append([]string{"/"}, strings.Split(strings.TrimPrefix(normalized, "/"), "/")...), nil
	}
	return strings.Split(normalized, "/"), nil
}
func (m *browserMerged) entries() []mergedEntry {
	var entries []mergedEntry
	for _, node := range m.current.children {
		if len(node.children) > 0 {
			entries = append(entries, mergedEntry{node, true})
		}
		if node.file {
			entries = append(entries, mergedEntry{node, false})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].directory != entries[j].directory {
			return entries[i].directory
		}
		return entries[i].node.name < entries[j].node.name
	})
	if m.filter != "" {
		entries, _ = filterRows(entries, m.filter, mergedFilterText)
	}
	return entries
}

func mergedFilterText(e mergedEntry) string {
	if e.directory {
		return e.node.name + "/"
	}
	if len(e.node.children) > 0 {
		return e.node.name + " (file versions) versions"
	}
	return e.node.name + " versions"
}

func (s *Store) mergedBrowser() (*browserMerged, error) {
	refs, err := s.sessionReferences()
	if err != nil {
		return nil, err
	}
	refs, err = filterSessionDates(refs, s.sessionDate)
	if err != nil {
		return nil, err
	}
	root := &mergedNode{children: map[string]*mergedNode{}}
	m := &browserMerged{root: root, current: root}
	for _, ref := range refs {
		session, files, err := s.listingFiles(ref.Session)
		if err != nil {
			return nil, err
		}
		m.errors += len(session.Errors)
		started, err := time.Parse(time.RFC3339Nano, session.Started)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			// Validate against the original source root before merging foreign paths.
			full := strings.TrimRight(session.Directory, "/") + "/" + file.relative
			if rel, err := relative(session.Directory, full); err != nil || rel != file.relative {
				return nil, errors.New("invalid file path in merged tree")
			}
			normalized, err := historyPath(full)
			if err != nil {
				return nil, err
			}
			parts, err := mergedPathParts(normalized)
			if err != nil {
				return nil, err
			}
			node := root
			for _, part := range parts {
				if part == "" {
					continue
				}
				next := node.children[part]
				if next == nil {
					fullPath := part
					if node.fullPath != "" {
						if node.fullPath == "/" || node.fullPath == "//" {
							fullPath = node.fullPath + part
						} else {
							fullPath = node.fullPath + "/" + part
						}
					}
					next = &mergedNode{name: part, fullPath: fullPath, children: map[string]*mergedNode{}}
					node.children[part] = next
				}
				node = next
			}
			node.file = true
			node.fullPath = normalized
			if node.latest.IsZero() || started.After(node.latest) {
				node.latest = started
				node.size = file.metadata.Size
			}
		}
	}

	m.home = root
	for len(m.home.children) == 1 {
		var child *mergedNode
		for _, node := range m.home.children {
			child = node
		}
		if child.file || len(child.children) == 0 {
			break
		}
		m.home = child
	}
	m.current = m.home
	return m, nil
}
func (u *browserUI) toggleMergedMode() {
	if u.merged != nil {
		u.merged = nil
		u.applySessionDate(u.store.sessionDate)
		u.message = "Sessions mode"
		return
	}
	merged, err := u.store.mergedBrowser()
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	refs, err := u.store.sessionReferences()
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	refs, err = filterSessionDates(refs, u.store.sessionDate)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	u.refs = groupedReferences(refs)
	u.stats = u.store.referenceStats(refs)
	u.ref = nil
	u.source = ""
	u.allRefs = nil
	u.sources = true
	u.current = ""
	u.parents = nil
	u.history = nil
	u.searchHistory = nil
	u.searchParent = nil
	u.detail = nil
	u.search = nil
	u.tree = nil
	u.clearComparison()
	u.merged = merged
	u.message = "Merged files mode"
}
func (u *browserUI) openMergedFile(node *mergedNode) {
	versions, err := u.store.historyVersions(node.fullPath)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	u.history = &browserHistory{path: node.fullPath, versions: versions}
	u.message = ""
}
func (u *browserUI) mergedKey(key string) {
	m := u.merged
	entries := m.entries()
	switch key {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(max(0, len(entries)-1), m.cursor+1)
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = max(0, len(entries)-1)
	case "pageup":
		m.cursor = max(0, m.cursor-10)
	case "pagedown":
		m.cursor = min(max(0, len(entries)-1), m.cursor+10)
	case "left", "h", "b":
		if len(m.parents) > 0 {
			pos := m.parents[len(m.parents)-1]
			m.parents = m.parents[:len(m.parents)-1]
			m.current = pos.node
			m.cursor = pos.cursor
		}
	case "right", "l":
		if len(entries) == 0 {
			return
		}
		entry := entries[m.cursor]
		if entry.directory {
			m.parents = append(m.parents, mergedPosition{m.current, m.cursor})
			m.current = entry.node
			m.cursor = 0
		} else {
			u.openMergedFile(entry.node)
		}
	case "/":
		text := ""
		u.searchInput = &text
	case "f":
		merged, err := u.store.mergedBrowser()
		if err != nil {
			u.message = "Error: " + err.Error()
		} else {
			u.merged = merged
			u.message = "Merged tree refreshed"
		}
	case "r", "o", "t", "x", "delete", "X", "d", "c", "v":
		u.message = "Select a file version first"
	}
}
func (u *browserUI) mergedList(width, visible int) []string {
	entries := u.merged.entries()
	tableWidth := width
	heading := " " + browserTextColumns("Name", "Type", tableWidth, 8)
	if len(entries) == 0 {
		return []string{heading, "No files in selected sessions."}
	}
	visible = max(1, visible-1)
	start := max(0, u.merged.cursor-visible+1)
	lines := []string{heading}
	for i := start; i < min(len(entries), start+visible); i++ {
		entry := entries[i]
		label := entry.node.name
		if entry.directory {
			if !strings.HasSuffix(label, "/") {
				label += "/"
			}
		} else if len(entry.node.children) > 0 {
			label += " (file versions)"
		}
		suffix := "versions"
		if entry.directory {
			suffix = ""
		}
		lines = append(lines, selectionMarker(i == u.merged.cursor)+browserTextColumns(label, suffix, tableWidth, 8))
	}
	return lines
}
func (u *browserUI) mergedLocation() string {
	if u.merged.current.fullPath == "" {
		return "All source paths"
	}
	return u.merged.current.fullPath
}
func (u *browserUI) mergedDetailKey(key string) {
	switch key {
	case "left", "h", "b":
		u.detail = nil
	case "r", "o":
		u.restoreSelection(key == "o")
	case "t":
		u.beginRestoreTo()
	case "c":
		u.showContent()
	case "d":
		u.compare()
	}
}

func (m *browserMerged) locateFile(full string) (*mergedNode, error) {
	parts, err := mergedPathParts(full)
	if err != nil {
		return nil, err
	}
	m.current = m.root
	m.parents = nil
	m.cursor = 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		entries := m.entries()
		found := false
		for j, entry := range entries {
			if entry.node.name != part || entry.directory != (i < len(parts)-1) {
				continue
			}
			found = true
			m.cursor = j
			if i == len(parts)-1 {
				return entry.node, nil
			}
			if m.current == m.home || len(m.parents) > 0 {
				m.parents = append(m.parents, mergedPosition{m.current, j})
			}
			m.current = entry.node
			m.cursor = 0
			break
		}
		if !found {
			return nil, errors.New("file not found in merged tree")
		}
	}
	return nil, errors.New("file not found in merged tree")
}
