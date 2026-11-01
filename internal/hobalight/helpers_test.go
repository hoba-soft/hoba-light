package hobalight

import (
	"os"
	"path/filepath"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func makeDir(t *testing.T, p string) { t.Helper(); must(t, os.MkdirAll(p, 0700)) }

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	makeDir(t, filepath.Dir(p))
	must(t, os.WriteFile(p, b, 0600))
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return b
}

func newStore(t *testing.T, root string) *Store {
	t.Helper()
	s, err := openStore(root)
	must(t, err)
	return s
}

func changeWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	previous, err := os.Getwd()
	must(t, err)
	must(t, os.Chdir(directory))
	t.Cleanup(func() { must(t, os.Chdir(previous)) })
}

// Local paths are used only by tests that inspect or damage the filesystem backend.
func (s *Store) object(id string) string {
	return filepath.Join(s.backend.LocalRoot(), filepath.FromSlash(objectKey(id)))
}
