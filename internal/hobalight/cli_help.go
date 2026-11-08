package hobalight

const commandHelp = `Usage: hobalight [--config file] [--exclude pattern ...] [command]

No command shows help. A file or directory path alone runs backup.
Use ./<name> for paths whose names match a command.
Commands:
  info                                Show settings, paths and storage statistics
  view [--mode auto|text|hex] <file-hash> | <session> <file-path>
                                      View file contents with scrolling and search
  browse                              Open the interactive backup browser
  backup [--force] [path]              Back up one file/directory or configured paths;
                                      --force rereads and rehashes every file
                                      --comment/-c text labels each new session
  list [--date YYYY-MM-DD] [session] [relative/path]       List sessions or directory contents
  history <absolute-path> | history <session> <relative-file-path>
                                      Show versions of a file across sessions
                                      list/find/history/browse accept --date YYYY-MM-DD (local time)
  find <name>                         Find files and session comments
  find <session> [relative/directory] <name>
  delete [--force] <session-or-file-hash> [relative/path]
                                      Confirm deletion; gc collects unused data
  gc [--dry-run] [--verbose]        Collect unused objects and expire trash
  verify [--repair] [session-hash]     Verify objects; optionally restore exact matches
  refresh                             Refresh session references from objects
  diff <old-session> <new-session>     Compare sessions; --mode auto|text|hex
  diff <old-session> <new-session> <path>
  diff <old-session> <old-path> <new-session> <new-path>
  diff <old-file-hash> <new-file-hash>  Compare file attributes and content
  restore <session-or-file-hash> [relative/path]
  restore-to <session> <relative/path-or-dot> <destination>
  restore-to <file-hash> <destination>

Options:`
