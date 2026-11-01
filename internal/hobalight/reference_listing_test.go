package hobalight

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReferenceListingAndLegacyFallback(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "nested", "one.txt"), []byte("hello"))
	s := newStore(t, filepath.Join(root, "storage"))
	id, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Files == nil {
		t.Fatal("missing persisted file listing")
	}
	_, want, err := s.sessionFiles(id)
	must(t, err)
	fileID := want[0].id
	original := readFile(t, s.object(fileID))
	must(t, os.Remove(s.object(fileID)))
	// Reopen the store: no metadata object or in-memory state is needed.
	s = newStore(t, s.backend.LocalRoot())
	_, got, err := s.listingFiles(id)
	must(t, err)
	if len(got) != 1 || got[0].relative != want[0].relative || got[0].id != want[0].id || got[0].metadata.Size != want[0].metadata.Size || got[0].metadata.Blob != "" {
		t.Fatal("incorrect compact listing")
	}
	if _, _, err := s.sessionFiles(id); err == nil {
		t.Fatal("full metadata must still come from objects")
	}
	refs, err = s.sessionReferences()
	must(t, err)
	if got := s.referenceStats(refs)[id].size; got != "5 B" {
		t.Fatal(got)
	}
	writeFile(t, s.object(fileID), original)
	entries, err := os.ReadDir(filepath.Join(s.backend.LocalRoot(), "sessions"))
	must(t, err)
	refPath := filepath.Join(s.backend.LocalRoot(), "sessions", entries[0].Name())
	// A malformed listing must use the immutable objects.
	(*refs[0].Files)[0].Path = "../unsafe"
	data, err := encode(refs[0])
	must(t, err)
	writeFile(t, refPath, data)
	s = newStore(t, s.backend.LocalRoot())
	_, got, err = s.listingFiles(id)
	must(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("invalid listing did not fall back")
	}
	// Existing references without a file listing stay readable.
	refs[0].Files = nil
	data, err = encode(refs[0])
	must(t, err)
	writeFile(t, refPath, data)
	s = newStore(t, s.backend.LocalRoot())
	_, got, err = s.listingFiles(id)
	must(t, err)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("legacy metadata differs")
	}
}

func TestBrowserTreeMatchesDirectoryListing(t *testing.T) {
	files := []sessionFile{
		{relative: "root", metadata: File{Identity: Identity{Size: 3}}},
		{relative: "a/one", metadata: File{Identity: Identity{Size: 5}}},
		{relative: "a/deep/two", metadata: File{Identity: Identity{Size: 7}}},
		{relative: "ab/three", metadata: File{Identity: Identity{Size: 11}}},
	}
	tree := browserTree(files)
	for _, dir := range []string{"", "a", "a/deep", "ab"} {
		got, want := tree[dir], browserEntries(files, dir)
		if len(got) != len(want) {
			t.Fatal(dir)
		}
		for i := range got {
			if got[i].name != want[i].name || got[i].size != want[i].size || got[i].directory != want[i].directory {
				t.Fatalf("%s: %+v != %+v", dir, got, want)
			}
		}
	}
}

func BenchmarkBrowserLargeDirectory(b *testing.B) {
	for _, count := range []int{1000, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			files := make([]sessionFile, count)
			for i := range files {
				files[i] = sessionFile{relative: fmt.Sprintf("file-%06d.txt", i), id: strings.Repeat("a", 64) + "F", metadata: File{Identity: Identity{Size: 100}}}
			}
			u := browserUI{files: files, ref: &Reference{Session: strings.Repeat("b", 64) + "S"}}
			u.prepareTree()
			u.entries = u.tree[""]
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				u.cursor = i % len(u.entries)
				u.view(100, 24)
			}
		})
	}
}

func TestEmptyReferenceListing(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "empty")
	makeDir(t, source)
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	refs, err := s.sessionReferences()
	must(t, err)
	if len(refs) != 1 || refs[0].Files == nil || len(*refs[0].Files) != 0 {
		t.Fatal("missing empty index")
	}
	_, files, err := s.listingFiles(id)
	must(t, err)
	if len(files) != 0 {
		t.Fatal(files)
	}
	data, err := encode(refs[0])
	must(t, err)
	if !strings.Contains(string(data), "files: []") || strings.Contains(string(data), "cache:") || strings.Contains(string(data), "blob:") {
		t.Fatal(string(data))
	}
}

func TestFailedReferenceReadDoesNotPoisonListingIndex(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("hello"))
	s := newStore(t, filepath.Join(root, "store"))
	id, err := s.backup(source, false)
	must(t, err)
	var session Session
	must(t, s.read(id, 'S', &session))
	invalid := filepath.Join(s.backend.LocalRoot(), "sessions", "000-invalid.yaml")
	writeFile(t, invalid, []byte("broken: ["))
	if _, err := s.sessionReferences(); err == nil {
		t.Fatal("expected invalid reference error")
	}
	must(t, os.Remove(invalid))
	must(t, os.Remove(s.object(session.Files[0])))
	// A successful reload must use the intact reference, not a partial index left
	// by the earlier failure and a fallback to the missing metadata object.
	_, files, err := s.listingFiles(id)
	must(t, err)
	if len(files) != 1 || files[0].relative != "file" || files[0].metadata.Size != 5 {
		t.Fatal(files)
	}
}
