package integration

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalDateCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses the system time zone; DST conversion is covered by unit tests")
	}
	c := newCLI(t)
	file := filepath.Join(c.source, "file.txt")
	writeFile(t, file, []byte("previous local day"))
	c.ok("backup", c.source)
	previous := retimeSession(t, c, latestRef(c.refs()).Session, "2024-03-10T04:59:59Z")
	writeFile(t, file, []byte("selected local day"))
	c.ok("backup", c.source)
	selected := retimeSession(t, c, latestRef(c.refs()).Session, "2024-03-10T05:00:00Z")
	env := map[string]string{"TZ": "America/New_York"}
	for _, args := range [][]string{
		{"list", "--date", "2024-03-10"},
		{"find", "--date", "2024-03-10", "file"},
		{"history", "--date", "2024-03-10", file},
	} {
		out, err := c.command(env, args...)
		must(t, err)
		if !strings.Contains(out, selected[:8]) || strings.Contains(out, previous[:8]) {
			t.Fatalf("local filter failed: %s", out)
		}
		if !strings.Contains(out, "-05:00") {
			t.Fatalf("local offset missing: %s", out)
		}
	}
	out, err := c.commandInput(env, "\rq", "browse", "--date", "2024-03-10")
	must(t, err)
	requireOutput(t, out, selected[:8])
	if strings.Contains(out, previous[:8]) {
		t.Fatal("browser included previous local day")
	}
	if !strings.HasSuffix(c.session(selected).Started, "Z") {
		t.Fatal("session storage is not UTC")
	}
}
