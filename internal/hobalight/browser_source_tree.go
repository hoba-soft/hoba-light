package hobalight

import "strings"

// The tree is presentation only. Its selectable indices are the existing source
// indices, so grouping rows never enter keyboard navigation or filter mappings.
type browserSourceRow struct {
	label  string
	source int
}

type browserSourceNode struct {
	name, separator string
	source          int
	children        []*browserSourceNode
}

func browserSourceParts(name string) ([]string, string) {
	separator := "/"
	windows := len(name) >= 2 && name[1] == ':' || strings.HasPrefix(name, `\\`)
	if windows && strings.Contains(name, `\`) {
		separator = `\`
	}
	path := name
	if windows {
		path = strings.ReplaceAll(path, `\`, "/")
	}
	var parts []string
	switch {
	case len(path) >= 2 && path[1] == ':':
		parts = append(parts, path[:2]+separator)
		path = strings.TrimLeft(path[2:], "/")
	case strings.HasPrefix(path, "//"):
		// Keep a UNC share together; it is a filesystem root in its own right.
		share := strings.SplitN(strings.TrimLeft(path, "/"), "/", 3)
		anchor := separator + separator + strings.Join(share[:min(2, len(share))], separator)
		parts = append(parts, anchor)
		path = ""
		if len(share) == 3 {
			path = share[2]
		}
	case strings.HasPrefix(path, "/"):
		parts = append(parts, "/")
		path = strings.TrimLeft(path, "/")
	}
	for _, part := range strings.Split(path, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, name)
	}
	return parts, separator
}

func browserSourceTree(names []string) []browserSourceRow {
	root := &browserSourceNode{source: -1}
	for index, name := range names {
		parts, separator := browserSourceParts(name)
		node := root
		for _, part := range parts {
			var next *browserSourceNode
			for _, child := range node.children {
				if child.name == part && child.separator == separator {
					next = child
					break
				}
			}
			if next == nil {
				next = &browserSourceNode{name: part, separator: separator, source: -1}
				node.children = append(node.children, next)
			}
			node = next
		}
		node.source = index
	}
	var rows []browserSourceRow
	var visit func(*browserSourceNode, string, string, bool)
	visit = func(node *browserSourceNode, prefix, branch string, last bool) {
		label := node.name
		for node.source < 0 && len(node.children) == 1 {
			child := node.children[0]
			if !strings.HasSuffix(label, node.separator) {
				label += node.separator
			}
			label += child.name
			node = child
		}
		if node.source < 0 && !strings.HasSuffix(label, node.separator) {
			label += node.separator
		}
		rows = append(rows, browserSourceRow{prefix + branch + label, node.source})
		if branch != "" {
			if last {
				prefix += "   "
			} else {
				prefix += "│  "
			}
		}
		for i, child := range node.children {
			last := i == len(node.children)-1
			branch := "├─ "
			if last {
				branch = "└─ "
			}
			visit(child, prefix, branch, last)
		}
	}
	for _, node := range root.children {
		visit(node, "", "", true)
	}
	return rows
}
