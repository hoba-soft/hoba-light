package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hoba-soft/hoba-light/internal/hobalight"
	"gopkg.in/yaml.v3"
)

type Config = hobalight.Config
type Session = hobalight.Session
type Reference = hobalight.Reference
type File = hobalight.File

type cliTest struct {
	t                     *testing.T
	root, storage, source string
	binary                string
}

var testBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hobalight-test-binary-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testBinary = filepath.Join(dir, "hobalight")
	if runtime.GOOS == "windows" {
		testBinary += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	buildArgs := []string{"build"}
	if testRaceEnabled {
		buildArgs = append(buildArgs, "-race")
		// Instrument the child CLI too, without a one-second delay per invocation.
		if err := os.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.RemoveAll(dir)
			os.Exit(1)
		}
	}
	buildArgs = append(buildArgs, "-o", testBinary, "../../cmd/hobalight")
	cmd := exec.CommandContext(ctx, "go", buildArgs...)
	output, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build test binary: %v\n%s", err, output)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func makeDir(t *testing.T, p string) { t.Helper(); must(t, os.MkdirAll(p, 0700)) }

func writeFile(t *testing.T, p string, b []byte) {
	t.Helper()
	makeDir(t, filepath.Dir(p))
	must(t, os.WriteFile(p, b, 0600))
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	must(t, err)
	return b
}

func decodeFile(t *testing.T, p string, out any) {
	t.Helper()
	must(t, yaml.Unmarshal(readFile(t, p), out))
}

func yamlFile(t *testing.T, p string, v any) {
	t.Helper()
	b, err := yaml.Marshal(v)
	must(t, err)
	writeFile(t, p, b)
}

func newCLI(t *testing.T) *cliTest {
	t.Helper()
	root := t.TempDir()
	c := &cliTest{t: t, root: root, storage: filepath.Join(root, "storage"), source: filepath.Join(root, "source")}
	makeDir(t, c.source)
	c.ok("list")
	return c
}

func latestRef(refs []Reference) Reference {
	latest := refs[0]
	for _, r := range refs[1:] {
		a, _ := time.Parse(time.RFC3339Nano, r.Started)
		b, _ := time.Parse(time.RFC3339Nano, latest.Started)
		if a.After(b) {
			latest = r
		}
	}
	return latest
}

func (c *cliTest) object(id string) string { return filepath.Join(c.storage, "objects", id[:2], id) }

func (c *cliTest) command(env map[string]string, args ...string) (string, error) {
	return c.commandInput(env, "", args...)
}

func (c *cliTest) commandInput(env map[string]string, input string, args ...string) (string, error) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	binary := c.binary
	if binary == "" {
		binary = testBinary
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = c.root
	cmd.Stdin = strings.NewReader(input)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "HOBALIGHT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TZ=UTC", "HOBALIGHT_STORAGE="+c.storage, "HOME="+filepath.Join(c.root, "home"), "USERPROFILE="+filepath.Join(c.root, "home"))
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		c.t.Fatalf("CLI timed out: %v", args)
	}
	return string(output), err
}

func (c *cliTest) ok(args ...string) string {
	c.t.Helper()
	out, err := c.command(nil, args...)
	if err != nil {
		c.t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return out
}

func (c *cliTest) fails(want string, args ...string) {
	c.t.Helper()
	c.failsCode(1, want, args...)
}

func (c *cliTest) failsCode(code int, want string, args ...string) {
	c.t.Helper()
	out, err := c.command(nil, args...)
	var exit *exec.ExitError
	if err == nil {
		c.t.Fatalf("%v unexpectedly succeeded: %s", args, out)
	}
	if !strings.Contains(out, want) {
		c.t.Fatalf("%v: expected %q; got %v: %s", args, want, err, out)
	}
	if !errors.As(err, &exit) || exit.ExitCode() != code {
		c.t.Fatalf("expected exit %d, got %v", code, err)
	}
}

func (c *cliTest) refs() []Reference {
	c.t.Helper()
	entries, err := os.ReadDir(filepath.Join(c.storage, "sessions"))
	must(c.t, err)
	var refs []Reference
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			var ref Reference
			decodeFile(c.t, filepath.Join(c.storage, "sessions", e.Name()), &ref)
			refs = append(refs, ref)
		}
	}
	// Filenames have second precision; use full timestamps to identify the latest session.
	sort.Slice(refs, func(i, j int) bool {
		a, err := time.Parse(time.RFC3339Nano, refs[i].Started)
		must(c.t, err)
		b, err := time.Parse(time.RFC3339Nano, refs[j].Started)
		must(c.t, err)
		return a.Before(b)
	})
	return refs
}

func (c *cliTest) session(id string) Session {
	c.t.Helper()
	var s Session
	decodeFile(c.t, c.object(id), &s)
	return s
}

func (c *cliTest) ids(kind byte) []string {
	c.t.Helper()
	var ids []string
	must(c.t, filepath.WalkDir(filepath.Join(c.storage, "objects"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") {
			c.t.Fatalf("temporary object leaked: %s", p)
		}
		if filepath.Base(filepath.Dir(p)) != d.Name()[:2] {
			c.t.Fatalf("object outside its bucket: %s", p)
		}
		if strings.HasSuffix(d.Name(), string(kind)) {
			ids = append(ids, d.Name())
		}
		return nil
	}))
	return ids
}

func assertTree(t *testing.T, root string, want map[string][]byte) {
	t.Helper()
	got := map[string][]byte{}
	must(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = readFile(t, p)
		return nil
	}))
	if len(got) != len(want) {
		t.Fatalf("restored %d files, expected %d: %v", len(got), len(want), got)
	}
	for p, b := range want {
		if !bytes.Equal(got[p], b) {
			t.Fatalf("content mismatch: %s", p)
		}
	}
}

// Unix CLI fixtures explicitly use TZ=UTC. Windows ignores TZ and uses the
// system zone, so assertions must follow the native runner's actual zone.
func cliTimeZone() *time.Location {
	if runtime.GOOS == "windows" {
		return time.Local
	}
	return time.UTC
}
func cliTimestamp(t *testing.T, value string) string {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, value)
	must(t, err)
	return at.In(cliTimeZone()).Format(time.RFC3339Nano)
}
func cliLocalSessionStart(t *testing.T, value string) string {
	t.Helper()
	at, err := time.ParseInLocation("2006-01-02T15:04:05", value, cliTimeZone())
	must(t, err)
	return at.UTC().Format(time.RFC3339Nano)
}
