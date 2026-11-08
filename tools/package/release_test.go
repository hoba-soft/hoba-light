package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleasePublishesPackagesWithoutChangingNotes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release publishing runs on Linux")
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum is required by the publishing runner")
	}
	script, err := filepath.Abs("../../packaging/release.sh")
	must(t, err)
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%v", existing), func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			must(t, os.Mkdir(bin, 0755))
			fake := `#!/usr/bin/env bash
set -eu
printf '%s\n' "$*" >> "$TEST_RELEASE_LOG"
if [[ "$1 $2" == 'release view' ]]; then
  [[ "$TEST_EXISTING_RELEASE" == true ]]
fi
`
			must(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(fake), 0755))
			count := 0
			for _, system := range []string{"linux", "darwin", "windows"} {
				suffixes := []string{"tar.gz", "pkg"}
				if system == "linux" {
					suffixes = []string{"tar.gz", "deb", "rpm"}
				}
				if system == "windows" {
					suffixes = []string{"zip", "msi"}
				}
				for _, arch := range []string{"amd64", "arm64"} {
					dir := filepath.Join(root, "collected", "hobalight-"+system+"-"+arch)
					must(t, os.MkdirAll(dir, 0755))
					for _, suffix := range suffixes {
						name := "hobalight_1.2.0_" + system + "_" + arch + "." + suffix
						must(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0644))
						count++
					}
				}
			}
			log := filepath.Join(root, "calls.log")
			command := exec.Command("bash", script)
			command.Dir = root
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "RELEASE_VERSION=1.2.0", "RELEASE_COMMIT=01234567", "GITHUB_RUN_ID=42", "GITHUB_REPOSITORY=hoba-soft/hoba-light", "TEST_RELEASE_LOG="+log, "TEST_EXISTING_RELEASE="+fmt.Sprint(existing))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			sums, err := os.ReadFile(filepath.Join(root, "dist", "SHA256SUMS"))
			must(t, err)
			if len(strings.Fields(string(sums))) != count*2 {
				t.Fatal("checksums do not cover all packages")
			}
			calls, err := os.ReadFile(log)
			must(t, err)
			if strings.Contains(string(calls), "api ") || strings.Contains(string(calls), "--notes-file") {
				t.Fatal("release must not read notes from PRs or files")
			}
			action := "release create"
			if existing {
				action = "release upload"
			}
			if !strings.Contains(string(calls), action+" v1.2.0") || !strings.Contains(string(calls), "release edit v1.2.0 --repo hoba-soft/hoba-light --draft=false") {
				t.Fatal(string(calls))
			}
			if !existing && !strings.Contains(string(calls), "--notes  --draft") {
				t.Fatal("initial release must have empty notes")
			}
		})
	}
}
