package submission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	"github.com/flexdinesh/diffx/internal/ingestion"
)

// SyncOptions describes one manual sync of a credential's route. Fresh is the
// new collection, or nil for an explicit retry or failed collection.
type SyncOptions struct {
	StateDir, Server, Token string
	Fresh                   *ingestion.Request
	// Fingerprint identifies content plus the metadata that must still upload.
	Fingerprint func(ingestion.Request) string
	Progress    func(string)
}

// SyncResult reports completed work and per-submission failures that left
// immutable payloads retained for a later retry.
type SyncResult struct {
	Uploaded, Unchanged int
	Failures            []error
	// Empty means no retained or fresh submission was eligible for upload.
	Empty bool
}

// Sync persists the fresh collection before contacting the server, pins
// retained payloads to the verified destination, suppresses uploads whose
// acknowledged context is current, and republishes a fresh collection when a
// reused payload commits without becoming the stream head.
func Sync(ctx context.Context, options SyncOptions) (SyncResult, error) {
	var result SyncResult
	show := func(message string) {
		if options.Progress != nil {
			options.Progress(message)
		}
	}
	digest := sha256.Sum256([]byte(options.Token))
	route := strings.TrimRight(options.Server, "/") + ":" + hex.EncodeToString(digest[:])
	box, err := Open(ctx, options.StateDir, route)
	if err != nil {
		return result, err
	}
	defer box.Close()
	pending, err := box.Pending()
	if err != nil {
		return result, err
	}
	var fresh ingestion.Request
	reusedSubmission := ""
	if options.Fresh != nil {
		fresh = *options.Fresh
		fingerprint := options.Fingerprint(fresh)
		for _, item := range pending {
			if fingerprint != "" && options.Fingerprint(item.Request) == fingerprint {
				reusedSubmission = item.Request.SubmissionID
				break
			}
		}
		if reusedSubmission == "" {
			item := Pending{Request: fresh}
			if err := box.Save(item); err != nil {
				return result, err
			}
			pending = append(pending, item)
		}
	}
	show("Upload · connecting")
	target, err := Resolve(ctx, options.Server, options.Token)
	if err != nil {
		return result, err
	}
	// Unknown-owner captures stay bound to the collecting credential. Once health
	// establishes the owner, persist under that identity before any upload so a
	// replacement credential can recover the exact attempted payload.
	provisional := box
	box, err = Open(ctx, options.StateDir, target.Target.Destination)
	if err != nil {
		return result, err
	}
	defer box.Close()
	verified, err := box.Pending()
	if err != nil {
		return result, err
	}
	for index := 0; index < len(pending); index++ {
		item := pending[index]
		if item.StateID != "" && item.StateID != target.Target.Identity {
			result.Failures = append(result.Failures, fmt.Errorf("submission %s belongs to another server database; retained for recovery", item.Request.SubmissionID))
			if item.Request.SubmissionID == reusedSubmission {
				freshItem := Pending{Request: fresh}
				if err := provisional.Save(freshItem); err != nil {
					return result, err
				}
				pending = append(pending, freshItem)
			}
			continue
		}
		found := false
		for _, existing := range verified {
			if existing.Request.SubmissionID == item.Request.SubmissionID {
				if !reflect.DeepEqual(existing.Request, item.Request) || existing.StateID != target.Target.Identity {
					return result, fmt.Errorf("submission %s conflicts with retained payload", item.Request.SubmissionID)
				}
				found = true
				break
			}
		}
		if !found && options.Fresh != nil && item.Request.SubmissionID == fresh.SubmissionID {
			// A new collection can recover an immutable attempted submission.
			fingerprint := options.Fingerprint(item.Request)
			for _, existing := range verified {
				if fingerprint != "" && options.Fingerprint(existing.Request) == fingerprint {
					reusedSubmission = existing.Request.SubmissionID
					found = true
					break
				}
			}
		}
		if !found {
			item.StateID = target.Target.Identity
			if err := box.Save(item); err != nil {
				return result, err
			}
			verified = append(verified, item)
		}
		if err := provisional.Remove(item.Request.SubmissionID); err != nil {
			return result, err
		}
	}
	pending = verified
	if len(pending) == 0 {
		result.Empty = true
		return result, nil
	}
	target.Progress = func(job ingestion.Job) {
		show(fmt.Sprintf("Ingest · %s · attempt %d", job.State, job.Attempt))
	}
	for index := 0; index < len(pending); index++ {
		item := pending[index]
		if item.StateID != "" && item.StateID != target.Target.Identity {
			result.Failures = append(result.Failures, fmt.Errorf("submission %s belongs to another server database; retained for recovery", item.Request.SubmissionID))
			if item.Request.SubmissionID == reusedSubmission {
				freshItem := Pending{Request: fresh}
				if err := box.Save(freshItem); err != nil {
					return result, err
				}
				pending = append(pending, freshItem)
			}
			continue
		}
		fingerprint := options.Fingerprint(item.Request)
		ack, err := box.Acknowledgement(item.Request)
		if err != nil {
			return result, err
		}
		if fingerprint != "" && ack.Fingerprint == fingerprint && ack.StateID == target.Target.Identity && ack.ContextID != "" {
			current, err := target.Current(ctx, ack.ContextID)
			if err != nil {
				return result, err
			}
			if current {
				if err := box.Remove(item.Request.SubmissionID); err != nil {
					return result, err
				}
				result.Unchanged++
				continue
			}
		}
		item.StateID = target.Target.Identity
		if err := box.Save(item); err != nil {
			return result, err
		}
		show(fmt.Sprintf("Upload · %d/%d", index+1, len(pending)))
		receipt, err := target.Deliver(ctx, item.Request)
		if err != nil {
			result.Failures = append(result.Failures, fmt.Errorf("submission %s retained; resume with sync --retry: %w", item.Request.SubmissionID, err))
			continue
		}
		result.Uploaded++
		if err := box.Acknowledge(item.Request, fingerprint, target.Target.Identity, receipt.ContextID); err != nil {
			return result, err
		}
		if item.Request.SubmissionID == reusedSubmission {
			// A retry can commit older content without becoming the stream head.
			// Publish the fresh collection only when that saved payload is stale.
			current, err := target.Current(ctx, receipt.ContextID)
			if err != nil {
				return result, err
			}
			if !current {
				freshItem := Pending{Request: fresh}
				if err := box.Save(freshItem); err != nil {
					return result, err
				}
				pending = append(pending, freshItem)
			}
		}
		if err := box.Remove(item.Request.SubmissionID); err != nil {
			return result, err
		}
	}
	return result, nil
}
