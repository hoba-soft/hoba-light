// Command package builds portable archives and native installers from an explicit
// file list. It deliberately excludes configs, backups, logs, and build scratch.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type options struct {
	version, commit, targetOS, arch, output, homepage string
	archiveOnly                                       bool
}

func main() {
	var o options
	flag.StringVar(&o.version, "version", "", "release version (major.minor.patch)")
	flag.StringVar(&o.commit, "commit", "", "source commit; defaults to HEAD")
	flag.StringVar(&o.targetOS, "os", runtime.GOOS, "target operating system")
	flag.StringVar(&o.arch, "arch", runtime.GOARCH, "target architecture")
	flag.StringVar(&o.output, "out", "dist", "output directory")
	flag.StringVar(&o.homepage, "homepage", "https://github.com/hoba-soft/hoba-light", "project URL")
	flag.BoolVar(&o.archiveOnly, "archive-only", false, "build only the portable archive")
	flag.Parse()
	if err := build(o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
func output(name string, args ...string) (string, error) {
	b, err := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(b)), err
}
func versionParts(version string) ([3]int, error) {
	var parts [3]int
	fields := strings.Split(version, ".")
	if len(fields) != 3 {
		return parts, fmt.Errorf("version must be major.minor.patch: %q", version)
	}
	for i, field := range fields {
		if field == "" || (len(field) > 1 && field[0] == '0') {
			return parts, fmt.Errorf("invalid version: %q", version)
		}
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 || n > 65535 {
			return parts, fmt.Errorf("invalid version: %q", version)
		}
		parts[i] = n
	}
	return parts, nil
}
func build(o options) error {
	parts, err := versionParts(o.version)
	if err != nil {
		return err
	}
	if o.arch != "amd64" && o.arch != "arm64" {
		return errors.New("supported architectures: amd64, arm64")
	}
	if o.targetOS != "linux" && o.targetOS != "darwin" && o.targetOS != "windows" {
		return errors.New("supported operating systems: linux, darwin, windows")
	}
	if o.targetOS == "windows" && (parts[0] > 255 || parts[1] > 255) {
		return errors.New("MSI requires major and minor versions <= 255")
	}
	if !o.archiveOnly && o.targetOS != "linux" && o.targetOS != runtime.GOOS {
		return errors.New("native macOS and Windows installers must be built on their respective OS")
	}
	module, err := output("go", "list", "-m")
	if err != nil {
		return err
	}
	if o.commit == "" {
		o.commit, err = output("git", "rev-parse", "HEAD")
		if err != nil {
			return err
		}
	}
	if strings.ContainsAny(o.commit, " \t\r\n") {
		return errors.New("commit must not contain whitespace")
	}
	if err = os.MkdirAll(o.output, 0755); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "hobalight-package-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	stage := filepath.Join(work, "portable")
	if err = os.Mkdir(stage, 0755); err != nil {
		return err
	}
	ext := ""
	if o.targetOS == "windows" {
		ext = ".exe"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", fmt.Sprintf("-X %s/internal/hobalight.BuildVersion=%s -X %s/internal/hobalight.BuildCommit=%s", module, o.version, module, o.commit), "-o", filepath.Join(stage, "hobalight"+ext), "./cmd/hobalight")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+o.targetOS, "GOARCH="+o.arch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return err
	}
	if err = copyFile(filepath.Join(stage, "hobalight"+ext), filepath.Join(stage, "hl"+ext), 0755); err != nil {
		return err
	}
	docs, err := documentation()
	if err != nil {
		return err
	}
	for _, file := range docs {
		if err = copyFile(file, filepath.Join(stage, file), 0644); err != nil {
			return err
		}
	}
	base := fmt.Sprintf("hobalight_%s_%s_%s", o.version, o.targetOS, o.arch)
	archive := filepath.Join(o.output, base+".tar.gz")
	if o.targetOS == "windows" {
		archive = filepath.Join(o.output, base+".zip")
	}
	if err = writeArchive(stage, archive, o.targetOS == "windows"); err != nil {
		return err
	}
	if o.archiveOnly {
		return nil
	}
	switch o.targetOS {
	case "linux":
		return linuxPackages(o, stage, docs, base, work)
	case "darwin":
		return macPackage(o, stage, docs, base, work)
	default:
		return windowsPackage(o, stage, docs, base, work)
	}
}
func documentation() ([]string, error) {
	// Keep this allowlist explicit: never ship untracked notes from docs/.
	return []string{"README.md", "LICENSE", "CONTRIBUTING.md", "configs/hobalight.conf.example", "docs/usage.md", "docs/storage-backends.md", "docs/installation.md"}, nil
}

func copyFile(from, to string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return err
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}
func writeArchive(root, name string, windows bool) (err error) {
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	var add func(string, os.FileInfo, io.Reader) error
	var closeArchive func() error
	if windows {
		writer := zip.NewWriter(file)
		closeArchive = writer.Close
		add = func(name string, info os.FileInfo, r io.Reader) error {
			h, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			h.Name = name
			h.Method = zip.Deflate
			w, err := writer.CreateHeader(h)
			if err != nil {
				return err
			}
			_, err = io.Copy(w, r)
			return err
		}
	} else {
		gz := gzip.NewWriter(file)
		writer := tar.NewWriter(gz)
		closeArchive = func() error { return errors.Join(writer.Close(), gz.Close()) }
		add = func(name string, info os.FileInfo, r io.Reader) error {
			h, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			h.Name = name
			h.Uid = 0
			h.Gid = 0
			h.Uname = ""
			h.Gname = ""
			if err = writer.WriteHeader(h); err != nil {
				return err
			}
			_, err = io.Copy(writer, r)
			return err
		}
	}
	walkErr := filepath.WalkDir(root, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(name)
		if err != nil {
			return err
		}
		defer in.Close()
		return add(filepath.ToSlash(rel), info, in)
	})
	return errors.Join(walkErr, closeArchive())
}
func linuxPackages(o options, stage string, docs []string, base, work string) error {
	contents := []map[string]any{
		{"src": filepath.Join(stage, "hobalight"), "dst": "/usr/bin/hobalight", "file_info": map[string]any{"mode": 0755}},
		{"src": "/usr/bin/hobalight", "dst": "/usr/bin/hl", "type": "symlink"},
	}
	for _, file := range docs {
		contents = append(contents, map[string]any{"src": filepath.Join(stage, file), "dst": "/usr/share/doc/hobalight/" + filepath.ToSlash(file), "file_info": map[string]any{"mode": 0644}})
	}
	config := map[string]any{"name": "hoba-light", "arch": o.arch, "platform": "linux", "version": o.version, "maintainer": "hoba-soft <hoba.soft@proton.me>", "description": "A small command-line backup tool.", "homepage": o.homepage, "license": "MIT", "section": "utils", "contents": contents}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	name := filepath.Join(work, "nfpm.yaml")
	if err = os.WriteFile(name, data, 0600); err != nil {
		return err
	}
	for _, format := range []string{"deb", "rpm"} {
		if err = run("nfpm", "package", "--config", name, "--packager", format, "--target", filepath.Join(o.output, base+"."+format)); err != nil {
			return err
		}
	}
	return nil
}
func xmlText(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
func macPackage(o options, stage string, docs []string, base, work string) error {
	root := filepath.Join(work, "root")
	if err := copyFile(filepath.Join(stage, "hobalight"), filepath.Join(root, "usr/local/bin/hobalight"), 0755); err != nil {
		return err
	}
	if err := os.Symlink("hobalight", filepath.Join(root, "usr/local/bin/hl")); err != nil {
		return err
	}
	for _, file := range docs {
		if err := copyFile(filepath.Join(stage, file), filepath.Join(root, "usr/local/share/doc/hobalight", file), 0644); err != nil {
			return err
		}
	}
	component := filepath.Join(work, "component.pkg")
	if err := run("pkgbuild", "--root", root, "--identifier", "com.hoba-soft.hobalight", "--version", o.version, "--install-location", "/", "--ownership", "recommended", component); err != nil {
		return err
	}
	arch := "arm64"
	if o.arch == "amd64" {
		arch = "x86_64"
	}
	distribution := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<installer-gui-script minSpecVersion="2">
 <title>Hoba Light</title>
 <options customize="never" require-scripts="false" hostArchitectures="%s"/>
 <domains enable_localSystem="true" enable_currentUserHome="false" enable_anywhere="false"/>
 <choices-outline><line choice="default"/></choices-outline>
 <choice id="default" visible="false"><pkg-ref id="com.hoba-soft.hobalight"/></choice>
 <pkg-ref id="com.hoba-soft.hobalight" version="%s">component.pkg</pkg-ref>
</installer-gui-script>`, arch, xmlText(o.version))
	dist := filepath.Join(work, "Distribution.xml")
	if err := os.WriteFile(dist, []byte(distribution), 0600); err != nil {
		return err
	}
	return run("productbuild", "--distribution", dist, "--package-path", work, filepath.Join(o.output, base+".pkg"))
}

// Keep this UpgradeCode across versions, architectures, and repository moves.
const upgradeCode = "7DC99072-2702-4B24-B2DA-F3EF028BBAA5"

func windowsManifest(o options, stage string, docs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<Wix xmlns="http://wixtoolset.org/schemas/v4/wxs">
 <Package Name="Hoba Light" Manufacturer="hoba-soft" Version="%s" UpgradeCode="%s" Scope="perMachine" Compressed="yes">
  <MajorUpgrade DowngradeErrorMessage="A newer version of Hoba Light is already installed." Schedule="afterInstallInitialize"/>
  <MediaTemplate EmbedCab="yes"/>
  <StandardDirectory Id="ProgramFiles6432Folder"><Directory Id="INSTALLFOLDER" Name="hobalight">
   <Component Id="MainExecutable" Guid="*"><File Id="HobaLightExe" Source="%s" KeyPath="yes"/>
    <Environment Id="InstallPath" Name="PATH" Value="[INSTALLFOLDER]" Action="set" Part="last" System="yes" Permanent="no"/>
   </Component>
   <Component Id="AliasExecutable" Guid="*"><File Id="AliasExe" Source="%s" KeyPath="yes"/></Component>
`, xmlText(o.version), upgradeCode, xmlText(filepath.Join(stage, "hobalight.exe")), xmlText(filepath.Join(stage, "hl.exe")))
	// Preserve documentation paths so relative README links work after install.
	for _, dir := range []string{".", "configs", "docs"} {
		if dir != "." {
			fmt.Fprintf(&b, "<Directory Id=\"Docs_%s\" Name=\"%s\">", dir, dir)
		}
		for i, file := range docs {
			if filepath.ToSlash(filepath.Dir(file)) != dir {
				continue
			}
			fmt.Fprintf(&b, "<Component Id=\"Doc%d\" Guid=\"*\"><File Id=\"DocFile%d\" Source=\"%s\" KeyPath=\"yes\"/></Component>", i, i, xmlText(filepath.Join(stage, file)))
		}
		if dir != "." {
			b.WriteString("</Directory>")
		}
	}

	b.WriteString("</Directory></StandardDirectory><Feature Id=\"Complete\" Level=\"1\"><ComponentRef Id=\"MainExecutable\"/><ComponentRef Id=\"AliasExecutable\"/>")
	for i := range docs {
		fmt.Fprintf(&b, "<ComponentRef Id=\"Doc%d\"/>", i)
	}
	b.WriteString("</Feature></Package></Wix>")
	return b.String()
}
func windowsPackage(o options, stage string, docs []string, base, work string) error {
	manifest := filepath.Join(work, "installer.wxs")
	if err := os.WriteFile(manifest, []byte(windowsManifest(o, stage, docs)), 0600); err != nil {
		return err
	}
	arch := "x64"
	if o.arch == "arm64" {
		arch = "arm64"
	}
	return run("wix", "build", "-arch", arch, "-pdbtype", "none", "-o", filepath.Join(o.output, base+".msi"), manifest)
}
