package hobalight

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"sort"
)

// storageBackend addresses repository data with slash-separated keys relative to
// the repository root. Keys are not local paths. Directory entries describe
// logical namespaces; remote adapters may synthesize them and page internally.
// Implementations must support concurrent calls and wrap fs.ErrNotExist for
// missing data. Write publishes complete bytes, never a partially written value.
// Move preserves the source if copying to the destination fails. It is used only
// under maintenance coordination and need not be an atomic rename.
// Safe writes reject non-regular destinations and unsafe parent namespaces.
// Coordination is a separate contract: storing marker objects alone is not a
// safe default implementation for an arbitrary remote provider.
type storageBackend interface {
	storageCoordination
	Read(key string) ([]byte, error)
	Open(key string) (storageReader, error)
	Write(key string, source io.Reader, safe bool) error
	Stat(key string) (fs.FileInfo, error)
	Inspect(key string) (fs.FileInfo, error)
	Scan(namespace string, visit func(fs.DirEntry) error) error
	Walk(namespace string, visit fs.WalkDirFunc) error
	EnsureNamespace(namespace string, safe bool) error
	Move(from, to string) error
	Remove(key string) error
	Reserve(namespace, pattern string) (string, error)
	// LocalRoot is empty for remote storage. It is used only to exclude the local
	// repository from source traversal, never to read or write repository data.
	LocalRoot() string
	NewLog() (io.WriteCloser, error)
}

// ZIP blobs need random access. A remote implementation may use range requests
// or a bounded-memory temporary-file download; callers always close the reader.
type storageReader interface {
	io.Reader
	io.ReaderAt
	io.Closer
	Size() int64
}

func objectKey(id string) string { return "objects/" + id[:2] + "/" + id }

type storageActivity struct {
	writers     int
	maintenance bool
}

// BeginWrite permits concurrent writers but excludes maintenance. Maintenance
// excludes both writers and other maintenance until the returned release runs.
// On failure neither method may leave a newly acquired lock behind. Backends
// unable to provide these guarantees must return an error, never a no-op release.
type storageCoordination interface {
	Activity() (storageActivity, error)
	BeginWrite(operation string) (func(), error)
	BeginMaintenance(operation string) (func(), error)
}

// storageEntries is for commands that already retain complete reference lists.
// Object-prefix resolution and recovery use Scan directly to bound memory.
func storageEntries(backend storageBackend, namespace string) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	err := backend.Scan(namespace, func(entry fs.DirEntry) error { entries = append(entries, entry); return nil })
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, err
}

func writeStorageBytes(backend storageBackend, key string, data []byte, safe bool) error {
	err := backend.Write(key, bytes.NewReader(data), safe)
	// Preserve publication of identical bytes when another writer won a race.
	// A safe-write refusal must not be hidden by reading an unsafe destination.
	if err != nil && !safe {
		if old, readErr := backend.Read(key); readErr == nil && bytes.Equal(old, data) {
			return nil
		}
	}
	return err
}

var errStopStorageScan = errors.New("stop storage scan")
