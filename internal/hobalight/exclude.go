package hobalight

import (
	"fmt"
	"regexp"
	"strings"
)

type excludeRule struct {
	pattern       *regexp.Regexp
	directoryOnly bool
}

// compileExcludes accepts slash-separated glob patterns relative to each source.
func compileExcludes(patterns []string) ([]excludeRule, error) {
	var rules []excludeRule
	for _, original := range patterns {
		if original == "" || strings.HasPrefix(original, "!") || strings.ContainsAny(original, "\\\x00") {
			return nil, fmt.Errorf("invalid exclude pattern %q", original)
		}
		anchored := strings.HasPrefix(original, "/")
		directoryOnly := strings.HasSuffix(original, "/")
		pattern := strings.TrimSuffix(strings.TrimPrefix(original, "/"), "/")
		parts := strings.Split(pattern, "/")
		prefix := "^"
		if !anchored && len(parts) == 1 {
			prefix = "(^|/)"
		}
		var expression strings.Builder
		expression.WriteString(prefix)
		for i, part := range parts {
			if part == "" || part == "." || part == ".." || (strings.Contains(part, "**") && part != "**") {
				return nil, fmt.Errorf("invalid exclude pattern %q", original)
			}
			if part == "**" {
				if i == len(parts)-1 {
					expression.WriteString(".*")
				} else {
					expression.WriteString("(?:[^/]+/)*")
				}
				continue
			}
			for _, char := range part {
				switch char {
				case '*':
					expression.WriteString("[^/]*")
				case '?':
					expression.WriteString("[^/]")
				default:
					expression.WriteString(regexp.QuoteMeta(string(char)))
				}
			}
			if i < len(parts)-1 {
				expression.WriteByte('/')
			}
		}
		expression.WriteByte('$')
		compiled, err := regexp.Compile(expression.String())
		if err != nil {
			return nil, fmt.Errorf("invalid exclude pattern %q: %w", original, err)
		}
		rules = append(rules, excludeRule{compiled, directoryOnly})
	}
	return rules, nil
}

func excluded(rules []excludeRule, relative string, directory bool) bool {
	for _, rule := range rules {
		if (!rule.directoryOnly || directory) && rule.pattern.MatchString(relative) {
			return true
		}
	}
	return false
}
