package review

import (
	"context"
	"errors"
	"testing"
)

func TestDiagnosticAnnotationPreservesFailureIdentityAndRetryPolicy(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, Error(409, "conflict"), Error(503, "busy")} {
		annotated := &DiagnosticError{ID: "report", Stage: "patches", Cause: cause}
		if !errors.Is(annotated, cause) || Permanent(annotated) != Permanent(cause) {
			t.Fatalf("annotation changed failure policy: %v", cause)
		}
	}
}
