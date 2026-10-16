// Package reviewdata defines storage-independent review identities and results.
package reviewdata

import (
	"errors"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

var ErrNotFound = errors.New("diff not found")
var ErrExpired = errors.New("captured diff expired")
var ErrSubmissionConflict = errors.New("submission ID reused with different input")

func VersionID(diffID, revision string) string { return diffID + "." + revision }

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Binding struct {
	ContextID    string
	LocationID   *string
	RepositoryID *string
	DiffIDs      map[review.DiffMode]string
}

// FileVersion identifies a stored scope and requested file without loading its
// manifest or preview. A nil fingerprint means the file is absent.
type FileVersion struct {
	DiffID      string
	VersionID   string
	Fingerprint *string
}

type ContextInfo struct {
	Source           string
	Stale            bool
	Sessions         []SessionAssociation
	Metadata         *ingestion.Metadata
	ID               string
	Kind             string
	Root             *string
	LocationID       *string
	RepositoryID     *string
	CommonDir        *string
	WorktreeKey      *string
	SubmittedFrom    *string
	CreatedAt        int64
	LastSubmittedAt  int64
	ExpiresAt        *int64
	ChangedFileCount *int
	// StreamID names the source/repository/checkout/branch/comparison stream;
	// LatestContextID is that stream's current head.
	StreamID        *string
	LatestContextID *string
}

type SessionAssociation struct {
	SourceID        string `json:"sourceId"`
	Harness         string `json:"harness"`
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	FirstObservedAt int64  `json:"firstObservedAt"`
	LastObservedAt  int64  `json:"lastObservedAt"`
}
