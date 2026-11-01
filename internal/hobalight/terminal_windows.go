package hobalight

import (
	"os"

	"golang.org/x/sys/windows"
)

func enableBrowserOutput(out *os.File) (func(), error) {
	handle := windows.Handle(out.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return nil, err
	}
	return func() { windows.SetConsoleMode(handle, mode) }, nil
}
