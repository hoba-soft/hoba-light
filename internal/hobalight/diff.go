package hobalight

import (
	"context"
	"fmt"
	"io"
)

// Paths are relative to each source root; absolute source locations are ignored.
func sameFileMetadata(a, b File) bool {
	createdEqual := a.Created == nil && b.Created == nil || a.Created != nil && b.Created != nil && *a.Created == *b.Created
	return a.Error == b.Error && a.Blob == b.Blob && a.Size == b.Size && a.Modified == b.Modified && createdEqual
}

func modificationDetails(before, after File) []string {
	var details []string
	if before.Error != after.Error {
		details = append(details, fmt.Sprintf("error %q -> %q", before.Error, after.Error))
	}
	if before.Blob != after.Blob {
		details = append(details, "content: changed")
	}
	if before.Size != after.Size {
		details = append(details, fmt.Sprintf("size: %s (%d bytes) -> %s (%d bytes)", displaySize(before.Size), before.Size, displaySize(after.Size), after.Size))
	}
	created := func(value *string) string {
		if value == nil {
			return "unavailable"
		}
		return *value
	}
	if (before.Created == nil) != (after.Created == nil) || before.Created != nil && after.Created != nil && *before.Created != *after.Created {
		details = append(details, "created: "+created(before.Created)+" -> "+created(after.Created))
	}
	if before.Modified != after.Modified {
		details = append(details, "modified: "+before.Modified+" -> "+after.Modified)
	}
	return details
}

func (s *Store) diff(first, second string, output io.Writer) error {
	return s.diffTargets(context.Background(), diffTarget{id: first}, diffTarget{id: second}, "auto", output)
}
