package hobalight

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshReferences(t *testing.T) {
	for _, mode := range []string{"missing-directory", "corrupt", "legacy", "outdated-listing"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeFile(t, filepath.Join(source, "nested", "file"), []byte("hello"))
			s := newStore(t, filepath.Join(root, "store"))
			id, err := s.backup(source, false)
			must(t, err)
			refs, err := s.sessionReferences()
			must(t, err)
			objects := readFile(t, s.object(id))
			dir := filepath.Join(s.backend.LocalRoot(), "sessions")
			entries, err := os.ReadDir(dir)
			must(t, err)
			name := filepath.Join(dir, entries[0].Name())
			switch mode {
			case "missing-directory":
				must(t, os.RemoveAll(dir))
			case "corrupt":
				writeFile(t, name, []byte("broken: ["))
			case "legacy":
				refs[0].Format = 0
				refs[0].Files = nil
				data, err := encode(refs[0])
				must(t, err)
				writeFile(t, name, data)
			case "outdated-listing":
				(*refs[0].Files)[0].Size = 99
				data, err := encode(refs[0])
				must(t, err)
				writeFile(t, name, data)
			}
			s, err = readStoreMode(s.backend.LocalRoot(), true)
			must(t, err)
			var output bytes.Buffer
			must(t, s.refresh(&output))
			if !strings.Contains(output.String(), "1 refreshed") {
				t.Fatal(output.String())
			}
			refs, err = s.sessionReferences()
			must(t, err)
			if len(refs) != 1 || refs[0].Format != referenceFormat || refs[0].Files == nil || (*refs[0].Files)[0].Path != "nested/file" || (*refs[0].Files)[0].Size != 5 {
				t.Fatalf("%+v", refs)
			}
			if !bytes.Equal(objects, readFile(t, s.object(id))) {
				t.Fatal("object changed")
			}
			output.Reset()
			must(t, s.refresh(&output))
			if !strings.Contains(output.String(), "0 refreshed, 1 unchanged") {
				t.Fatal(output.String())
			}
		})
	}
}

func TestRefreshContinuesPastDamagedObjectsAndPreservesNewerFormats(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("hello"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	refs[0].Format = referenceFormat + 1
	data, err := encode(refs[0])
	must(t, err)
	entries, err := os.ReadDir(filepath.Join(s.backend.LocalRoot(), "sessions"))
	must(t, err)
	name := filepath.Join(s.backend.LocalRoot(), "sessions", entries[0].Name())
	writeFile(t, name, data)
	badID := strings.Repeat("e", 64) + "S"
	writeFile(t, s.object(badID), []byte("damaged"))
	var output bytes.Buffer
	if err := s.refresh(&output); err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(output.String(), "2 failed") {
		t.Fatal(output.String())
	}
	if !bytes.Equal(data, readFile(t, name)) {
		t.Fatal("newer reference overwritten")
	}
	must(t, os.Remove(name))
	output.Reset()
	if err := s.refresh(&output); err == nil {
		t.Fatal("expected corrupt object failure")
	}
	if !strings.Contains(output.String(), "1 refreshed") {
		t.Fatal(output.String())
	}
	if _, err := os.Stat(s.object(id)); err != nil {
		t.Fatal(err)
	}
}
