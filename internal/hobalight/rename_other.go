//go:build !windows

package hobalight

// Rename replaces open files on Unix, so no failure is transient.
func isTransientRename(error) bool { return false }
