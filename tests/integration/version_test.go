package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestVersionWithoutStorageOrConfig(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "must-not-be-created")
	cmd := exec.Command(testBinary, "--config", filepath.Join(root, "missing.conf"), "--version")
	cmd.Env = append(os.Environ(), "HOBALIGHT_STORAGE="+storage)
	output, err := cmd.CombinedOutput()
	must(t, err)
	pattern := `^hobalight 0\.0\.0-[0-9a-f]{8} ` + runtime.GOOS + `/` + runtime.GOARCH + `$`
	if !regexp.MustCompile(pattern).MatchString(strings.TrimSpace(string(output))) {
		t.Fatalf("unexpected version: %s", output)
	}
	if _, err := os.Stat(storage); !os.IsNotExist(err) {
		t.Fatalf("version touched storage: %v", err)
	}
}

func TestVersionCommandIsNotSupported(t *testing.T) {
	c := newCLI(t)
	c.fails("unknown command \"version\"", "version")
}
