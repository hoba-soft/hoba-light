package hobalight

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

func parseSessionDate(value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil || date.Format(time.DateOnly) != value {
		return time.Time{}, fmt.Errorf("invalid session date %q: use YYYY-MM-DD (local time)", value)
	}
	return date, nil
}
func matchesSessionDate(started, day string) (bool, error) {
	if day == "" {
		return true, nil
	}
	at, err := time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return false, fmt.Errorf("invalid session start time: %w", err)
	}
	return at.In(time.Local).Format(time.DateOnly) == day, nil
}
func filterSessionDates(refs []Reference, day string) ([]Reference, error) {
	if day == "" {
		return refs, nil
	}
	var filtered []Reference
	for _, ref := range refs {
		match, err := matchesSessionDate(ref.Started, day)
		if err != nil {
			return nil, err
		}
		if match {
			filtered = append(filtered, ref)
		}
	}
	return filtered, nil
}
func sessionDateArgs(args []string) ([]string, string, error) {
	var rest []string
	date := ""
	seen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if arg != "--date" && !strings.HasPrefix(arg, "--date=") {
			rest = append(rest, arg)
			continue
		}
		if seen {
			return nil, "", errors.New("--date may only be specified once")
		}
		seen = true
		if arg == "--date" {
			i++
			if i >= len(args) {
				return nil, "", errors.New("--date requires YYYY-MM-DD")
			}
			date = args[i]
		} else {
			date = strings.TrimPrefix(arg, "--date=")
		}
		if _, err := parseSessionDate(date); err != nil {
			return nil, "", err
		}
	}
	return rest, date, nil
}
