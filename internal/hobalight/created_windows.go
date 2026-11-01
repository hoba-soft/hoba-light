package hobalight

import (
	"os"
	"syscall"
	"time"
)

func creationTime(_ string, info os.FileInfo) *string {
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		value := timestamp(time.Unix(0, data.CreationTime.Nanoseconds()))
		return &value
	}
	return nil
}
