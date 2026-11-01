package hobalight

import (
	"errors"
	"syscall"
)

const errorSharingViolation syscall.Errno = 32

// Antivirus and indexing software briefly open new files without delete
// sharing, which makes replacing them fail until they are closed.
func isTransientRename(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errorSharingViolation)
}
