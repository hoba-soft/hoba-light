package hobalight

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupAndRestoreProgress(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "one"), []byte("one"))
	writeFile(t, filepath.Join(source, "nested", "two"), []byte("two"))
	s := newStore(t, filepath.Join(source, "storage"))
	var updates []progressUpdate
	s.progress = func(p progressUpdate) { updates = append(updates, p) }
	id, err := s.backup(source, false)
	must(t, err)
	check := func(total int) {
		t.Helper()
		if len(updates) == 0 {
			t.Fatal("no progress")
		}
		last := updates[len(updates)-1]
		if last.done != total || last.total != total || !strings.Contains(last.text(), "100%") {
			t.Fatalf("wrong completion: %+v", last)
		}
		for _, p := range updates {
			if p.total >= 0 && (p.done < 0 || p.done > p.total) {
				t.Fatalf("invalid progress: %+v", p)
			}
		}
	}
	check(2)
	updates = nil
	_, err = s.backup(source, false)
	must(t, err)
	check(2) // reused files count too
	updates = nil
	must(t, s.restoreTo(id[:64], filepath.Join(root, "restore"), "", &bytes.Buffer{}))
	check(2)
	updates = nil
	must(t, s.restoreTo(id[:64], filepath.Join(root, "single"), "one", &bytes.Buffer{}))
	check(1)
}
func TestEmptyBackupProgress(t *testing.T) {
	s := newStore(t, t.TempDir())
	var last progressUpdate
	s.progress = func(p progressUpdate) { last = p }
	_, err := s.backup(t.TempDir(), false)
	must(t, err)
	if last.total != 0 || last.done != 0 || !strings.Contains(last.text(), "100%") {
		t.Fatal(last)
	}
}

func TestProgressPrinterShowsCurrentPathAndPhase(t *testing.T) {
	var output bytes.Buffer
	current := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	print := progressPrinterWithClock(&output, func() time.Time { return current })
	print(progressUpdate{"Backup", 0, -1, "/source", 0, 0})
	current = current.Add(83 * time.Second)
	print(progressUpdate{"Backup", 0, 2, "/source/large.bin", 0, 0})
	print(progressUpdate{"Backup", 2, 2, "/source/last.txt", 0, 0})
	for _, want := range []string{"counting files (0 found) [00:00:00]  /source", "0/2 (0%)  0 B added [00:01:23]  /source/large.bin", "2/2 (100%)  0 B added [00:01:23]"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q: %s", want, output.String())
		}
	}
}

func TestProgressElapsedResetsForNextSource(t *testing.T) {
	var out bytes.Buffer
	current := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	print := progressPrinterWithClock(&out, func() time.Time { return current })
	print(progressUpdate{"Backup", 0, -1, "/first", 0, 0})
	current = current.Add(2 * time.Hour)
	print(progressUpdate{"Backup", 1, 1, "/first/file", 0, 0})
	print(progressUpdate{"Backup", 0, -1, "/second", 0, 0})
	if !strings.Contains(out.String(), "[02:00:00]\n") || !strings.Contains(out.String(), "[00:00:00]  /second") {
		t.Fatal(out.String())
	}
	if got := elapsedText(25*time.Hour + 2*time.Minute + 3*time.Second); got != "25:02:03" {
		t.Fatal(got)
	}
}
