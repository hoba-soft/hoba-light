# Usage

Use `hl` or `hobalight`. Run `hl --help` for all commands and options.

## Start

```sh
hl /path/to/documents
hl browse
```

The first command backs up a directory. The second opens saved backups.
Press `?` in the browser for controls.

Use `hl backup <file>` or `hl <file>` to back up a single file.
Restoring its session places the file in the destination under its original name.

Add a session comment with `hl backup --comment "Before upgrade" <path>`.
The short form is `hl -c "Before upgrade" <path>`.
Comments appear in `list` and the browser; `find <text>` also matches comments.
In the browser, `/` filters the current list, including comments; `n` searches
file and directory names across sessions. Both ignore case.
Leave a filter empty or press Esc/Left to clear it. Navigation clears the filter.

Each backup creates a session. Unchanged data is reused.
Use `hl ./list` for a directory named `list`; command names take priority.

Backups are not encrypted. Empty directories and file permissions are not saved.
Sessions store `completed`, `completed_with_errors`, or `failed`.
Exit codes: `0` for success, `2` when all source errors are recorded in the saved
session, `1` for storage failures or an unsaved report. Unreadable directories
are reported as a whole; their contents are unknown. For multiple sources,
failure takes priority. If storage is unavailable, even a failed session may
not be saved.

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
hl backup                # all configured files and directories
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

The browser starts with a shared tree of backup roots. Open a root to see its sessions;
open a session to see its files. Left returns to the previous level.

Filter sessions by their creation day in local time with `--date YYYY-MM-DD` on
`list`, `find`, `history`, or `browse`. For example, `hl list --date 2026-11-07`.
In the browser, press `D` to choose a day and preview its roots and session counts.
Tab switches between the calendar and scrolling the roots. Enter applies the day;
`c` clears the filter. File modification dates do not affect this filter.

Press `m` to switch between Sessions and Merged files. The merged tree contains
all paths from the selected sessions, including older files. Open a file to see
its versions, then open a version to view metadata. Press `c` for content,
`r` to restore, or `o` to restore and open it. `s` lists a version’s sessions.

Dates are displayed in the computer’s local time zone. Stored timestamps and
logs stay in UTC. `--date` and the calendar select a local day, including
daylight-saving changes.

Optional session retention is applied only by `gc` (also shown by `gc --dry-run`):

```yaml
retention:
  days: 30
  sessions: 10
```

For each source, keep sessions within the last 30 days **or** its 10 newest
sessions. All statuses count. Days mean elapsed 24-hour periods. Omit a rule
or use `0` to disable it; both disabled keeps all sessions. Removed sessions
and unused objects go to trash, subject to `trash_retention_days`.
