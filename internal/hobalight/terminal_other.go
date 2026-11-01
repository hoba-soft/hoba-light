//go:build !windows

package hobalight

import "os"

func enableBrowserOutput(*os.File) (func(), error) { return func() {}, nil }
