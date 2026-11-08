package hobalight

import (
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	sessionCompleted           = "completed"
	sessionCompletedWithErrors = "completed_with_errors"
	sessionFailed              = "failed"
)

// Only failures reading the source may produce a completed-with-errors session.
// All storage/encoding failures remain fatal, even when diagnostic metadata saves.
type sourceBackupError struct{ error }

func sourceFailure(err error) error { return &sourceBackupError{err} }
func isSourceFailure(err error) bool {
	var source *sourceBackupError
	return errors.As(err, &source)
}

type sourceReader struct{ io.Reader }

func (r sourceReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err != nil && err != io.EOF {
		err = sourceFailure(err)
	}
	return n, err
}

// Older sessions did not store a status. Do not rewrite their hashed objects.
func (s Session) completionStatus() string {
	if s.Status != "" {
		return s.Status
	}
	if len(s.Errors) > 0 {
		return sessionCompletedWithErrors
	}
	return sessionCompleted
}

// A failed report is best effort: storage may no longer accept any writes.
func (s *Store) saveFailedBackup(session Session, cause error) (string, error) {
	s.logger.Printf("session failed: %v", cause)
	session.Status = sessionFailed
	session.Errors = append(session.Errors, cause.Error())
	data, err := encode(session)
	if err != nil {
		return "", errors.Join(cause, err)
	}
	id := hash(data) + "S"
	if err = s.put(id, data); err != nil {
		return "", errors.Join(cause, fmt.Errorf("save failed session: %w", err))
	}
	s.sessionListings = nil
	ref, err := s.referenceFromSession(id, session)
	if err != nil {
		return id, errors.Join(cause, err)
	}
	data, err = encode(ref)
	if err != nil {
		return id, errors.Join(cause, err)
	}
	started, _ := time.Parse(time.RFC3339Nano, session.Started)
	err = writeStorageBytes(s.backend, "sessions/"+sessionReferenceName(started, id), data, false)
	return id, errors.Join(cause, err)
}
