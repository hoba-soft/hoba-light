package hobalight

import "archive/zip"

type storageArchive struct {
	*zip.Reader
	source storageReader
}

func (z *storageArchive) Close() error { return z.source.Close() }
func (s *Store) openArchive(key string) (*storageArchive, error) {
	source, err := s.backend.Open(key)
	if err != nil {
		return nil, err
	}
	z, err := zip.NewReader(source, source.Size())
	if err != nil {
		source.Close()
		return nil, err
	}
	return &storageArchive{Reader: z, source: source}, nil
}
