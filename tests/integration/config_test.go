package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIntegrationConfiguration(t *testing.T) {
	c := newCLI(t)
	other := filepath.Join(c.root, "other")
	makeDir(t, other)
	writeFile(t, filepath.Join(c.source, "one"), []byte("one"))
	writeFile(t, filepath.Join(other, "two"), []byte("two"))
	config := filepath.Join(c.root, "custom.conf")
	yamlFile(t, config, Config{Storage: filepath.Join(c.root, "ignored-storage"), Directories: []string{c.source, other}})
	c.ok("--config", config, "backup")
	if len(c.refs()) != 2 {
		t.Fatal("expected one session per configured directory")
	}
	if _, err := os.Stat(filepath.Join(c.root, "ignored-storage")); !os.IsNotExist(err) {
		t.Fatal("environment did not override storage")
	}
	c.ok("--config", config, "backup", other)
	if len(c.refs()) != 3 {
		t.Fatal("explicit source did not override configured sources")
	}
	list, err := yaml.Marshal([]string{c.source})
	must(t, err)
	out, err := c.command(map[string]string{"HOBALIGHT_CONFIG": config, "HOBALIGHT_DIRECTORIES": string(list)}, "backup")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if len(c.refs()) != 4 {
		t.Fatal("environment list did not override configured list")
	}
	yamlFile(t, filepath.Join(c.root, "hobalight.conf"), Config{Directories: []string{other}})
	c.ok("--config", filepath.Join(c.root, "hobalight.conf"), "backup")
	if len(c.refs()) != 5 {
		t.Fatal("explicit config was not loaded")
	}
}

func TestIntegrationInvalidConfiguration(t *testing.T) {
	for _, value := range []string{"[", "storage: [not, a, string]"} {
		t.Run(value, func(t *testing.T) {
			c := newCLI(t)
			writeFile(t, filepath.Join(c.root, "hobalight.conf"), []byte(value))
			c.fails("yaml:", "--config", filepath.Join(c.root, "hobalight.conf"), "backup", c.source)
		})
	}
	c := newCLI(t)
	out, err := c.command(map[string]string{"HOBALIGHT_DIRECTORIES": "not-a-list"}, "backup")
	if err == nil || !strings.Contains(out, "must be a YAML list") {
		t.Fatalf("invalid environment accepted: %v %s", err, out)
	}
	home := filepath.Join(c.root, "home")
	makeDir(t, home)
	out, err = c.command(map[string]string{"HOBALIGHT_STORAGE": "", "HOME": home, "USERPROFILE": home}, "list")
	if err != nil {
		t.Fatalf("empty environment did not use default storage: %v %s", err, out)
	}
	if _, err = os.Stat(filepath.Join(home, ".hobalight", "store.yaml")); err != nil {
		t.Fatal(err)
	}

}

func TestIntegrationStorageCreatedAutomatically(t *testing.T) {
	c := newCLI(t)
	writeFile(t, filepath.Join(c.source, "file"), []byte("x"))
	for _, storage := range []string{filepath.Join(c.root, "missing", "store"), filepath.Join(c.root, "empty")} {
		local := *c
		local.storage = storage
		local.ok("backup", c.source)
		if _, err := os.Stat(filepath.Join(storage, "store.yaml")); err != nil {
			t.Fatal(err)
		}
		local.ok("list")
	}
	other := *c
	other.storage = c.source
	other.fails("not empty", "list")
}

func TestIntegrationDefaultsWithoutConfig(t *testing.T) {
	c := newCLI(t)
	must(t, os.RemoveAll(c.storage))
	// Empty storage override selects the default. No directory override or config.
	home := filepath.Join(c.root, "home")
	makeDir(t, home)
	out, err := c.command(map[string]string{"HOBALIGHT_STORAGE": "", "HOME": home, "USERPROFILE": home}, "list")
	if err != nil {
		t.Fatalf("automatic creation defaults: %v %s", err, out)
	}
	c.storage = filepath.Join(home, ".hobalight")
	config := readFile(t, filepath.Join(home, ".hobalight", "hobalight.conf"))
	var defaults map[string]any
	must(t, yaml.Unmarshal(config, &defaults))
	for _, key := range []string{"directories", "exclude"} {
		list, ok := defaults[key].([]any)
		if !ok || len(list) != 0 {
			t.Fatalf("default %s must be an explicit empty list: %s", key, config)
		}
	}
	example := readFile(t, filepath.Join("..", "..", "configs", "hobalight.conf.example"))
	if string(config) != string(example) {
		t.Fatalf("generated config differs from documented example: %s", config)
	}
	c.fails("no backup sources configured", "backup")
	writeFile(t, filepath.Join(c.source, "note.txt"), []byte("explicit source"))
	c.ok("backup", c.source)
	refs := c.refs()
	if len(refs) != 1 {
		t.Fatalf("expected one session: %+v", refs)
	}
	dest := filepath.Join(c.root, "restore")
	c.ok("restore-to", refs[0].Session[:8], ".", dest)
	assertTree(t, dest, map[string][]byte{"note.txt": []byte("explicit source")})
}

