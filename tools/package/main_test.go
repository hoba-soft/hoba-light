package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPortableArchivesPreserveExecutableAndDoNotEmbedOwners(t *testing.T) {
	for _, windows := range []bool{false, true} {
		t.Run(map[bool]string{false: "tar", true: "zip"}[windows], func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "stage")
			must(t, os.MkdirAll(filepath.Join(source, "docs"), 0755))
			must(t, os.WriteFile(filepath.Join(source, "hobalight"), []byte("binary"), 0755))
			must(t, os.WriteFile(filepath.Join(source, "docs/usage.md"), []byte("documentation"), 0644))
			name := filepath.Join(root, "release")
			must(t, writeArchive(source, name, windows))
			files := map[string]string{}
			if windows {
				z, err := zip.OpenReader(name)
				must(t, err)
				defer z.Close()
				for _, file := range z.File {
					r, err := file.Open()
					must(t, err)
					data, err := io.ReadAll(r)
					must(t, err)
					must(t, r.Close())
					files[file.Name] = string(data)
				}
			} else {
				f, err := os.Open(name)
				must(t, err)
				defer f.Close()
				gz, err := gzip.NewReader(f)
				must(t, err)
				defer gz.Close()
				r := tar.NewReader(gz)
				for {
					header, err := r.Next()
					if err == io.EOF {
						break
					}
					must(t, err)
					if header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
						t.Fatal("local owner leaked", header)
					}
					sourceInfo, err := os.Stat(filepath.Join(source, filepath.FromSlash(header.Name)))
					must(t, err)
					if header.Mode != int64(sourceInfo.Mode().Perm()) {
						t.Fatal(header.Mode)
					}
					data, err := io.ReadAll(r)
					must(t, err)
					files[header.Name] = string(data)
				}
			}
			if len(files) != 2 || files["hobalight"] != "binary" || files["docs/usage.md"] != "documentation" {
				t.Fatal(files)
			}
		})
	}
}
func TestDocumentationAllowlistDoesNotPickUpPersonalFiles(t *testing.T) {
	files, err := documentation()
	must(t, err)
	for _, file := range files {
		if strings.Contains(file, "private") || file == "hobalight.conf" || strings.HasPrefix(file, "bin/") {
			t.Fatal(file)
		}
	}
	// Verify the allowlisted source files exist when run from the module root.
	for _, file := range files {
		if _, err := os.Stat(filepath.Join("../..", file)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestWindowsInstallerPreservesPathsAndStableUpgradeIdentity(t *testing.T) {
	docs, _ := documentation()
	text := windowsManifest(options{version: "1.12.0"}, "C:/staging & input", docs)
	decoder := xml.NewDecoder(strings.NewReader(text))
	dirs := map[string]bool{}
	files := 0
	pathFound := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		must(t, err)
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		attrs := map[string]string{}
		for _, attr := range start.Attr {
			attrs[attr.Name.Local] = attr.Value
		}
		switch start.Name.Local {
		case "Package":
			if attrs["UpgradeCode"] != upgradeCode || attrs["Scope"] != "perMachine" {
				t.Fatal(attrs)
			}
		case "Directory":
			if dirs[attrs["Id"]] {
				t.Fatal("duplicate directory", attrs)
			}
			dirs[attrs["Id"]] = true
		case "Environment":
			pathFound = attrs["Name"] == "PATH" && attrs["Permanent"] == "no" && attrs["System"] == "yes"
		case "File":
			files++
			if strings.HasSuffix(attrs["Source"], "hobalight.conf") {
				t.Fatal("active config shipped")
			}
		}
	}
	if files != len(docs)+2 || !pathFound || !dirs["Docs_docs"] || !dirs["Docs_configs"] {
		t.Fatalf("files=%d PATH=%t dirs=%v", files, pathFound, dirs)
	}
}
func TestVersionScriptKeepsCommitCountAndSeparateMajor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test runs on Unix runners")
	}
	script, err := filepath.Abs("../../packaging/version.sh")
	must(t, err)
	repo := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=hoba-soft", "GIT_AUTHOR_EMAIL=hoba.soft@proton.me", "GIT_COMMITTER_NAME=hoba-soft", "GIT_COMMITTER_EMAIL=hoba.soft@proton.me")
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, data)
		}
		return strings.TrimSpace(string(data))
	}
	git("init", "--bare", remote)
	git("init", "-b", "main")
	git("remote", "add", "origin", remote)
	git("commit", "--allow-empty", "-m", "First")
	git("commit", "--allow-empty", "-m", "Second")
	sha := git("rev-parse", "HEAD")
	for _, major := range []string{"0", "1"} {
		out := filepath.Join(t.TempDir(), "output")
		cmd := exec.Command("bash", script)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "VERSION_MAJOR="+major, "GITHUB_REF=refs/heads/main", "GITHUB_EVENT_NAME=push", "GITHUB_SHA="+sha, "BEFORE_SHA=0000000000000000000000000000000000000000", "GITHUB_OUTPUT="+out)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("version script: %v %s", err, data)
		}
		contents, err := os.ReadFile(out)
		must(t, err)
		if !strings.Contains(string(contents), "version="+major+".2.0") || git("rev-parse", "v"+major+".2.0") != sha {
			t.Fatal(string(contents))
		}
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
