package hobalight

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupDisplayCompletion(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			var out bytes.Buffer
			d := &backupDisplay{out: &out, source: "/source", terminal: terminal, fd: -1, started: time.Now(), progress: progressUpdate{operation: "Backup", total: -1}}
			d.draw("Backing up: /source", false)
			d.update(progressUpdate{operation: "Backup", total: 2, current: "/source/file"})
			n := 0
			var err error
			if failed {
				n = 1
				err = errors.New("incomplete")
			}
			d.update(progressUpdate{operation: "Backup", done: 2, total: 2, current: "/source/last", errors: n, addedBytes: 2048})
			if strings.Contains(out.String(), "100%") {
				t.Fatal("completion displayed before publication")
			}
			d.finish("123456789abcdefS", err)
			text := out.String()
			header := "Backup finished: /source (12345678)"
			if failed {
				header = "Backup finished with errors: /source (12345678)"
			}
			if !strings.Contains(text, header) || !strings.Contains(text, "2.0 KiB added") || strings.Contains(text, "/source/last") {
				t.Fatal(text)
			}
			if terminal != strings.Contains(text, "\x1b[1A") {
				t.Fatal("wrong cursor behavior", text)
			}
			if failed != strings.Contains(text, "1 error") {
				t.Fatal(text)
			}
		}
	}
}
func TestBackupProgressAddedData(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	s := newStore(t, filepath.Join(root, "store"))
	s.workers = 8
	for _, name := range []string{"a", "b", "c", "d"} {
		writeFile(t, filepath.Join(source, name), []byte("shared content"))
	}
	for _, force := range []bool{false, false, true} {
		var updates []progressUpdate
		s.progress = func(p progressUpdate) { updates = append(updates, p) }
		var summary backupSummary
		_, err := s.backupWithSummary(source, force, &summary)
		must(t, err)
		var last int64
		for _, p := range updates {
			if p.addedBytes < last {
				t.Fatal("added bytes decreased")
			}
			last = p.addedBytes
		}
		if last != summary.addedBytes {
			t.Fatalf("progress %d summary %d", last, summary.addedBytes)
		}
	}
}
