package hobalight

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// This backend has no local repository at all. It catches accidental path-based
// I/O in workflows that must work with future remote implementations.
type memoryStorage struct {
	mu                sync.Mutex
	files             fstest.MapFS
	sequence, writers int
	maintenance       bool
}

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{files: fstest.MapFS{
		"objects":  {Mode: fs.ModeDir | 0700},
		"sessions": {Mode: fs.ModeDir | 0700},
	}}
}
func (m *memoryStorage) snapshot() fstest.MapFS {
	m.mu.Lock()
	defer m.mu.Unlock()
	files := fstest.MapFS{}
	for key, file := range m.files {
		copy := *file
		files[key] = &copy
	}
	return files
}
func (m *memoryStorage) Read(key string) ([]byte, error)         { return fs.ReadFile(m.snapshot(), key) }
func (m *memoryStorage) Stat(key string) (fs.FileInfo, error)    { return fs.Stat(m.snapshot(), key) }
func (m *memoryStorage) Inspect(key string) (fs.FileInfo, error) { return m.Stat(key) }
func (m *memoryStorage) Scan(key string, visit func(fs.DirEntry) error) error {
	entries, err := fs.ReadDir(m.snapshot(), key)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := visit(entry); err != nil {
			if errors.Is(err, errStopStorageScan) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (m *memoryStorage) Walk(key string, visit fs.WalkDirFunc) error {
	return fs.WalkDir(m.snapshot(), key, visit)
}
func (m *memoryStorage) EnsureNamespace(key string, safe bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key != "." {
		m.files[key] = &fstest.MapFile{Mode: fs.ModeDir | 0700}
	}
	return nil
}
func (m *memoryStorage) Write(key string, r io.Reader, safe bool) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = &fstest.MapFile{Data: data, Mode: 0600}
	return nil
}
func (m *memoryStorage) Move(from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[from]
	if !ok {
		return fs.ErrNotExist
	}
	m.files[to] = file
	delete(m.files, from)
	return nil
}
func (m *memoryStorage) Remove(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[key]; !ok {
		return fs.ErrNotExist
	}
	delete(m.files, key)
	return nil
}
func (m *memoryStorage) Reserve(namespace, pattern string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sequence++
	key := path.Join(namespace, strings.Replace(pattern, "*", fmt.Sprint(m.sequence), 1))
	m.files[key] = &fstest.MapFile{Mode: 0600}
	return key, nil
}
func (m *memoryStorage) LocalRoot() string { return "" }

type discardLog struct{ io.Writer }

func (discardLog) Close() error                          { return nil }
func (m *memoryStorage) NewLog() (io.WriteCloser, error) { return discardLog{io.Discard}, nil }

type memoryStorageReader struct{ *bytes.Reader }

func (*memoryStorageReader) Close() error { return nil }
func (m *memoryStorage) Open(key string) (storageReader, error) {
	data, err := m.Read(key)
	if err != nil {
		return nil, err
	}
	return &memoryStorageReader{bytes.NewReader(data)}, nil
}
func (m *memoryStorage) BeginWrite(string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.maintenance {
		return nil, errors.New("maintenance active")
	}
	m.writers++
	return func() { m.mu.Lock(); defer m.mu.Unlock(); m.writers-- }, nil
}
func (m *memoryStorage) BeginMaintenance(string) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.maintenance || m.writers != 0 {
		return nil, errors.New("storage busy")
	}
	m.maintenance = true
	return func() { m.mu.Lock(); defer m.mu.Unlock(); m.maintenance = false }, nil
}

