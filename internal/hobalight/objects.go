package hobalight

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"
)

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func encode(v any) ([]byte, error) { return yaml.Marshal(v) }

// put stores a small metadata object. An existing object is kept only when its
// bytes match; replaced bytes are archived before publishing the new version.
func (s *Store) put(id string, data []byte) error {
	key := objectKey(id)
	if old, err := s.backend.Read(key); err == nil && bytes.Equal(old, data) {
		return nil
	} else if err == nil {
		if err = s.archiveBroken(id, old); err != nil {
			return err
		}
		s.logger.Printf("replace object=%s reason=content_mismatch", id)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeStorageBytes(s.backend, key, data, false)
}

func (s *Store) resolve(prefix string, kind byte) (string, error) {
	return s.resolveKinds(prefix, string(kind))
}

func (s *Store) resolveKinds(prefix, kinds string) (string, error) {
	if len(prefix) > 0 && strings.ContainsRune(kinds, rune(prefix[len(prefix)-1])) && (len(prefix) == 65 || prefix[len(prefix)-1] == 'S') {
		kinds = prefix[len(prefix)-1:]
		prefix = prefix[:len(prefix)-1]
	}
	if len(prefix) < 8 || len(prefix) > 64 {
		return "", errors.New("object hash must contain 8 to 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(prefix + strings.Repeat("0", len(prefix)%2)); err != nil {
		return "", errors.New("invalid hexadecimal hash")
	}
	prefix = strings.ToLower(prefix)
	found := ""
	err := s.backend.Scan("objects/"+prefix[:2], func(e fs.DirEntry) error {
		n := e.Name()
		if !e.IsDir() && len(n) == 65 && strings.ContainsRune(kinds, rune(n[64])) && validID(n, n[64]) && strings.HasPrefix(n, prefix) {
			if found != "" {
				return errors.New("ambiguous hash: provide more characters")
			}
			found = n
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("object not found")
	}
	if err != nil {
		return "", err
	}

	if found == "" {
		return "", errors.New("object not found")
	}
	return found, nil
}

func validID(id string, kind byte) bool {
	if len(id) != 65 || id[64] != kind {
		return false
	}
	_, err := hex.DecodeString(id[:64])
	return err == nil
}

// Sessions hash their exact YAML bytes. Companions hash Identity (including any error);
// legacy companions whose IDs hash the full YAML remain readable.
func (s *Store) read(id string, kind byte, out any) error {
	if (kind != 'S' && kind != 'F') || !validID(id, kind) {
		return errors.New("invalid object reference")
	}
	b, err := s.backend.Read(objectKey(id))
	if err != nil {
		return err
	}
	return decodeObject(id, kind, b, out)
}
func decodeObject(id string, kind byte, b []byte, out any) error {
	if !validID(id, kind) {
		return errors.New("invalid object reference")
	}
	if kind == 'S' {
		if hash(b) != id[:64] {
			return errors.New("session checksum mismatch")
		}
		return yaml.Unmarshal(b, out)
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return err
	}
	identityBytes, err := encode(f.Identity)
	if err != nil {
		return err
	}
	if hash(identityBytes) != id[:64] && hash(b) != id[:64] {
		return errors.New("file metadata checksum mismatch")
	}
	if (f.Error == "" && !validID(f.Blob, 'B')) || (f.Error != "" && f.Blob != "") {
		return errors.New("invalid blob reference")
	}
	destination, ok := out.(*File)
	if !ok {
		return errors.New("file metadata destination must be *File")
	}
	*destination = f
	return nil
}
