package hobalight

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type filesystemStorage struct{ root string }

var _ storageBackend = (*filesystemStorage)(nil)

func (s *filesystemStorage) LocalRoot() string { return s.root }
func (s *filesystemStorage) name(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key))
}
func (s *filesystemStorage) Read(key string) ([]byte, error)         { return readPublishedFile(s.name(key)) }
func (s *filesystemStorage) Stat(key string) (fs.FileInfo, error)    { return os.Stat(s.name(key)) }
func (s *filesystemStorage) Inspect(key string) (fs.FileInfo, error) { return os.Lstat(s.name(key)) }
func (s *filesystemStorage) Scan(key string, visit func(fs.DirEntry) error) error {
	directory, err := os.Open(s.name(key))
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		entries, readErr := directory.ReadDir(256)
		for _, entry := range entries {
			if err := visit(entry); err != nil {
				if errors.Is(err, errStopStorageScan) {
					return nil
				}
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (s *filesystemStorage) EnsureNamespace(key string, safe bool) error {
	if safe {
		return safeParents(s.root, s.name(key))
	}
	return os.MkdirAll(s.name(key), 0700)
}
func (s *filesystemStorage) Remove(key string) error    { return os.Remove(s.name(key)) }
func (s *filesystemStorage) Move(from, to string) error { return renameRetry(s.name(from), s.name(to)) }
func (s *filesystemStorage) Walk(key string, visit fs.WalkDirFunc) error {
	return filepath.WalkDir(s.name(key), func(name string, entry fs.DirEntry, err error) error {
		relative, relErr := filepath.Rel(s.root, name)
		if relErr != nil {
			return relErr
		}
		return visit(filepath.ToSlash(relative), entry, err)
	})
}
func (s *filesystemStorage) Reserve(namespace, pattern string) (string, error) {
	if err := s.EnsureNamespace(namespace, false); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(s.name(namespace), pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	relative, err := filepath.Rel(s.root, name)
	return filepath.ToSlash(relative), err
}
func (s *filesystemStorage) NewLog() (io.WriteCloser, error) {
	if err := s.EnsureNamespace("logs", false); err != nil {
		return nil, err
	}
	return os.CreateTemp(s.name("logs"), time.Now().UTC().Format("20060102T150405Z")+"-*.log")
}

type filesystemReader struct {
	*os.File
	size int64
}

func (r *filesystemReader) Size() int64 { return r.size }
func (s *filesystemStorage) Open(key string) (storageReader, error) {
	f, err := os.Open(s.name(key))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &filesystemReader{File: f, size: info.Size()}, nil
}
func (s *filesystemStorage) Write(key string, source io.Reader, safe bool) error {
	if err := s.prepareWrite(key, safe); err != nil {
		return err
	}
	name := s.name(key)
	f, err := os.CreateTemp(filepath.Dir(name), ".hobalight-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, copyErr := io.Copy(f, source)
	err = errors.Join(copyErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return renameRetry(f.Name(), name)
}

func storageExclusion(root, source string) (func(string, os.DirEntry) bool, error) {
	if root == "" {
		return func(string, os.DirEntry) bool { return false }, nil
	}
	storageCanonical := canonical(root)
	if within(storageCanonical, canonical(source)) {
		return nil, errors.New("backup source must be outside storage")
	}
	storageInfo, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	return func(name string, d os.DirEntry) bool {
		if d.Type()&os.ModeSymlink != 0 {
			return within(storageCanonical, canonical(name))
		}
		if d.IsDir() {
			info, err := d.Info()
			return err == nil && os.SameFile(info, storageInfo)
		}
		return false
	}, nil
}

// Windows can briefly deny both reads and replacement while another writer or
// an antivirus scanner holds the destination. Retry only transient OS errors.
func retryFileOperation(operation func() error, transient func(error) bool, delays []time.Duration) error {
	err := operation()
	for _, delay := range delays {
		if err == nil || !transient(err) {
			return err
		}
		time.Sleep(delay)
		err = operation()
	}
	return err
}

func retryStoreOperation(operation func() error) error {
	return retryFileOperation(operation, isTransientRename, []time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond})
}

func renameRetry(from, to string) error {
	return retryStoreOperation(func() error { return os.Rename(from, to) })
}

func readPublishedFile(name string) (data []byte, err error) {
	err = retryStoreOperation(func() error { data, err = os.ReadFile(name); return err })
	return data, err
}

// Every writer uses a private temporary file. Concurrent writers of an identical
// object may keep either copy; the object ID determines the same logical data.
func publish(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".hobalight-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = renameRetry(tmp, name); err == nil {
		return nil
	}
	// Another writer may already have published exactly the same bytes.
	if old, readErr := readPublishedFile(name); readErr == nil && bytes.Equal(old, data) {
		return nil
	}
	return err
}

func (s *filesystemStorage) prepareWrite(key string, safe bool) error {
	name := s.name(key)
	if safe {
		if err := safeParents(s.root, filepath.Dir(name)); err != nil {
			return err
		}
		if info, err := os.Lstat(name); err == nil && !info.Mode().IsRegular() {
			return errors.New("destination is not a regular file")
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	return nil
}
func (s *filesystemStorage) NewStage(pattern string) (*os.File, error) {
	return os.CreateTemp(s.name("objects"), pattern)
}
func (s *filesystemStorage) PublishStage(key, filename string, safe bool) error {
	if err := s.prepareWrite(key, safe); err != nil {
		return err
	}
	return renameRetry(filename, s.name(key))
}
