package hobalight

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigEnvironment(t *testing.T) {
	t.Setenv("HOBALIGHT_STORAGE", t.TempDir())
	t.Setenv("HOBALIGHT_DIRECTORIES", "[one, two]")
	config := filepath.Join(t.TempDir(), "config.conf")
	writeFile(t, config, nil)
	c, err := loadConfig(config)
	if err != nil || len(c.Directories) != 2 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestConfigDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOBALIGHT_STORAGE", "")
	t.Setenv("HOBALIGHT_DIRECTORIES", "[.]")
	config := filepath.Join(t.TempDir(), "config.conf")
	writeFile(t, config, nil)
	c, err := loadConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Dir(config))
	if err != nil {
		t.Fatal(err)
	}
	if c.Storage != want || len(c.Directories) != 1 || c.Directories[0] != filepath.Dir(config) {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestConfiguredPathBases(t *testing.T) {
	binaryDirectory := filepath.Join(t.TempDir(), "bin")
	config := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, config, []byte("storage: ../store\ndirectories: [documents, ../pictures]\n"))
	// Remove environment overrides while preserving the calling environment after the test.
	for _, key := range []string{"HOBALIGHT_STORAGE", "HOBALIGHT_DIRECTORIES"} {
		t.Setenv(key, "")
		must(t, os.Unsetenv(key))
	}
	c, err := loadConfigAt(config, binaryDirectory)
	must(t, err)
	if c.Storage != filepath.Join(filepath.Dir(config), "../store") || c.Directories[0] != filepath.Join(filepath.Dir(config), "documents") || c.Directories[1] != filepath.Join(filepath.Dir(config), "../pictures") {
		t.Fatalf("%+v", c)
	}
	t.Setenv("HOBALIGHT_STORAGE", "environment-store")
	t.Setenv("HOBALIGHT_DIRECTORIES", "[environment-source]")
	c, err = loadConfigAt(config, binaryDirectory)
	must(t, err)
	if c.Storage != filepath.Join(filepath.Dir(config), "environment-store") || c.Directories[0] != filepath.Join(filepath.Dir(config), "environment-source") {
		t.Fatalf("%+v", c)
	}
}

func TestConfigWorkers(t *testing.T) {
	config := filepath.Join(t.TempDir(), "config.yaml")
	binaryDirectory := t.TempDir()
	writeFile(t, config, nil)
	for _, value := range []string{"0", "-1", "no", ""} {
		t.Setenv("HOBALIGHT_WORKERS", value)
		if _, err := loadConfigAt(config, binaryDirectory); err == nil {
			t.Fatalf("accepted workers %q", value)
		}
	}
	t.Setenv("HOBALIGHT_WORKERS", "7")
	writeFile(t, config, []byte("workers: 2\n"))
	c, err := loadConfigAt(config, binaryDirectory)
	must(t, err)
	if c.Workers != 7 {
		t.Fatal(c.Workers)
	}
	must(t, os.Unsetenv("HOBALIGHT_WORKERS"))
	c, err = loadConfigAt(config, binaryDirectory)
	must(t, err)
	if c.Workers != 2 {
		t.Fatal(c.Workers)
	}
	writeFile(t, config, []byte("workers: 0\n"))
	if _, err = loadConfigAt(config, binaryDirectory); err == nil {
		t.Fatal("accepted zero workers")
	}
}

func TestStorageRelativeToSelectedConfig(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	must(t, err)
	must(t, os.Chdir(root))
	root, err = os.Getwd()
	must(t, err)
	t.Cleanup(func() { must(t, os.Chdir(previous)) })
	for _, key := range []string{"HOBALIGHT_STORAGE", "HOBALIGHT_DIRECTORIES", "HOBALIGHT_WORKERS"} {
		t.Setenv(key, "")
		must(t, os.Unsetenv(key))
	}
	config := filepath.Join("settings", "custom.conf")
	binaryDirectory := filepath.Join(root, "bin")
	for _, content := range []string{"", "storage: .\n", "storage: ''\n"} {
		writeFile(t, config, []byte(content))
		c, err := loadConfigAt(config, binaryDirectory)
		must(t, err)
		if c.Storage != filepath.Join(root, "settings") {
			t.Fatalf("storage for %q: %s", content, c.Storage)
		}
	}
	t.Setenv("HOBALIGHT_STORAGE", "../backups")
	c, err := loadConfigAt(config, binaryDirectory)
	must(t, err)
	if c.Storage != filepath.Join(root, "backups") {
		t.Fatal(c.Storage)
	}
	absolute := t.TempDir()
	t.Setenv("HOBALIGHT_STORAGE", absolute)
	c, err = loadConfigAt(config, binaryDirectory)
	must(t, err)
	if c.Storage != absolute {
		t.Fatal(c.Storage)
	}
}
