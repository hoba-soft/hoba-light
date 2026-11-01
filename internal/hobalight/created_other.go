//go:build !windows && !darwin && !linux

package hobalight

import "os"

// Unavailable creation times have a consistent YAML null representation.
func creationTime(_ string, info os.FileInfo) *string { return nil }