func TestIntegrationConfigBesideExecutable(t *testing.T) {
	c := newCLI(t)
	c.binary = filepath.Join(c.root, "bin", filepath.Base(testBinary))
	writeFile(t, c.binary, readFile(t, testBinary))
	must(t, os.Chmod(c.binary, 0700))
	yamlFile(t, filepath.Join(filepath.Dir(c.binary), "hobalight.conf"), Config{Directories: []string{c.source}})
	// A config in the working directory must not override the adjacent config.
	writeFile(t, filepath.Join(c.root, "hobalight.conf"), []byte("invalid: ["))
	writeFile(t, filepath.Join(c.source, "file"), []byte("data"))
	c.ok("backup")
	refs := c.refs()
	canonical, err := filepath.Abs(c.source)
	must(t, err)
	if len(refs) != 1 || refs[0].Directory != filepath.ToSlash(canonical) {
		t.Fatalf("adjacent config not used: %+v", refs)
	}
	// Both flag and environment config paths resolve from the working directory.
	yamlFile(t, filepath.Join(c.root, "override.conf"), Config{Directories: []string{c.source}})
	// A file beside the executable must not shadow the working-directory file.
	writeFile(t, filepath.Join(filepath.Dir(c.binary), "override.conf"), []byte("invalid: ["))
	c.ok("--config", "override.conf", "backup")
	out, err := c.command(map[string]string{"HOBALIGHT_CONFIG": "override.conf"}, "backup")
	if err != nil {
		t.Fatalf("environment override: %v %s", err, out)
	}
	if len(c.refs()) != 3 {
		t.Fatal("config overrides not used")
	}
}

func TestIntegrationConfigFlagOverridesEnvironment(t *testing.T) {
	for _, equalForm := range []bool{false, true} {
		name := "separate"
		if equalForm {
			name = "equals"
		}
		t.Run(name, func(t *testing.T) {
			c := newCLI(t)
			writeFile(t, filepath.Join(c.source, "file"), []byte("data"))
			config := filepath.Join(c.root, "custom config.conf")
			yamlFile(t, config, Config{Directories: []string{c.source}})
			envConfig := filepath.Join(c.root, "environment.conf")
			writeFile(t, envConfig, []byte("invalid: ["))
			args := []string{"--config", "custom config.conf", "backup"}
			if equalForm {
				args = []string{"--config=custom config.conf", "backup"}
			}
			out, err := c.command(map[string]string{"HOBALIGHT_CONFIG": envConfig}, args...)
			if err != nil {
				t.Fatalf("flag did not override environment: %v %s", err, out)
			}
			refs := c.refs()
			if len(refs) != 1 || refs[0].Directory != filepath.ToSlash(c.source) {
				t.Fatalf("flag config not used: %+v", refs)
			}
		})
	}
}

func TestIntegrationRelativePathsAndCLIOverrides(t *testing.T) {
	c := newCLI(t)
	c.binary = filepath.Join(c.root, "bin", filepath.Base(testBinary))
	writeFile(t, c.binary, readFile(t, testBinary))
	must(t, os.Chmod(c.binary, 0700))
	source := filepath.Join(c.root, "bin", "source")
	writeFile(t, filepath.Join(source, "file"), []byte("configured"))
	yamlFile(t, filepath.Join(c.root, "bin", "hobalight.conf"), Config{Storage: "store", Directories: []string{"source"}})
	c.storage = filepath.Join(c.root, "bin", "store")
	out, err := c.command(map[string]string{"HOBALIGHT_STORAGE": "store"}, "backup")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if len(c.refs()) != 1 || c.refs()[0].Directory != filepath.ToSlash(source) {
		t.Fatalf("wrong source: %+v", c.refs())
	}
	out, err = c.command(map[string]string{"HOBALIGHT_STORAGE": "store", "HOBALIGHT_DIRECTORIES": "[source]"}, "backup")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	writeFile(t, filepath.Join(c.source, "file"), []byte("CLI"))
	c.ok("backup", "source")
	id := latestRef(c.refs()).Session
	c.ok("restore-to", id[:8], ".", "restored")
	assertTree(t, filepath.Join(c.root, "restored"), map[string][]byte{"file": []byte("CLI")})
}

