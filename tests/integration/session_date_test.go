package integration

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func retimeSession(t *testing.T, c *cliTest, id, started string) string {
	t.Helper()
	session := c.session(id)
	session.Started = started
	data, err := yaml.Marshal(session)
	must(t, err)
	next := fmt.Sprintf("%xS", sha256.Sum256(data))
	writeFile(t, c.object(next), data)
	c.ok("delete", "--force", id)
	c.ok("refresh")
	return next
}
func TestSessionDateCLIAndCalendar(t *testing.T) {
	c := newCLI(t)
	file := filepath.Join(c.source, "file.txt")
	writeFile(t, file, []byte("older"))
	c.ok("backup", c.source)
	first := retimeSession(t, c, latestRef(c.refs()).Session, cliLocalSessionStart(t, "2024-02-28T23:59:59"))
	writeFile(t, file, []byte("newer contents"))
	c.ok("backup", c.source)
	second := retimeSession(t, c, latestRef(c.refs()).Session, cliLocalSessionStart(t, "2024-02-29T00:00:00"))
	for _, command := range [][]string{
		{"list", "--date", "2024-02-28"},
		{"find", "--date=2024-02-28", "file"},
		{"history", "--date", "2024-02-28", file},
	} {
		out := c.ok(command...)
		if !strings.Contains(out, first[:8]) || strings.Contains(out, second[:8]) {
			t.Fatalf("date filter failed: %s", out)
		}
	}
	c.fails("outside the selected date", "list", "--date", "2024-02-28", second)
	c.fails("use YYYY-MM-DD", "list", "--date", "2024-02-30")
	c.fails("requires YYYY-MM-DD", "find", "--date")
	// Applying the next day from the calendar selects only the leap-day session.
	out, err := c.commandInput(nil, "D\x1b[C\r\rq", "browse", "--date", "2024-02-28")
	must(t, err)
	requireOutput(t, out, "Select session date (local time)", second[:8])
	out, err = c.commandInput(nil, "Dc\rq", "browse", "--date", "2024-02-28")
	must(t, err)
	requireOutput(t, out, "Date filter cleared", first[:8], second[:8])
	// Date selection does not change or delete any stored sessions.
	c.ok("verify")
	requireOutput(t, c.ok("list"), first[:8], second[:8])
}
