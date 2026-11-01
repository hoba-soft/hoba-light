# Usage

Use `hl` or `hobalight`. Run `hl --help` for all commands and options.

## Start

```sh
hl /path/to/documents
hl browse
```

The first command backs up a directory. The second opens saved backups.
Press `?` in the browser for controls.

Each backup creates a session. Unchanged data is reused.
Use `hl ./list` for a directory named `list`; command names take priority.

Backups are not encrypted. Empty directories and file permissions are not saved.
A backup with errors may be incomplete; the command reports failures and exits
with a nonzero status.

## Configuration

The config is created at `~/.hobalight/hobalight.conf` on macOS/Linux or
`%USERPROFILE%\.hobalight\hobalight.conf` on Windows. Backups go in the same folder
by default. To change this, edit the config:

```yaml
storage: /path/to/backups
directories:
  - /path/to/documents
workers: 4
exclude:
  - '*.log'
  - 'cache/'
trash_retention_days: 30
```

Relative storage and configured source paths use the config's directory.
Command paths use the working directory. Use `/` in Windows YAML paths.

Use `hl --config /path/to/hobalight.conf <command>` for another config.
A `hobalight.conf` beside the executable also takes priority over the home config.
`hl info` shows the selected config, settings and storage use.

Settings can also be set with `HOBALIGHT_CONFIG`, `HOBALIGHT_STORAGE`,
`HOBALIGHT_DIRECTORIES`, `HOBALIGHT_WORKERS`, `HOBALIGHT_EXCLUDE` and
`HOBALIGHT_TRASH_RETENTION_DAYS`. Directory and exclusion values are YAML lists.

## Backup

```sh
hl backup                # all configured directories
hl backup --force /path/to/documents
hl --exclude '*.log' /path/to/documents
```

`--force` reads every file, even if its size and timestamps have not changed.
`--exclude` replaces configured exclusions; `--exclude=` clears them.
Patterns use `/`: `*.log` matches names at any depth, `cache/` skips directories,
`/build/` selects the source root, and `**` matches directory levels.
Negation (`!`) is not supported.

## Find and restore

```sh
hl list
hl find report
hl history /path/to/documents/report.pdf
hl restore <session>
hl restore <session> Documents/report.pdf
hl restore-to <session> . /path/to/recovery
```

Use a session hash from `list`; at least eight unique characters are enough.
Paths inside a session are relative to its source directory.
The default restore folder is `./hobalight_restore/<short-hash>/`.
Different existing files are not overwritten.

Use `hl view <file-hash>` to read saved content and
`hl diff <old-session> <new-session>` to compare backups.

## Check and clean up

```sh
hl verify
hl verify --repair
hl refresh
hl delete <session>
hl gc --dry-run
hl gc
```

`verify` checks saved data. `--repair` looks for exact copies in trash or the
original source. `refresh` rebuilds session listings.

Deletion asks for confirmation and moves data to trash. `gc` collects unused
data and permanently removes trash older than `trash_retention_days` (default 30).
Use `--dry-run` to preview changes.

After a crash, a `.backup-*` file or `.gc.lock` directory may block operations.
Errors show its path. Remove it only after checking that the related process
has stopped.