func TestWorkflowsWithoutLocalStorage(t *testing.T) {
	backend := newMemoryStorage()
	s := storeWithBackend(backend)
	s.workers = 4
	source := filepath.Join(t.TempDir(), "source")
	original := []byte("hello remote storage\n")
	writeFile(t, filepath.Join(source, "nested", "file.txt"), original)
	// Exercise concurrent source workers and deduplication against the same blob.
	writeFile(t, filepath.Join(source, "copy.txt"), original)
	var summary backupSummary
	sid, err := s.backupWithSummary(source, false, &summary)
	must(t, err)
	if summary.newFiles != 2 {
		t.Fatalf("summary: %+v", summary)
	}
	refs, err := s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Session != sid {
		t.Fatalf("references: %+v", refs)
	}
	_, files, err := s.sessionFiles(sid)
	must(t, err)
	if len(files) != 2 {
		t.Fatal(files)
	}
	var content bytes.Buffer
	must(t, s.diffFileContent(context.Background(), File{}, files[0].metadata, "text", &content))
	if !strings.Contains(content.String(), "+hello remote storage") {
		t.Fatal(content.String())
	}
	index, err := s.buildDiffIndex(context.Background(), diffTarget{id: sid})
	must(t, err)
	if index.count != 2 {
		t.Fatal(index.count)
	}
	index.close()
	destination := filepath.Join(t.TempDir(), "restore")
	must(t, s.restore(sid[:8], destination, ""))
	if !bytes.Equal(readFile(t, filepath.Join(destination, "nested", "file.txt")), original) {
		t.Fatal("restore changed bytes")
	}
	must(t, s.verify(nil, io.Discard))
	must(t, s.refresh(io.Discard))
	stats, err := s.backendStats()
	must(t, err)
	if stats.sessions != 1 || stats.objects != 4 {
		t.Fatalf("stats: %+v", stats)
	}
	// Repair metadata and compressed content exclusively from remote trash.
	must(t, s.trashObject(files[0].id, "orphan"))
	blob := files[0].metadata.Blob
	must(t, s.trashObject(blob, "orphan"))
	must(t, s.backend.Write(objectKey(blob), strings.NewReader("damaged"), false))
	must(t, os.RemoveAll(source))
	if err := s.verify([]string{sid}, io.Discard); err == nil {
		t.Fatal("damage was not detected")
	}
	must(t, s.verify([]string{"--repair", sid}, io.Discard))
	// Recovery of a missing root must also search remote references and trash.
	must(t, s.trashObject(sid, "deleted"))
	must(t, s.verify([]string{"--repair", sid[:8]}, io.Discard))
	next, err := s.deletePath(sid, "nested", io.Discard)
	must(t, err)
	_, remaining, err := s.sessionFiles(next)
	must(t, err)
	if len(remaining) != 1 {
		t.Fatal(remaining)
	}
	must(t, s.deleteSession(next, io.Discard))
	must(t, s.gc(io.Discard))
	stats, err = s.backendStats()
	must(t, err)
	if stats.objects != 0 {
		t.Fatalf("GC retained unreachable objects: %+v", stats)
	}
	_, err = s.expireTrash(time.Now().AddDate(0, 0, 31))
	must(t, err)
	trash, err := storageEntries(backend, "trash")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if len(trash) != 0 {
		t.Fatal("expired trash retained", trash)
	}
}

type failingStorage struct {
	storageBackend
	failure error
}

func (s failingStorage) BeginWrite(string) (func(), error)       { return nil, s.failure }
func (s failingStorage) BeginMaintenance(string) (func(), error) { return nil, s.failure }
func TestBackendCoordinationFailureStopsMutation(t *testing.T) {
	backend := newMemoryStorage()
	failure := errors.New("provider cannot coordinate")
	s := storeWithBackend(failingStorage{storageBackend: backend, failure: failure})
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "file"), []byte("data"))
	_, err := s.backup(source, false)
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, operation := range []func() error{
		func() error { return s.gc(io.Discard) },
		func() error { return s.refresh(io.Discard) },
		func() error { return s.deleteSession("12345678", io.Discard) },
		func() error { return s.verify(nil, io.Discard) },
	} {
		if err := operation(); !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
	if len(backend.snapshot()) != 2 {
		t.Fatal("failed coordination changed storage")
	}
}

type interruptedStorageReader struct{ sent bool }

