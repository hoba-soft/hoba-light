package hobalight

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type recoveryCandidate struct {
	source string
	local  bool
}

func checkRecovery(id, name string, out any) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("recovery source is not a regular file")
	}
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	return checkRecoveryReader(id, &filesystemReader{File: file, size: info.Size()}, out)
}

func checkRecoveryReader(id string, source storageReader, out any) error {
	if validID(id, 'B') {
		z, err := zip.NewReader(source, source.Size())
		if err != nil {
			return err
		}
		if len(z.File) != 1 {
			return errors.New("blob must contain exactly one file")
		}
		r, err := z.File[0].Open()
		if err != nil {
			return err
		}
		defer r.Close()
		h := sha256.New()
		if _, err = io.Copy(h, r); err != nil {
			return err
		}
		if hex.EncodeToString(h.Sum(nil)) != id[:64] {
			return errors.New("blob checksum mismatch")
		}
		return nil
	}
	data, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	if out == nil {
		if validID(id, 'S') {
			out = &Session{}
		} else {
			out = &File{}
		}
	}
	return decodeObject(id, id[64], data, out)
}

func (s *Store) checkStoredRecovery(id, key string, out any) error {
	info, err := s.backend.Inspect(key)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("recovery source is not a regular file")
	}
	source, err := s.backend.Open(key)
	if err != nil {
		return err
	}
	defer source.Close()
	return checkRecoveryReader(id, source, out)
}
func (v *verifier) findTrash(id string, kind byte, out any) bool {
	if !validID(id, kind) {
		return false
	}
	if candidate, ok := v.recovery[id]; ok && !candidate.local {
		return v.store.checkStoredRecovery(id, candidate.source, out) == nil
	}
	if v.searched == nil {
		v.searched = map[string]bool{}
	}
	if v.recovery == nil {
		v.recovery = map[string]recoveryCandidate{}
	}
	if v.searched[id] {
		return false
	}
	v.searched[id] = true
	info, err := v.store.backend.Inspect("trash")
	if err != nil || !info.IsDir() {
		return false
	}
	found := false
	_ = v.store.backend.Scan("trash", func(entry fs.DirEntry) error {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), id+"-") {
			return nil
		}
		key := path.Join("trash", entry.Name())
		if v.store.checkStoredRecovery(id, key, out) != nil {
			return nil
		}
		v.recovery[id] = recoveryCandidate{source: key}
		v.print("Recoverable %s from trash: %s\n", id, browserText(key))
		found = true
		return errStopStorageScan
	})
	return found
}

