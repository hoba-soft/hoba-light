package hobalight

import "testing"

func TestExcludePatterns(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		dir, want     bool
	}{
		{"*.tmp", "nested/a.tmp", false, true},
		{"*.tmp", "nested/a.txt", false, false},
		{"cache/", "a/cache", true, true},
		{"cache/", "cache", false, false},
		{"/cache/", "a/cache", true, false},
		{"/cache/", "cache", true, true},
		{"build/*.log", "build/a.log", false, true},
		{"build/*.log", "nested/build/a.log", false, false},
		{"build/*.log", "build/nested/a.log", false, false},
		{"**/temp/*.tmp", "temp/a.tmp", false, true},
		{"**/temp/*.tmp", "a/b/temp/a.tmp", false, true},
		{"a/**/b", "a/b", false, true},
		{"a/**/b", "a/x/y/b", false, true},
		{"file?.txt", "file1.txt", false, true},
		{"file?.txt", "file12.txt", false, false},
		{"[x]", "[x]", false, true},
	} {
		t.Run(tc.pattern+":"+tc.name, func(t *testing.T) {
			rules, err := compileExcludes([]string{tc.pattern})
			must(t, err)
			if got := excluded(rules, tc.name, tc.dir); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	for _, pattern := range []string{"", "!keep", "/", "../file", "a//b", "a/**b", `a\b`} {
		if _, err := compileExcludes([]string{pattern}); err == nil {
			t.Errorf("accepted %q", pattern)
		}
	}
}
