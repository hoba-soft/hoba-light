package hobalight

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

func TestCalendarPreviewUsesLocalDayAndRootCounts(t *testing.T) {
	location, err := time.LoadLocation("Europe/Warsaw")
	must(t, err)
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	refs := []Reference{
		{Directory: "/data/Before", Started: "2024-03-30T22:59:59Z"},
		{Directory: "/data/Docs", Started: "2024-03-30T23:00:00Z"},
		{Directory: "/data/Docs", Started: "2024-03-31T21:59:59Z"},
		{Directory: "/data/Photos", Started: "2024-03-31T10:00:00Z"},
		{Directory: "/data/After", Started: "2024-03-31T22:00:00Z"},
	}
	grouped, err := groupCalendarReferences(refs)
	must(t, err)
	day, _ := parseSessionDate("2024-03-31")
	u := browserUI{datePicker: &day, dateReferences: grouped}
	view := u.view(100, 30)
	if !strings.Contains(view, "Docs") || !strings.Contains(view, "Photos") || strings.Contains(view, "Before") || strings.Contains(view, "After") {
		t.Fatal(view)
	}
	var heading string
	for _, row := range strings.Split(view, "\n") {
		if strings.Contains(row, "Sessions") {
			heading = row
		}
		if strings.Contains(row, "Docs") {
			assertBrowserColumn(t, heading, row, "Sessions", "2")
		}
		if strings.Contains(row, "Photos") {
			assertBrowserColumn(t, heading, row, "Sessions", "1")
		}
	}
	if strings.Contains(view, "│>") || strings.Count(view, "/data/") != 1 {
		t.Fatal("preview has selection or repeats shared path", view)
	}
	u.datePickerKey("right")
	view = u.view(100, 30)
	if !strings.Contains(view, "After") || strings.Contains(view, "Photos") {
		t.Fatal("preview did not update before Enter", view)
	}
	u.datePickerKey("right")
	if !strings.Contains(u.view(100, 24), "No backups on this date.") {
		t.Fatal("empty day not explained")
	}
}

type calendarReadCounter struct {
	storageBackend
	reads int
}

func (s *calendarReadCounter) Read(key string) ([]byte, error) {
	s.reads++
	return s.storageBackend.Read(key)
}

func TestCalendarPreviewLoadsAllReferencesOnce(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	refs := []Reference{
		{Session: strings.Repeat("a", 64) + "S", Directory: "/data/Docs", Started: "2024-02-28T12:00:00Z"},
		{Session: strings.Repeat("b", 64) + "S", Directory: "/data/Photos", Started: "2024-02-29T12:00:00Z"},
	}
	for _, ref := range refs {
		data, err := encode(ref)
		must(t, err)
		must(t, writeStorageBytes(s.backend, "sessions/"+ref.Session+".yaml", data, false))
	}
	s.sessionDate = "2024-02-28"
	counter := &calendarReadCounter{storageBackend: s.backend}
	s.backend = counter
	u := browserUI{browser: browser{store: s}, refs: refs[:1], allRefs: refs[:1], source: refs[0].Directory}
	u.openDatePicker()
	if u.datePicker == nil || counter.reads != 2 {
		t.Fatalf("did not load all references: %d %s", counter.reads, u.message)
	}
	total := 0
	for _, list := range u.dateReferences {
		total += len(list)
	}
	if total != 2 {
		t.Fatal("active source/date scope leaked into calendar")
	}
	u.datePickerKey("right")
	u.view(100, 24)
	if !strings.Contains(u.view(100, 24), "Photos") {
		t.Fatal("next day missing outside current source/date filter")
	}
	u.datePickerKey("left")
	u.view(60, 24)
	if counter.reads != 2 {
		t.Fatal("calendar reread storage on navigation")
	}
	u.datePickerKey("cancel")
	if u.dateReferences != nil || s.sessionDate != "2024-02-28" || u.source != refs[0].Directory {
		t.Fatal("cancel changed original scope or retained cache")
	}
}

func TestCalendarPreviewScrollAndGeometry(t *testing.T) {
	day, _ := parseSessionDate("2026-11-08")
	var refs []Reference
	for i := 0; i < 40; i++ {
		refs = append(refs, Reference{Directory: fmt.Sprintf("/data/group%02d/backup", i)})
	}
	for _, width := range []int{60, 100, 140} {
		for _, height := range []int{18, 24, 40} {
			u := browserUI{datePicker: &day, dateReferences: map[string][]Reference{"2026-11-08": refs}}
			check := func() string {
				view := u.view(width, height)
				lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
				if len(lines) >= height {
					t.Fatalf("%dx%d overflow (%d rows):\n%s", width, height, len(lines), view)
				}
				for _, line := range lines {
					if runewidth.StringWidth(line) > width {
						t.Fatalf("row overflow: %q", line)
					}
					if strings.HasPrefix(line, "│") && runewidth.StringWidth(line) != width {
						t.Fatalf("uneven frame: %q", line)
					}
				}
				if strings.Count(view, "┌") != 1 || strings.Count(view, "\n├") != 2 {
					t.Fatal("calendar and roots do not share one divided frame", view)
				}
				return view
			}
			check()
			key, err := readCalendarKey(bufio.NewReader(strings.NewReader("\t")))
			must(t, err)
			u.datePickerKey(key)
			u.datePickerKey("end")
			if view := check(); !strings.Contains(view, "group39/backup") {
				t.Fatal("last root not visible", view)
			}
			if u.datePicker.Format(time.DateOnly) != "2026-11-08" {
				t.Fatal("scroll changed day")
			}
			u.datePickerKey("toggle-date-list")
			u.datePickerKey("down")
			if u.datePicker.Format(time.DateOnly) != "2026-11-15" || u.dateRootOffset != 0 {
				t.Fatal("calendar arrows or preview scroll reset broken")
			}
		}
	}
}
