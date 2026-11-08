package hobalight

import (
	"strings"
	"testing"
	"time"
)

func TestLocalSessionDaysAcrossDST(t *testing.T) {
	for _, tc := range []struct{ zone, day, start, end string }{
		{"America/New_York", "2024-03-10", "2024-03-10T05:00:00Z", "2024-03-11T04:00:00Z"},
		{"America/New_York", "2024-11-03", "2024-11-03T04:00:00Z", "2024-11-04T05:00:00Z"},
		{"Europe/Warsaw", "2024-03-31", "2024-03-30T23:00:00Z", "2024-03-31T22:00:00Z"},
		{"Europe/Warsaw", "2024-10-27", "2024-10-26T22:00:00Z", "2024-10-27T23:00:00Z"},
	} {
		t.Run(tc.zone+tc.day, func(t *testing.T) {
			location, err := time.LoadLocation(tc.zone)
			must(t, err)
			previous := time.Local
			time.Local = location
			t.Cleanup(func() { time.Local = previous })
			start, err := time.Parse(time.RFC3339, tc.start)
			must(t, err)
			end, err := time.Parse(time.RFC3339, tc.end)
			must(t, err)
			for _, point := range []struct {
				at   time.Time
				want bool
			}{
				{start.Add(-time.Nanosecond), false}, {start, true}, {end.Add(-time.Nanosecond), true}, {end, false},
			} {
				match, err := matchesSessionDate(timestamp(point.at), tc.day)
				must(t, err)
				if match != point.want {
					t.Fatalf("%s: %v", point.at, match)
				}
			}
			if !strings.HasSuffix(timestamp(start.In(location)), "Z") {
				t.Fatal("storage timestamp stopped using UTC")
			}
			day, err := parseSessionDate(tc.day)
			must(t, err)
			if shiftPickerMonth(day, 0).Format(time.DateOnly) != tc.day {
				t.Fatal("calendar changed civil date")
			}
		})
	}
}

func TestLocalDisplayDisambiguatesRepeatedHour(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	must(t, err)
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })
	first := displayTimestamp("2024-11-03T05:30:00Z")
	second := displayTimestamp("2024-11-03T06:30:00Z")
	if first != "2024-11-03T01:30:00-04:00" || second != "2024-11-03T01:30:00-05:00" {
		t.Fatal(first, second)
	}
	if got := sessionTime(Reference{Started: "2024-11-03T06:30:00Z"}); got != "2024-11-03 01:30:00 -05:00" {
		t.Fatal(got)
	}
	created := "2024-11-03T05:30:00Z"
	f := sessionFile{id: strings.Repeat("a", 64) + "F", metadata: File{Identity: Identity{Path: "/file", Created: &created, Modified: "2024-11-03T06:30:00Z"}}}
	details := strings.Join(fileDetails(f, 120), "\n")
	if !strings.Contains(details, first) || !strings.Contains(details, second) {
		t.Fatal(details)
	}
}