func TestIntegrationStorageBesideBinary(t *testing.T) {
	c := newCLI(t)
	c.binary = filepath.Join(c.root, "portable", filepath.Base(testBinary))
	writeFile(t, c.binary, readFile(t, testBinary))
	must(t, os.Chmod(c.binary, 0700))
	c.storage = filepath.Dir(c.binary)
	yamlFile(t, filepath.Join(c.storage, "hobalight.conf"), Config{Storage: "."})
	writeFile(t, filepath.Join(c.source, "file"), []byte("portable backup"))
	c.ok("backup", c.source)
	if len(c.refs()) != 1 {
		t.Fatal("portable storage missing session")
	}
	c.ok("restore-to", c.refs()[0].Session[:8], ".", "restored")
	assertTree(t, filepath.Join(c.root, "restored"), map[string][]byte{"file": []byte("portable backup")})
	c.ok("list")
}

func TestIntegrationExcludesAcrossSources(t *testing.T) {
	c := newCLI(t)
	other := filepath.Join(c.root, "other")
	for _, source := range []string{c.source, other} {
		for _, name := range []string{"keep.txt", "nested/keep.txt", "nested/debug.log", "cache/secret.txt", "nested/cache/secret.txt", "root-only/secret.txt", "nested/root-only/keep.txt"} {
			writeFile(t, filepath.Join(source, filepath.FromSlash(name)), []byte(name))
		}
	}
	config := filepath.Join(c.root, "exclude.conf")
	yamlFile(t, config, Config{Directories: []string{c.source, other}, Exclude: []string{"*.log", "cache/", "/root-only/"}})
	c.ok("--config", config, "backup")
	refs := c.refs()
	if len(refs) != 2 {
		t.Fatalf("expected two sessions: %+v", refs)
	}
	for i, ref := range refs {
		destination := filepath.Join(c.root, "restore", string(rune('a'+i)))
		c.ok("restore-to", ref.Session, ".", destination)
		assertTree(t, destination, map[string][]byte{
			"keep.txt":                  []byte("keep.txt"),
			"nested/keep.txt":           []byte("nested/keep.txt"),
			"nested/root-only/keep.txt": []byte("nested/root-only/keep.txt"),
		})
	}
	// The same rules apply when the source is supplied explicitly and with --force.
	c.ok("--config", config, "backup", "--force", c.source)
	if len(c.session(latestRef(c.refs()).Session).Files) != 3 {
		t.Fatal("explicit source ignored excludes")
	}
	writeFile(t, config, []byte("exclude: ['!keep']\n"))
	c.fails("invalid exclude pattern", "--config", config, "backup")
}

func TestIntegrationHomeConfigAndMissingExplicitConfig(t *testing.T) {
	c := newCLI(t)
	config := filepath.Join(c.root, "home", ".hobalight", "hobalight.conf")
	yamlFile(t, config, Config{Directories: []string{c.source}})
	original := readFile(t, config)
	writeFile(t, filepath.Join(c.source, "file"), []byte("home config"))
	c.ok("backup")
	if len(c.refs()) != 1 {
		t.Fatal("home config not used")
	}
	if string(readFile(t, config)) != string(original) {
		t.Fatal("existing home config overwritten")
	}
	for _, useFlag := range []bool{false, true} {
		args := []string{"list"}
		env := map[string]string{"HOBALIGHT_CONFIG": "missing.conf"}
		if useFlag {
			args = []string{"--config", "missing.conf", "list"}
			env = nil
		}
		out, err := c.command(env, args...)
		if err == nil || !strings.Contains(out, "missing.conf") {
			t.Fatalf("missing explicit config accepted: %v %s", err, out)
		}
	}
	c.fails("config path must not be empty", "--config=", "list")
	writeFile(t, config, []byte("invalid: ["))
	c.fails("yaml:", "list")
	// An adjacent config takes precedence even when the home config is invalid.
	c.binary = filepath.Join(c.root, "bin", filepath.Base(testBinary))
	writeFile(t, c.binary, readFile(t, testBinary))
	must(t, os.Chmod(c.binary, 0700))
	writeFile(t, filepath.Join(filepath.Dir(c.binary), "hobalight.conf"), []byte("workers: 2\n"))
	c.ok("list")
}

