## Added

- Back up individual files and add session comments with `-c`.
- Browse roots as a shared tree, or browse merged files and their versions.
- Filter the current list, search file names across sessions, and choose a local date with a preview of that day's backups.
- Apply optional retention by age or session count during `gc`.
- Save session outcomes and distinguish partial backups with exit code `2`.

## Fixed

- Faster restoration of selected files, with size checks and saved timestamps.
- Safer handling of changed sources, storage failures, and ambiguous short IDs.
- Consistent browser navigation, column layout, window resizing, and full-height content viewing with stable scrolling.
- Relative paths in backup progress and clearer forced deletion output.
