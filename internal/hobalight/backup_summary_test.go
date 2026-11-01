package hobalight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupSummaryChangesAndWrites(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	s := newStore(t, filepath.Join(root, "store"))
	s.workers = 1
	writeFile(t, filepath.Join(source, "a"), []byte("one"))
	writeFile(t, filepath.Join(source, "b"), []byte("two"))
	var first, second, third, forced backupSummary
	id, err := s.backupWithSummary(source, false, &first)
	must(t, err)
	if first.newFiles != 2 || first.writes != 2 || first.reused != 0 || first.logicalBytes != 6 || first.writtenBytes == 0 {
		t.Fatalf("%+v", first)
	}
	_, files, err := s.sessionFiles(id)
	must(t, err)
	var total int64
	for _, f := range files {
		info, e := os.Stat(s.object(f.metadata.Blob))
		must(t, e)
		total += info.Size()
	}
	if first.addedBytes != total {
		t.Fatalf("added %d, actual %d", first.addedBytes, total)
	}
	if first.writtenBytes != total {
		t.Fatalf("written %d, actual %d", first.writtenBytes, total)
	}
	_, err = s.backupWithSummary(source, false, &second)
	must(t, err)
	if second.unchanged != 2 || second.reused != 2 || second.writes != 0 || second.writtenBytes != 0 || second.addedBytes != 0 || second.baseline != id {
		t.Fatalf("%+v", second)
	}
	writeFile(t, filepath.Join(source, "a"), []byte("changed"))
	writeFile(t, filepath.Join(source, "c"), []byte("two"))
	_, err = s.backupWithSummary(source, false, &third)
	must(t, err)
	if third.newFiles != 1 || third.modified != 1 || third.unchanged != 1 || third.reused != 2 || third.writes != 1 || third.logicalBytes != 13 {
		t.Fatalf("%+v", third)
	}
	_, err = s.backupWithSummary(source, true, &forced)
	must(t, err)
	if forced.unchanged != 3 || forced.writes != 3 || forced.reused != 0 || forced.addedBytes != 0 {
		t.Fatalf("%+v", forced)
	}
}
func TestBackupSummaryMetadataChangeAndMissingReferences(t *testing.T) {
	s, sid, fid, _ := verifyFixture(t)
	var f File
	must(t, s.read(fid, 'F', &f))
	source := filepath.Dir(f.Path)
	must(t, os.RemoveAll(filepath.Join(s.backend.LocalRoot(), "sessions")))
	must(t, os.MkdirAll(filepath.Join(s.backend.LocalRoot(), "sessions"), 0700))
	date := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	must(t, os.Chtimes(f.Path, date, date))
	var summary backupSummary
	_, err := s.backupWithSummary(source, false, &summary)
	must(t, err)
	if summary.baseline != sid || summary.modified != 1 || summary.reused != 1 || summary.writes != 0 {
		t.Fatalf("%+v", summary)
	}
}
func TestBackupSummaryFailureAndUnavailableBaseline(t *testing.T) {
	s, sid, fid, bid := verifyFixture(t)
	var f File
	must(t, s.read(fid, 'F', &f))
	source := filepath.Dir(f.Path)
	must(t, os.Remove(s.object(bid)))
	must(t, os.Mkdir(s.object(bid), 0700))
	must(t, os.WriteFile(filepath.Join(s.object(bid), "blocked"), []byte("x"), 0600))
	var summary backupSummary
	id, err := s.backupWithSummary(source, true, &summary)
	if err == nil || id == "" || summary.failed != 1 || summary.writes != 0 || summary.reused != 0 {
		t.Fatalf("%+v %v", summary, err)
	}
	must(t, os.RemoveAll(s.object(bid)))
	must(t, os.WriteFile(s.object(sid), []byte("broken"), 0600))
	_, err = s.backupWithSummary(source, false, &summary)
	must(t, err)
	if summary.comparisonError == nil || summary.unclassified != 1 || summary.newFiles != 0 {
		t.Fatalf("%+v", summary)
	}
	var text strings.Builder
	must(t, summary.print(&text))
	if !strings.Contains(text.String(), "changes unknown") {
		t.Fatal(text.String())
	}
}
func TestBackupSummaryParallelDuplicates(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	s := newStore(t, filepath.Join(root, "store"))
	s.workers = 8
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		writeFile(t, filepath.Join(source, name), []byte("duplicate"))
	}
	var summary backupSummary
	id, err := s.backupWithSummary(source, false, &summary)
	must(t, err)
	if summary.newFiles != 8 || summary.writes+summary.reused != 8 || summary.failed != 0 {
		t.Fatalf("%+v", summary)
	}
	_, files, err := s.sessionFiles(id)
	must(t, err)
	info, err := os.Stat(s.object(files[0].metadata.Blob))
	must(t, err)
	if summary.addedBytes != info.Size() {
		t.Fatalf("duplicate content counted more than once: %+v", summary)
	}
	if summary.writtenBytes != int64(summary.writes)*info.Size() {
		t.Fatalf("%+v", summary)
	}
}

func TestBackupSummaryForcedContentChange(t *testing.T) {
	s, _, fid, _ := verifyFixture(t)
	var f File
	must(t, s.read(fid, 'F', &f))
	info, err := os.Stat(f.Path)
	must(t, err)
	must(t, os.WriteFile(f.Path, []byte("CHANGED"), 0600))
	must(t, os.Chtimes(f.Path, info.ModTime(), info.ModTime()))
	var summary backupSummary
	_, err = s.backupWithSummary(filepath.Dir(f.Path), true, &summary)
	must(t, err)
	if summary.modified != 1 || summary.unchanged != 0 || summary.writes != 1 {
		t.Fatalf("%+v", summary)
	}
}
