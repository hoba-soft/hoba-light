package hobalight

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPortablePaths(t *testing.T) {
	for _, root := range []string{"C:/Users/person/source", "/home/person/source", "/"} {
		name := strings.TrimRight(root, "/") + "/nested/file"
		rel, err := relative(root, name)
		if err != nil || rel != "nested/file" {
			t.Fatalf("%s %s %v", root, rel, err)
		}
	}
	for _, name := range []string{"/source/../escape", "/other/file", "/source/a//b", "/source/./a", "/source/", "/source/a/.."} {
		if _, err := relative("/source", name); err == nil {
			t.Fatalf("accepted unsafe path: %s", name)
		}
	}
	// Names that are legal on Linux and macOS must remain listable everywhere.
	for _, name := range []string{"meeting 10:30.txt", "a\\b", "C:"} {
		rel, err := relative("/source", "/source/"+name)
		if err != nil || rel != name {
			t.Fatalf("rejected portable name %q: %v", name, err)
		}
		_, err = nativeRelative(rel)
		if (runtime.GOOS == "windows") != (err != nil) {
			t.Fatalf("native %q: %v", name, err)
		}
	}
}

func TestRestoreRejectsSymlinkParents(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "dest")
	outside := filepath.Join(root, "outside")
	makeDir(t, dest)
	makeDir(t, outside)
	if err := os.Symlink(outside, filepath.Join(dest, "nested")); err != nil {
		t.Skip(err)
	}
	if err := safeParents(dest, filepath.Join(dest, "nested")); err == nil {
		t.Fatal("accepted symlink parent")
	}
}

func TestUserRelativeRejectsAbsoluteRoots(t *testing.T) {
	paths := []string{"/", "//", "/absolute", "/absolute/"}
	if runtime.GOOS == "windows" {
		paths = append(paths, `\`, `C:\`, `C:/`, `C:relative`, `\\server\share\`)
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			if got, err := userRelative(p); err == nil {
				t.Fatalf("accepted absolute path %q as %q", p, got)
			}
		})
	}
	for _, tc := range []struct{ input, want string }{{"", ""}, {".", ""}, {"nested/", "nested"}, {"nested/file", "nested/file"}} {
		got, err := userRelative(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("relative path %q: %q, %v", tc.input, got, err)
		}
	}
}

func TestNativeRelativeRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escape", "a/../file", "/absolute", "a//file", "a/./file", "a\x00b"} {
		if _, err := nativeRelative(name); err == nil {
			t.Fatalf("accepted unsafe native path %q", name)
		}
	}
}
