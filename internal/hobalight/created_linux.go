package hobalight

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func creationTime(name string, _ os.FileInfo) *string {
	var stat unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, name, 0, unix.STATX_BTIME, &stat); err == nil && stat.Mask&unix.STATX_BTIME != 0 {
		value := timestamp(time.Unix(stat.Btime.Sec, int64(stat.Btime.Nsec)))
		return &value
	}
	return nil
}
