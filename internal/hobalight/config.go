package hobalight

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func loadConfig(name string) (Config, error) {
	executable, err := os.Executable()
	if err != nil {
		return Config{}, fmt.Errorf("locate executable: %w", err)
	}
	return loadConfigAt(name, filepath.Dir(executable))
}

// defaultConfigPath prefers the adjacent config, then creates the home config if needed.
func defaultConfigPath(binaryDirectory string) (string, error) {
	name := filepath.Join(binaryDirectory, "hobalight.conf")
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		return name, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name = filepath.Join(home, ".hobalight", "hobalight.conf")
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		return name, err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return name, nil
	}
	if err != nil {
		return "", err
	}
	_, writeErr := fmt.Fprintf(f, `# Backup storage. Relative paths use this configuration file's directory.
# "." stores backups beside this file; absolute paths are also supported.
storage: .

# Source files or directories. Relative paths use this configuration file's directory.
# An empty list requires an explicit source: hl backup <path>.
# Example: directories: ["C:/Users/me/Documents", "./photos"]
directories: []

# Number of files processed concurrently; must be a positive integer.
workers: %d

# Exclusions applied relative to each source root. Use / even on Windows.
# Example: exclude: ["*.log", "cache/", "/temp/"]
# An empty list disables exclusions.
exclude: []

# gc permanently deletes objects after this many days in trash (positive integer).
trash_retention_days: %d

# Applied by gc per source. Keep sessions matching either rule.
# 0 disables a rule; both 0 disable session retention.
retention:
  days: 0
  sessions: 0
`, defaultWorkers, defaultTrashRetentionDays)
	return name, errors.Join(writeErr, f.Close())
}

// Relative storage and source paths use the selected configuration directory.
func loadConfigAt(name, binaryDirectory string) (Config, error) {
	if name == "" {
		var err error
		name, err = defaultConfigPath(binaryDirectory)
		if err != nil {
			return Config{}, err
		}
	}
	name, err := filepath.Abs(name)
	if err != nil {
		return Config{}, err
	}
	c := Config{TrashRetentionDays: defaultTrashRetentionDays, Storage: ".", Workers: defaultWorkers, path: name, sources: map[string]string{}}
	b, err := os.ReadFile(name)
	if err == nil {
		// Reject unknown keys so a typo cannot silently drop a setting.
		d := yaml.NewDecoder(bytes.NewReader(b))
		d.KnownFields(true)
		if err = d.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
			return c, fmt.Errorf("%s: %w", name, err)
		}
	} else {
		return c, err
	}
	var fields map[string]yaml.Node
	if err := yaml.Unmarshal(b, &fields); err != nil {
		return c, err
	}
	for _, key := range []string{"storage", "directories", "workers", "exclude", "trash_retention_days", "retention"} {
		c.sources[key] = "default"
		if node, ok := fields[key]; ok && node.Tag != "!!null" {
			c.sources[key] = "config"
		}
		if _, ok := os.LookupEnv("HOBALIGHT_" + strings.ToUpper(key)); ok {
			c.sources[key] = "HOBALIGHT_" + strings.ToUpper(key)
		}
	}
	if v, ok := os.LookupEnv("HOBALIGHT_STORAGE"); ok {
		c.Storage = v
	}
	if v, ok := os.LookupEnv("HOBALIGHT_DIRECTORIES"); ok {
		if err = yaml.Unmarshal([]byte(v), &c.Directories); err != nil {
			return c, fmt.Errorf("HOBALIGHT_DIRECTORIES must be a YAML list: %w", err)
		}
	}
	if value, ok := os.LookupEnv("HOBALIGHT_WORKERS"); ok {
		c.Workers, err = strconv.Atoi(value)
		if err != nil {
			return c, fmt.Errorf("HOBALIGHT_WORKERS must be a positive integer: %w", err)
		}
	}
	if value, ok := os.LookupEnv("HOBALIGHT_TRASH_RETENTION_DAYS"); ok {
		c.TrashRetentionDays, err = strconv.Atoi(value)
		if err != nil {
			return c, fmt.Errorf("HOBALIGHT_TRASH_RETENTION_DAYS must be a positive integer: %w", err)
		}
	}
	if value, ok := os.LookupEnv("HOBALIGHT_RETENTION"); ok {
		c.Retention = Retention{}
		d := yaml.NewDecoder(strings.NewReader(value))
		d.KnownFields(true)
		if err := d.Decode(&c.Retention); err != nil {
			return c, fmt.Errorf("HOBALIGHT_RETENTION: %w", err)
		}
	}
	if err := c.Retention.validate(); err != nil {
		return c, err
	}
	if c.TrashRetentionDays < 1 || c.TrashRetentionDays > 365000 {
		return c, errors.New("trash_retention_days must be between 1 and 365000")
	}
	if value, ok := os.LookupEnv("HOBALIGHT_EXCLUDE"); ok {
		if err = yaml.Unmarshal([]byte(value), &c.Exclude); err != nil {
			return c, fmt.Errorf("HOBALIGHT_EXCLUDE must be a YAML list: %w", err)
		}
	}
	if _, err := compileExcludes(c.Exclude); err != nil {
		return c, err
	}
	if c.Workers < 1 {
		return c, errors.New("workers must be a positive integer")
	}
	if c.Storage == "" {
		c.Storage = "."
	}
	for _, d := range c.Directories {
		if d == "" {
			return c, errors.New("directories must not contain empty entries")
		}
	}
	if c.Storage == "~" || strings.HasPrefix(c.Storage, "~/") || strings.HasPrefix(c.Storage, "~"+string(filepath.Separator)) {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return c, fmt.Errorf("resolve storage home directory: %w", homeErr)
		}
		c.Storage = filepath.Join(home, filepath.FromSlash(c.Storage[1:]))
	}
	if !filepath.IsAbs(c.Storage) {
		c.Storage = filepath.Join(filepath.Dir(name), c.Storage)
	}
	for i, directory := range c.Directories {
		if !filepath.IsAbs(directory) {
			c.Directories[i] = filepath.Join(filepath.Dir(name), directory)
		}
	}
	c.Storage, err = filepath.Abs(c.Storage)
	return c, err
}
