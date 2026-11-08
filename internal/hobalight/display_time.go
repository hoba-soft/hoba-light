package hobalight

import "time"

// Storage keeps UTC timestamps. Conversion happens only at the display boundary.
func displayTimestamp(value string) string {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return at.In(time.Local).Format(time.RFC3339Nano)
}

// A calendar value is a civil date, not a timestamp. Use UTC for arithmetic
// on its fields so DST never skips or repeats a calendar cell.
func localCalendarDay() time.Time {
	day, _ := parseSessionDate(time.Now().In(time.Local).Format(time.DateOnly))
	return day
}
