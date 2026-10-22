package review

import (
	"context"
	"errors"
	"fmt"
)

type Source interface {
	Root() string
	Kind() string
	Support() Support
	Snapshot(context.Context, DiffMode) (RepositoryDiff, error)
	Patch(context.Context, DiffMode, ChangedFile, *string) (FilePatch, error)
	Contents(context.Context, DiffMode, ChangedFile, *string) (FileContents, error)
}

type Support struct {
	Scopes          []DiffMode
	Refresh         bool
	StagingMetadata bool
	FileContents    bool
}

type RequestError struct {
	Status int
	Detail string
}

func (err *RequestError) Error() string { return err.Detail }

func Error(status int, format string, values ...any) error {
	return &RequestError{Status: status, Detail: fmt.Sprintf(format, values...)}
}

// Permanent reports whether retrying the same request cannot succeed: a
// client-class rejection other than timeout or rate limiting. Unclassified
// errors, timeouts, throttling and server failures are transient.
func Permanent(err error) bool {
	var request *RequestError
	if !errors.As(err, &request) || request.Status < 400 || request.Status >= 500 {
		return false
	}
	return request.Status != 408 && request.Status != 429
}

func Message(value string) *string { return &value }