func (v *verifier) metadata(id string, kind byte, out any) error {
	err := v.regular(id)
	if err == nil {
		err = v.store.read(id, kind, out)
	}
	if err == nil {
		return nil
	}
	if v.findTrash(id, kind, out) {
		// Continue traversing a valid recovery candidate without altering storage.
		v.issue("object "+id, err)
		return nil
	}
	return err
}
func localHash(name string) (string, error) {
	info, err := os.Stat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("local source is not a regular file")
	}
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("local source is not a regular file")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (v *verifier) findLocal(f File) {
	if _, ok := v.recovery[f.Blob]; ok {
		return
	}
	// Windows paths on Unix (or vice versa) must not turn into relative lookups.
	key := "local:" + f.Blob + ":" + f.Path
	if v.searched == nil {
		v.searched = map[string]bool{}
	}
	if v.searched[key] {
		return
	}
	v.searched[key] = true
	name := filepath.FromSlash(f.Path)
	if !filepath.IsAbs(name) {
		return
	}
	sum, err := localHash(name)
	if err != nil || sum != f.Blob[:64] {
		return
	}
	if v.recovery == nil {
		v.recovery = map[string]recoveryCandidate{}
	}
	v.recovery[f.Blob] = recoveryCandidate{source: name, local: true}
	v.print("Recoverable %s from original file: %s\n", f.Blob, browserText(name))
}
func (s *Store) repairObject(id string, c recoveryCandidate) error {
	if err := s.backend.EnsureNamespace(path.Dir(objectKey(id)), true); err != nil {
		return err
	}
	tmp, err := newStorageStage(s.backend, ".repair-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	var input io.ReadCloser
	if c.local {
		info, e := os.Stat(c.source)
		if e != nil || !info.Mode().IsRegular() {
			tmp.Close()
			return errors.New("recovery source is not a regular file")
		}
		file, e := os.Open(c.source)
		if e != nil {
			tmp.Close()
			return e
		}
		info, e = file.Stat()
		if e != nil || !info.Mode().IsRegular() {
			file.Close()
			tmp.Close()
			return errors.New("recovery source is not a regular file")
		}
		input = file
	} else {
		info, e := s.backend.Inspect(c.source)
		if e != nil || !info.Mode().IsRegular() {
			tmp.Close()
			return errors.New("recovery source is not a regular file")
		}
		input, err = s.backend.Open(c.source)
		if err != nil {
			tmp.Close()
			return err
		}
	}
	if c.local {
		z := zip.NewWriter(tmp)
		w, e := z.Create("data")
		if e == nil {
			_, e = io.Copy(w, input)
		}
		err = errors.Join(e, z.Close())
	} else {
		_, err = io.Copy(tmp, input)
	}
	err = errors.Join(err, input.Close(), tmp.Sync(), tmp.Close())
	if err != nil {
		return err
	}
	// Revalidate staged bytes in case the source changed after verification.
	if err = checkRecovery(id, name, nil); err != nil {
		return err
	}
	if info, e := s.backend.Inspect(objectKey(id)); e == nil {
		if !info.Mode().IsRegular() {
			return errors.New("refusing to replace a non-regular object")
		}
		if err = s.trashObject(id, "broken"); err != nil {
			return err
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return publishStorageStage(s.backend, objectKey(id), name, true)
}

func (v *verifier) resolveSession(prefix string) (string, error) {
	id, err := v.store.resolve(prefix, 'S')
	if err == nil {
		return id, nil
	}
	// Only an explicitly requested session may be resurrected solely from trash.
	if err.Error() != "object not found" {
		return "", err
	}
	raw := strings.TrimSuffix(prefix, "S")
	if len(raw) < 8 || len(raw) > 64 {
		return "", err
	}
	if _, e := hex.DecodeString(raw + strings.Repeat("0", len(raw)%2)); e != nil {
		return "", err
	}
	raw = strings.ToLower(raw)
	entries, e := storageEntries(v.store.backend, "trash")
	if e != nil {
		return "", err
	}
	found := ""
	for _, entry := range entries {
		name := entry.Name()
		if len(name) < 67 || name[65] != '-' || !validID(name[:65], 'S') || !strings.HasPrefix(name, raw) {
			continue
		}
		candidate := name[:65]
		if v.store.checkStoredRecovery(candidate, path.Join("trash", name), nil) != nil {
			continue
		}
		if found != "" && found != candidate {
			return "", fmt.Errorf("ambiguous hash: provide more characters")
		}
		found = candidate
	}
	if found == "" {
		return "", err
	}
	return found, nil
}

// References are hints for missing roots; their cached file lists are not trusted.
func (v *verifier) missingRoots() {
	dir := "sessions"
	entries, err := storageEntries(v.store.backend, dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !isSessionReference(entry) {
			continue
		}
		data, err := v.store.backend.Read(path.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var ref struct {
			Session string `yaml:"session"`
		}
		if yaml.Unmarshal(data, &ref) != nil || !validID(ref.Session, 'S') || v.checked[ref.Session] {
			continue
		}
		v.session(ref.Session)
	}
}
func (s *Store) repairReference(id string) error {
	ref, err := s.buildReference(id)
	if err != nil {
		return err
	}
	data, err := encode(ref)
	if err != nil {
		return err
	}
	started, err := time.Parse(time.RFC3339Nano, ref.Started)
	if err != nil {
		return err
	}
	return writeStorageBytes(s.backend, path.Join("sessions", sessionReferenceName(started, id)), data, true)
}