func TestIntegrationExcludeOverrides(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   string
		flags []string
		want  int
	}{
		{"environment replaces config", "['*.log']", nil, 3},
		{"flags replace environment", "['*.log']", []string{"--exclude", "*.txt", "--exclude=cache/"}, 2},
		{"empty environment list", "[]", nil, 4},
		{"empty flag", "['*.log']", []string{"--exclude="}, 4},
		{"reset flags", "[]", []string{"--exclude=*.txt", "--exclude=", "--exclude=cache/"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCLI(t)
			for _, name := range []string{"a.log", "b.txt", "cache/data", "keep"} {
				writeFile(t, filepath.Join(c.source, filepath.FromSlash(name)), []byte(name))
			}
			config := filepath.Join(c.root, "custom.conf")
			yamlFile(t, config, Config{Exclude: []string{"keep"}})
			args := append([]string{"--config", config}, tc.flags...)
			args = append(args, "backup", c.source)
			out, err := c.command(map[string]string{"HOBALIGHT_EXCLUDE": tc.env}, args...)
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if got := len(c.session(latestRef(c.refs()).Session).Files); got != tc.want {
				t.Fatalf("got %d files, want %d", got, tc.want)
			}
		})
	}
	c := newCLI(t)
	for _, value := range []string{"not-a-list", "[", "['!keep']"} {
		if out, err := c.command(map[string]string{"HOBALIGHT_EXCLUDE": value}, "backup", c.source); err == nil {
			t.Fatalf("accepted %q: %s", value, out)
		}
	}
	c.fails("invalid exclude pattern", "--exclude=!keep", "backup", c.source)
}

func TestIntegrationSourcesRelativeToExternalConfig(t *testing.T) {
	c := newCLI(t)
	config := filepath.Join(c.root, "settings", "custom.conf")
	source := filepath.Join(c.root, "settings", "documents")
	writeFile(t, filepath.Join(source, "file"), []byte("configured"))
	source, err := filepath.EvalSymlinks(source)
	must(t, err)
	c.source, err = filepath.EvalSymlinks(c.source)
	must(t, err)
	yamlFile(t, config, Config{Directories: []string{"documents"}})
	c.ok("--config", filepath.Join("settings", "custom.conf"), "backup")
	assertSameDirectory(t, latestRef(c.refs()).Directory, source)
	out, err := c.command(map[string]string{
		"HOBALIGHT_CONFIG":      filepath.Join("settings", "custom.conf"),
		"HOBALIGHT_DIRECTORIES": "[documents]",
	}, "backup")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	assertSameDirectory(t, latestRef(c.refs()).Directory, source)
	writeFile(t, filepath.Join(c.source, "file"), []byte("CLI"))
	c.ok("--config", config, "backup", "source")
	assertSameDirectory(t, latestRef(c.refs()).Directory, c.source)
}

func TestIntegrationHelpAndEffectiveSettings(t *testing.T) {
	c := newCLI(t)
	c.storage = filepath.Join(c.root, "not-created")
	config := filepath.Join(c.root, "settings.conf")
	writeFile(t, config, []byte("workers: 2\nexclude: ['*.tmp']\n"))
	out := c.ok("--config", config, "--exclude=*.log", "info")
	for _, want := range []string{"Effective settings:", "settings.conf", "(HOBALIGHT_STORAGE)", "directories: [] (default)", "workers: 2 (config)", "exclude: [\"*.log\"] (--exclude)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if _, err := os.Stat(c.storage); !os.IsNotExist(err) {
		t.Fatalf("settings touched storage: %v", err)
	}
	c.ok("--config", filepath.Join(c.root, "missing.conf"), "--help")
	c.fails("usage: browse", "browse", "extra")
	// Help does not load or generate configuration.
	home := filepath.Join(c.root, "fresh-home")
	out, err := c.command(map[string]string{"HOME": home, "USERPROFILE": home, "HOBALIGHT_STORAGE": ""})
	must(t, err)
	if !strings.Contains(out, "Usage:") || strings.Contains(out, "Effective settings:") {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(home, ".hobalight")); !os.IsNotExist(err) {
		t.Fatalf("help created config: %v", err)
	}

}

func TestLongOptionsOnly(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{{"-h"}, {"--h"}, {"-version"}, {"-config", "unused"}, {"backup", "-f", c.source}, {"delete", "-f", "12345678"}, {"gc", "-v"}} {
		if out, err := c.command(nil, args...); err == nil {
			t.Fatalf("accepted %v: %s", args, out)
		}
	}
	requireOutput(t, c.ok("--help"), "--config", "--version", "--force", "--verbose")
	c.ok("--version")
}

// Windows can expose a directory through both its long and 8.3 names.
func assertSameDirectory(t *testing.T, got, want string) {
	t.Helper()
	if got != filepath.ToSlash(got) {
		t.Fatalf("session path is not slash-separated: %q", got)
	}
	actual, err := os.Stat(filepath.FromSlash(got))
	must(t, err)
	expected, err := os.Stat(want)
	must(t, err)
	if !os.SameFile(actual, expected) {
		t.Fatalf("session directory %q differs from %q", got, want)
	}
}