func (r *interruptedStorageReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.ErrUnexpectedEOF
	}
	r.sent = true
	return copy(p, "partial"), nil
}
func TestFilesystemStorageFailedWritePreservesDestination(t *testing.T) {
	backend := &filesystemStorage{root: t.TempDir()}
	key := "objects/ab/value"
	must(t, backend.Write(key, strings.NewReader("original"), false))
	err := backend.Write(key, &interruptedStorageReader{}, false)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	data, err := backend.Read(key)
	must(t, err)
	if string(data) != "original" {
		t.Fatalf("partial publication: %q", data)
	}
	entries, err := os.ReadDir(filepath.Dir(backend.name(key)))
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("staging files leaked: %v", entries)
	}
}
func TestFilesystemStorageSafeWriteRejectsSymlink(t *testing.T) {
	backend := &filesystemStorage{root: t.TempDir()}
	outside := filepath.Join(t.TempDir(), "file")
	writeFile(t, outside, []byte("original"))
	if err := os.Symlink(outside, backend.name("link")); err != nil {
		t.Skip(err)
	}
	if err := writeStorageBytes(backend, "link", []byte("original"), true); err == nil {
		t.Fatal("safe write followed symlink or hid refusal")
	}
	if string(readFile(t, outside)) != "original" {
		t.Fatal("outside destination changed")
	}
}
func TestFilesystemStorageScanStopsAndPropagatesErrors(t *testing.T) {
	backend := &filesystemStorage{root: t.TempDir()}
	// Cross the backend's page boundary and check that callback errors survive.
	for i := 0; i < 300; i++ {
		must(t, backend.Write(fmt.Sprintf("objects/%03d", i), strings.NewReader("data"), false))
	}
	count := 0
	must(t, backend.Scan("objects", func(fs.DirEntry) error {
		count++
		if count == 257 {
			return errStopStorageScan
		}
		return nil
	}))
	if count != 257 {
		t.Fatal(count)
	}
	failure := errors.New("visitor failed")
	if err := backend.Scan("objects", func(fs.DirEntry) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}

type trackedStorage struct {
	storageBackend
	opened, closed int
}
type trackedStorageReader struct {
	storageReader
	owner *trackedStorage
}

func (r *trackedStorageReader) Close() error { r.owner.closed++; return r.storageReader.Close() }
func (s *trackedStorage) Open(key string) (storageReader, error) {
	reader, err := s.storageBackend.Open(key)
	if err != nil {
		return nil, err
	}
	s.opened++
	return &trackedStorageReader{storageReader: reader, owner: s}, nil
}
func TestArchiveFailureClosesBackendReader(t *testing.T) {
	memory := newMemoryStorage()
	must(t, memory.Write("invalid", strings.NewReader("not a ZIP archive"), false))
	backend := &trackedStorage{storageBackend: memory}
	s := storeWithBackend(backend)
	if _, err := s.openArchive("invalid"); err == nil {
		t.Fatal("invalid ZIP accepted")
	}
	if backend.opened != 1 || backend.closed != 1 {
		t.Fatalf("reader leaked: %+v", backend)
	}
}

func (m *memoryStorage) Activity() (storageActivity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return storageActivity{writers: m.writers, maintenance: m.maintenance}, nil
}

func TestRemoteBackendForcedBackupRepairsBlob(t *testing.T) {
	backend := newMemoryStorage()
	s := storeWithBackend(backend)
	source := t.TempDir()
	writeFile(t, filepath.Join(source, "file"), []byte("original"))
	first, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(first)
	must(t, err)
	key := objectKey(files[0].metadata.Blob)
	must(t, backend.Write(key, strings.NewReader("damaged ZIP"), false))
	reused, err := s.backup(source, false)
	must(t, err)
	if err := s.verify([]string{reused}, io.Discard); err == nil {
		t.Fatal("unchanged metadata unexpectedly repaired a damaged blob")
	}
	repaired, err := s.backup(source, true)
	must(t, err)
	must(t, s.verify([]string{repaired}, io.Discard))
	// The same adapter must also upload a locally recovered blob after deletion.
	must(t, backend.Remove(key))
	must(t, s.verify([]string{"--repair", repaired}, io.Discard))
	must(t, s.verify(nil, io.Discard))
}

func TestFilesystemStagingPublishesWithoutCopy(t *testing.T) {
	backend := &filesystemStorage{root: t.TempDir()}
	must(t, backend.EnsureNamespace("objects", false))
	stage, err := newStorageStage(backend, ".blob-*")
	must(t, err)
	name := stage.Name()
	defer os.Remove(name)
	_, err = stage.WriteString("complete content")
	must(t, err)
	must(t, stage.Sync())
	info, err := stage.Stat()
	must(t, err)
	must(t, stage.Close())
	must(t, publishStorageStage(backend, "objects/ab/blob", name, false))
	if _, err := os.Stat(name); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("staging file was not moved: %v", err)
	}
	published, err := backend.Stat("objects/ab/blob")
	must(t, err)
	if !os.SameFile(info, published) {
		t.Fatal("local publication copied instead of renaming")
	}
}
