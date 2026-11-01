package hobalight

import (
	"fmt"
	"syscall"
	"unsafe"
)

// ShellExecute uses the file association directly, without a command shell.
func openFile(name string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	shell := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")
	result, _, _ := shell.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("Windows could not open file (ShellExecute error %d)", result)
	}
	return nil
}
