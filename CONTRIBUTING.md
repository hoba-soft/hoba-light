# Development

Run commands from the directory containing `go.mod`. Requires Go 1.23 or newer.

```sh
go build -trimpath -o bin/hl ./cmd/hobalight
go vet ./...
go test -race -count=1 ./...
```

With Go Task installed, `task build` builds both commands and `task check` runs
all checks. Windows ARM64 tests run without the unsupported race detector.
Integration tests build and run the real CLI with temporary data.

## Code

- `cmd/hobalight`: entry point.
- `internal/hobalight`: commands, storage and terminal UI.
- `tests/integration`: CLI and browser tests.
- `configs/hobalight.conf.example`: must match the generated config.

Keep these rules when changing storage:

- Objects are the source of truth. Session references can be rebuilt.
- Validate all session dependencies before GC moves objects.
- Save replaced bytes in trash. Retention starts when data enters trash.
- Protect restore paths from traversal, symlinks and overwriting different files.
- Use writer/maintenance coordination for storage changes.
- Stream large content; keep memory use bounded and allow cancellation.
- Clear browser caches after changes and keep CLI/browser behavior in sync.

See [storage design](docs/storage-backends.md) for the backend contract.

## Packages and releases

```sh
go run ./tools/package --version 1.1.0 --os linux --arch amd64
```

Output goes to `dist/`. Use `--archive-only` to build archives for other systems.
Linux installers need nFPM 2.47.0. macOS installers need `pkgbuild` and
`productbuild` on macOS. Windows MSI builds need .NET and WiX 5.0.2 on Windows.

The same workflow runs in both repositories. Work in `hoba/hoba-light`, then
copy finished changes to the root of `hoba-light`; also copy the workflow.
Keep shared files identical. Main pushes create tags and releases with installers,
archives and checksums. CI tests install, upgrade and removal on six platforms.

Versions use `<major>.<first-parent commit count>.0`: major `0` in `hoba`, `1` in
`hoba-light`. Binaries also show the source commit. MSI limits are 255, 255, 65535.
Keep package name `hoba-light`, macOS ID `com.hoba-soft.hobalight` and the Windows
UpgradeCode unchanged so updates replace the installed version.
