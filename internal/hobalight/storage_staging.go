package hobalight

import (
	"errors"
	"os"
)

// Local storage can stage on its own volume and publish by rename, preserving
// the existing disk I/O cost. Remote backends need only implement Write: the
// fallback stages locally and uploads a complete stream without buffering it all
// in memory. The caller owns and cleans up every staging file.
type localStorageStaging interface {
	NewStage(pattern string) (*os.File, error)
	PublishStage(key, filename string, safe bool) error
}

func newStorageStage(backend storageBackend, pattern string) (*os.File, error) {
	if local, ok := backend.(localStorageStaging); ok {
		return local.NewStage(pattern)
	}
	return os.CreateTemp("", "hobalight-"+pattern)
}
func publishStorageStage(backend storageBackend, key, filename string, safe bool) error {
	if local, ok := backend.(localStorageStaging); ok {
		return local.PublishStage(key, filename, safe)
	}
	input, err := os.Open(filename)
	if err != nil {
		return err
	}
	return errors.Join(backend.Write(key, input, safe), input.Close())
}
