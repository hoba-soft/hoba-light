package hobalight

import (
	"runtime"
	"runtime/debug"
)

// BuildVersion and BuildCommit are set by the build task.
var BuildVersion = "0.0.0"
var BuildCommit string

func Version() string {
	commit := BuildCommit
	if commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					commit = setting.Value
				}
			}
		}
	}
	if len(commit) < 8 {
		commit = "00000000"
	}
	return BuildVersion + "-" + commit[:8]
}

func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }
