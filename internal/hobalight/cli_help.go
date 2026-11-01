package hobalight

const commandHelp = `Usage: hobalight [--config file] [--exclude pattern ...] [command]

No command shows help. A directory path alone runs backup.
Use ./<name> for directories whose names match a command.
Commands:
  info                                Show settings, paths and storage statistics
  view [--mode auto|text|hex] <file-hash> | <session> <file-path>
                                      View file contents with scrolling and search
  browse                              Open the interactive backup browser
  backup [--force] [directory]     Back up one or configured directories;
                                      --force rereads and rehashes every file
  list [session] [relative/path]       List sessions or directory contents
  history <absolute-path> | history <session> <relative-file-path>
                                      Show versions of a file across sessions
  find <name>                         Find files in all sessions
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
