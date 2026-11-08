package hobalight

import (
	"path"
	"strings"

	"github.com/mattn/go-runewidth"
)

// Full navigation text is wrapped by the view, never shortened like table cells.
func (u *browserUI) browserBreadcrumb() string {
	if u.history != nil {
		label := "File history: " + u.history.path
		if u.merged != nil && u.detail != nil {
			label = "File version metadata › " + u.history.path
		}
		if u.history.sessions && len(u.history.versions) > 0 {
			label += " › Version: " + u.history.versions[u.history.cursor].id[:8]
		}
		return label
	}
	if u.search != nil {
		return "Find: " + u.search.query + " › All sessions"
	}
	if u.merged != nil {
		if u.merged.current != nil {
			return u.merged.current.fullPath
		}
		return ""
	}
	if u.ref == nil {
		if u.sources {
			return "/"
		}
		return u.source
	}
	parts := []string{u.ref.Directory, u.ref.Session[:8] + commentSuffix(u.ref.Comment)}
	if u.current != "" {
		parts = append(parts, u.current)
	}
	if u.detail != nil {
		parts = append(parts, path.Base(u.detail.relative))
	}
	return strings.Join(parts, " › ")
}

// Prefer word boundaries in comments while allowing long paths to wrap in full.
// Keep boundary spaces so joining the lines reproduces the original context.
func wrapBrowserBreadcrumb(text string, width int) []string {
	text = browserText(text)
	var lines []string
	for runewidth.StringWidth(text) > width {
		line := runewidth.Truncate(text, width, "")
		if boundary := strings.LastIndexByte(line, ' '); boundary >= 0 && runewidth.StringWidth(line[:boundary]) >= width/2 {
			line = line[:boundary+1]
		}
		if line == "" {
			break
		}
		lines = append(lines, line)
		text = text[len(line):]
	}
	return append(lines, text)
}
