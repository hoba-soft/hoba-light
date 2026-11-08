package hobalight

// partialBackupError means a session and its complete source-error report were saved.
type partialBackupError struct{ error }

// ExitCode maps command errors to shell exit codes.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if onlyPartialBackups(err) {
		return 2
	}
	return 1
}

func onlyPartialBackups(err error) bool {
	if _, ok := err.(*partialBackupError); ok {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyPartialBackups(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyPartialBackups(wrapped.Unwrap())
	}
	return false
}
