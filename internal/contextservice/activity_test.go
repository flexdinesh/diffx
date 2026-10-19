package contextservice

import (
	"testing"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

func TestActivityTimeFallsBackToCapturedTime(t *testing.T) {
	for _, item := range []Context{
		{LastChangedAt: 123, CreatedAt: 999, LastSubmittedAt: 1000},
		{Observation: &ingestion.Metadata{CollectedAt: 123}, CreatedAt: 999, LastSubmittedAt: 1000},
		{CreatedAt: 123, LastSubmittedAt: 1000},
	} {
		if got := ActivityTime(item); got != 123 {
			t.Fatalf("activity %d, want 123: %+v", got, item)
		}
	}
	left, right := Context{ID: "a", CreatedAt: 123}, Context{ID: "b", CreatedAt: 123}
	if CompareActivity(left, right) >= 0 || CompareActivity(right, left) <= 0 || CompareActivity(left, left) != 0 {
		t.Fatal("context identity must break activity ties deterministically")
	}
}
