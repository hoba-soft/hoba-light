package hobalight

import (
	"strings"
	"testing"
)

func TestCompareSessionMetadata(t *testing.T) {
	created := "2026-10-25T10:00:00Z"
	sameCreated := created
	changedCreated := "2026-10-25T11:00:00Z"
	before := File{Blob: "content"}
	before.Size = 10
	before.Modified = created
	before.Created = &created
	for _, tc := range []struct {
		name   string
		change func(*File)
		want   bool
	}{
		{"content", func(f *File) { f.Blob = "other" }, true},
		{"size", func(f *File) { f.Size++ }, true},
		{"modified", func(f *File) { f.Modified = changedCreated }, true},
		{"created", func(f *File) { f.Created = &changedCreated }, true},
		{"missing created", func(f *File) { f.Created = nil }, true},
		{"equal created values", func(f *File) { f.Created = &sameCreated }, false},
		{"different source root", func(f *File) { f.Path = "/other/file" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			after := before
			tc.change(&after)
			changed := !sameFileMetadata(before, after)
			if changed != tc.want {
				t.Fatalf("metadata change: got %v, want %v", changed, tc.want)
			}
		})
	}
}
func TestSessionListNestedAndInvalidPath(t *testing.T) {
	root := t.TempDir()
	source := root + "/source"
	writeFile(t, source+"/nested/file", []byte("data"))
	s := newStore(t, root+"/storage")
	id, err := s.backup(source, false)
	must(t, err)
	var output strings.Builder
	must(t, s.list([]string{id[:8], "nested"}, &output))
	if !strings.Contains(output.String(), "file  ") || strings.Contains(output.String(), " F ") || strings.Contains(output.String(), " B ") {
		t.Fatal(output.String())
	}
	for _, p := range []string{"../escape", "..", "/absolute", "C:/absolute", "missing"} {
		if err = s.list([]string{id[:8], p}, &strings.Builder{}); err == nil {
			t.Fatalf("accepted %s", p)
		}
	}
}

func TestModificationDetails(t *testing.T) {
	created := "2026-10-25T10:00:00Z"
	before := File{Identity: Identity{Size: 1024, Modified: "old"}, Blob: "a"}
	after := File{Identity: Identity{Size: 2048, Created: &created, Modified: "new"}, Blob: "b"}
	details := strings.Join(modificationDetails(before, after), "\n")
	want := "content: changed\nsize: 1.0 KiB (1024 bytes) -> 2.0 KiB (2048 bytes)\ncreated: unavailable -> " + created + "\nmodified: old -> new"
	if details != want {
		t.Fatalf("%s", details)
	}
	if len(modificationDetails(before, before)) != 0 {
		t.Fatal("unchanged metadata has details")
	}
	// Small size changes must stay visible despite human-readable rounding.
	after = before
	after.Size++
	if got := strings.Join(modificationDetails(before, after), "\n"); got != "size: 1.0 KiB (1024 bytes) -> 1.0 KiB (1025 bytes)" {
		t.Fatal(got)
	}
}
