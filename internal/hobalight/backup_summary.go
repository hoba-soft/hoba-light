package hobalight

import (
	"context"
	"fmt"
	"io"
	"time"
)

type backupFileWrites struct {
	addedID      string
	reused       bool
	publications int
	bytes        int64
}
type backupSummary struct {
	newFiles, modified, unchanged, failed, unclassified int
	reused, writes, otherErrors                         int
	logicalBytes, writtenBytes, addedBytes              int64
	elapsed                                             time.Duration
	baseline                                            string
	comparisonError                                     error
}

// Select from authoritative session objects, not the derived sessions cache.
// An unavailable baseline never prevents a backup or classifies files as new.
func (s *Store) previousBackup(directory string) (string, map[string]File, error) {
	latest := ""
	var newest time.Time
	err := s.walkObjects(func(id string, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !validID(id, 'S') {
			return nil
		}
		session, e := s.scanDiffSession(context.Background(), id, func(string) error { return nil })
		if e != nil {
			return e
		}
		if session.Directory != directory {
			return nil
		}
		started, e := time.Parse(time.RFC3339Nano, session.Started)
		if e != nil {
			return e
		}
		if latest == "" || started.After(newest) || started.Equal(newest) && id > latest {
			latest, newest = id, started
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	previous := map[string]File{}
	if latest == "" {
		return "", previous, nil
	}
	_, files, err := s.sessionFiles(latest)
	if err != nil {
		return "", nil, err
	}
	for _, f := range files {
		previous[f.metadata.Path] = f.metadata
	}
	return latest, previous, nil
}
func (b backupSummary) print(w io.Writer) error {
	var err error
	switch {
	case b.comparisonError != nil:
		_, err = fmt.Fprintf(w, "Comparison unavailable: %s; %d saved (changes unknown)\n", browserText(b.comparisonError.Error()), b.unclassified)
	case b.baseline == "":
		_, err = fmt.Fprintf(w, "First session: %d new\n", b.newFiles)
	default:
		_, err = fmt.Fprintf(w, "Compared with session %.8s: %d new, %d modified, %d unchanged\n", b.baseline, b.newFiles, b.modified, b.unchanged)
	}
	return err
}
