package hobalight

import (
	"os"
	"syscall"
	"time"
)

func creationTime(_ string, info os.FileInfo) *string {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		birth := stat.Birthtimespec
		if birth.Sec != 0 || birth.Nsec != 0 {
			value := timestamp(time.Unix(birth.Sec, birth.Nsec))
			return &value
		}
	}
	return nil
}
