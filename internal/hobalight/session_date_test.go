package hobalight

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

func TestSessionDateValidation(t *testing.T) {
	previous := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = previous })
	for _, value := range []string{"", "2024-2-01", "2023-02-29", "2024-02-30", "tomorrow"} {
		if _, err := parseSessionDate(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if _, err := parseSessionDate("2024-02-29"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		stamp, day string
		want       bool
	}{
		{"2024-02-29T00:30:00+02:00", "2024-02-28", true},
		{"2024-02-29T00:30:00+02:00", "2024-02-29", false},
		{"2024-02-29T23:59:59.999999999Z", "2024-02-29", true},
		{"2024-03-01T00:00:00Z", "2024-02-29", false},
	} {
		got, err := matchesSessionDate(tc.stamp, tc.day)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %v %v", tc, got, err)
		}
	}
	if _, _, err := sessionDateArgs([]string{"--date", "2024-02-29", "--date=2024-03-01"}); err == nil {
		t.Fatal("duplicate date accepted")
	}
}
func TestCalendarNavigation(t *testing.T) {
	day, _ := parseSessionDate("2024-01-31")
	if got := shiftPickerMonth(day, 1).Format(time.DateOnly); got != "2024-02-29" {
		t.Fatal(got)
	}
	u := browserUI{datePicker: &day}
	u.datePickerKey("pagedown")
	if u.datePicker.Format(time.DateOnly) != "2024-02-29" {
		t.Fatal(u.datePicker)
	}
	u.datePickerKey("right")
	if u.datePicker.Format(time.DateOnly) != "2024-03-01" {
		t.Fatal(u.datePicker)
	}
	if key, err := readCalendarKey(bufio.NewReader(strings.NewReader("\r"))); err != nil || key != "apply-date" {
		t.Fatal(key, err)
	}
	if key, err := readCalendarKey(bufio.NewReader(strings.NewReader("\x1b[C"))); err != nil || key != "right" {
		t.Fatal(key, err)
	}
	u.datePickerKey("cancel")
	if u.datePicker != nil {
		t.Fatal("calendar did not cancel")
	}
}
