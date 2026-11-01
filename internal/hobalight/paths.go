package hobalight

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func portable(p string) string { return filepath.ToSlash(p) }

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func canonical(p string) string {
	if v, err := filepath.EvalSymlinks(p); err == nil {
		return v
	}
	return p
}

// validRelative accepts slash-separated paths whose components are ordinary
// names. Characters such as ':' and '\' are valid in Linux and macOS file names,
// so they are checked only when restoring on Windows (see nativeRelative).
func validRelative(rel string) bool {
	if rel == "" || strings.ContainsRune(rel, 0) {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// Paths stored in a session are always slash-separated, even for Windows sources.
func relative(root, name string) (string, error) {
	root = strings.TrimRight(root, "/")
	prefix := root + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", errors.New("file is outside session directory")
	}
	rel := strings.TrimPrefix(name, prefix)
	if !validRelative(rel) {
		return "", errors.New("unsafe relative path")
	}
	return rel, nil
}

// userRelative normalizes a relative path typed by the user. Backslashes are
// separators only on Windows; elsewhere they are part of a file name.
func userRelative(p string) (string, error) {
	if runtime.GOOS == "windows" {
		p = strings.ReplaceAll(p, "\\", "/")
	}
	// Reject absolute paths before trimming: "/" must not become the empty
	// selection used to restore an entire session.
	if strings.HasPrefix(p, "/") || (runtime.GOOS == "windows" && filepath.VolumeName(p) != "") {
		return "", errors.New("path must be relative to the session directory")
	}
	p = strings.TrimRight(p, "/")
	if p == "" || p == "." {
		return "", nil
	}
	if strings.HasPrefix(p, "/") || !validRelative(p) {
		return "", errors.New("path must be relative to the session directory")
	}
	return p, nil
}

// nativeRelative converts a validated session path into a path for the local
// filesystem, rejecting names that the local platform would interpret
// differently (separators, drive letters, or alternate data streams).
func nativeRelative(rel string) (string, error) {
	if !validRelative(rel) {
		return "", errors.New("unsafe relative path")
	}
	if runtime.GOOS == "windows" {
		for _, part := range strings.Split(rel, "/") {
			if strings.ContainsAny(part, "\\:*?\"<>|") {
				return "", errors.New("file name cannot be restored on Windows")
			}
		}
	}
	return filepath.FromSlash(rel), nil
}
