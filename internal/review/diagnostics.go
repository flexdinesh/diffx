package review

import "fmt"

// DiagnosticError annotates a producer failure without changing its identity or
// retry classification. The report itself remains on the producer's machine.
type DiagnosticError struct {
	ID    string
	Code  string
	Stage string
	Cause error
}

func (failure *DiagnosticError) Error() string {
	if failure.Code == "collection_timeout" {
		return fmt.Sprintf("Collection timed out during %s. Diagnostics: %s", failure.Stage, failure.ID)
	}
	return fmt.Sprintf("%v (during %s; diagnostics: %s)", failure.Cause, failure.Stage, failure.ID)
}

func (failure *DiagnosticError) Unwrap() error { return failure.Cause }
