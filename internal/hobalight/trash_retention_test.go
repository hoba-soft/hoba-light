package hobalight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrashRetention(t *testing.T) {
	s := newStore(t, t.TempDir())
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	id := strings.Repeat("a", 64) + "B"
	dir := filepath.Join(s.backend.LocalRoot(), "trash")
	name := func(days int) string {
		return id + "-trashed-" + now.AddDate(0, 0, -days).Format(trashTimeFormat) + "-1.orphan"
	}
	for _, n := range []string{name(31), name(30), name(1), id + "-legacy.orphan", "unknown.txt"} {
		writeFile(t, filepath.Join(dir, n), []byte("data"))
	}
	n, err := s.expireTrash(now)
	must(t, err)
	if n != 1 {
		t.Fatal(n)
	}
	if _, err := os.Stat(filepath.Join(dir, name(31))); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, n := range []string{name(30), name(1), "unknown.txt", id + "-trashed-" + now.Format(trashTimeFormat) + "-legacy.orphan"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	s.trashRetentionDays = 2
	n, err = s.expireTrash(now)
	must(t, err)
	if n != 1 {
		t.Fatal(n)
	}
}

func TestTrashRemovalDateIgnoresObjectAge(t *testing.T) {
	s := newStore(t, t.TempDir())
	id := strings.Repeat("a", 64) + "B"
	writeFile(t, s.object(id), []byte("old"))
	old := time.Now().AddDate(-1, 0, 0)
	must(t, os.Chtimes(s.object(id), old, old))
	must(t, s.trashObject(id, "orphan"))
	removed, err := s.expireTrash(time.Now())
	must(t, err)
	if removed != 0 {
		t.Fatal("newly trashed old object deleted")
	}
}

func TestGeneratedConfigTrashRetention(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	name, err := defaultConfigPath(t.TempDir())
	must(t, err)
	data, err := os.ReadFile(name)
	must(t, err)
	if !strings.Contains(string(data), "trash_retention_days: 30") {
		t.Fatal(string(data))
	}
	c, err := loadConfigAt(name, t.TempDir())
	must(t, err)
	if c.TrashRetentionDays != 30 {
		t.Fatal(c.TrashRetentionDays)
	}
}
