//go:build !windows

package hobalight

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func openFile(name string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	} else if runtime.GOOS != "linux" {
		return fmt.Errorf("opening files is unsupported on %s", runtime.GOOS)
	}
	output, err := exec.Command(command, name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", command, err, strings.TrimSpace(string(output)))
	}
	return nil
}
