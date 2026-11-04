package workspace

import (
	"context"
	"errors"
	"slices"

	"github.com/flexdinesh/diffx/packages/api/goclient"
)

type mutation struct {
	id             uint64
	contextID      string
	scope          goclient.DiffMode
	commit         string
	generation     uint64
	kind           string
	run            func(context.Context) error
	admissionError error
}

// admitLocked captures ownership before the caller returns. Writes settle in
// admission order and outlive selected-read cancellation when navigating.
func (c *Controller) admitLocked(job mutation) uint64 {
	c.nextOperation++
	job.id = c.nextOperation
	c.state.Results[job.id] = MutationResult{ID: job.id, ContextID: job.contextID, Scope: job.scope, CommitID: job.commit, Kind: job.kind}
	if !c.aliveLocked() {
		job.admissionError = errors.New("workspace closed")
	}
	if job.admissionError == nil {
		c.writes = append(c.writes, job)
		select {
		case c.writeReady <- struct{}{}:
		default:
		}
		c.pending++
		c.state.Saving = true
		c.reviewEpoch++
		return job.id
	}
	result := c.state.Results[job.id]
	result.Done = true
	result.Error = job.admissionError.Error()
	c.state.Results[job.id] = result
	return job.id
}

func (c *Controller) currentMutationLocked(kind string) mutation {
	job := mutation{kind: kind, scope: c.state.Scope, commit: c.state.CommitID, generation: c.generation}
	if c.state.Current != nil {
		job.contextID = c.state.Current.Id
	}
	if c.state.Current == nil || c.state.Diff == nil || c.state.Session == nil {
		job.admissionError = errors.New("review not loaded")
	} else if job.commit != "" {
		job.admissionError = errors.New("captured commits are read only")
	} else if !ScopeAvailable(c.state.Session.Capabilities, job.scope) {
		job.admissionError = errors.New("comparison scope unavailable in this snapshot")
	} else if kind != "mark-reviewed" && kind != "clear-reviewed" && c.state.Session.Capabilities.Review.Comments.State != "enabled" {
		job.admissionError = errors.New("review operations unavailable")
	}
	return job
}

func (c *Controller) writeLoop() {
	defer func() {
		c.mu.Lock()
		for id, result := range c.state.Results {
			if !result.Done {
				result.Done = true
				result.Error = "workspace closed before save completed"
				c.state.Results[id] = result
			}
		}
		c.pending = 0
		c.writes = nil
		c.state.Saving = false
		c.mu.Unlock()
		c.emit()
	}()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.writeReady:
			for {
				if c.ctx.Err() != nil {
					return
				}
				c.mu.Lock()
				if len(c.writes) == 0 {
					c.mu.Unlock()
					break
				}
				job := c.writes[0]
				c.writes[0] = mutation{}
				c.writes = c.writes[1:]
				c.mu.Unlock()
				err := job.run(c.ctx)
				c.mu.Lock()
				result := c.state.Results[job.id]
				result.Done = true
				if err != nil {
					result.Error = err.Error()
				}
				c.state.Results[job.id] = result
				c.pending--
				c.state.Saving = c.pending > 0
				c.reviewEpoch++
				c.mu.Unlock()
				c.emit()
				c.reloadReview()
			}
		}
	}
}

func (c *Controller) CreateComment(body goclient.CreateComment) uint64 {
	body = detached(body)
	c.mu.Lock()
	job := c.currentMutationLocked("create-comment")
	if job.admissionError == nil {
		diff := c.state.Diff
		fileIndex := slices.IndexFunc(diff.Files, func(file goclient.ChangedFile) bool {
			return file.Id == body.FileId && file.Fingerprint == body.FileVersion
		})
		if body.Scope != job.scope || body.DiffId != diff.Id || body.VersionId != diff.VersionId || fileIndex < 0 {
			job.admissionError = errors.New("draft belongs to a different comparison; save with its captured context")
		}
	}
	id := job.contextID
	job.run = func(ctx context.Context) error {
		r, err := c.client.CreateCommentV2WithResponse(ctx, id, body)
		if err != nil {
			return err
		}
		return responseError(r, 201)
	}
	operation := c.admitLocked(job)
	c.mu.Unlock()
	c.emit()
	return operation
}

// CreateCommentFor saves an existing draft using its captured context and full
// server identities, even after navigation. It never retargets that draft.
func (c *Controller) CreateCommentFor(contextID string, body goclient.CreateComment) uint64 {
	body = detached(body)
	c.mu.Lock()
	job := mutation{kind: "create-comment", contextID: contextID, scope: body.Scope, generation: c.generation}
	if contextID == "" {
		job.admissionError = errors.New("draft context missing")
	}
	job.run = func(ctx context.Context) error {
		s, err := c.client.GetSessionV2WithResponse(ctx, contextID)
		if err != nil {
			return err
		}
		if err = responseError(s, 200); err != nil {
			return err
		}
		if s.JSON200 == nil || s.JSON200.Capabilities.Review.Comments.State != "enabled" || !ScopeAvailable(s.JSON200.Capabilities, body.Scope) {
			return errors.New("draft comparison unavailable")
		}
		r, err := c.client.CreateCommentV2WithResponse(ctx, contextID, body)
		if err != nil {
			return err
		}
		return responseError(r, 201)
	}
	operation := c.admitLocked(job)
	c.mu.Unlock()
	c.emit()
	return operation
}

func (c *Controller) UpdateComment(id string, body goclient.UpdateComment) uint64 {
	body = detached(body)
	return c.mutate("update-comment", func(ctx context.Context, owner string, scope goclient.DiffMode) error {
		r, err := c.client.UpdateCommentV2WithResponse(ctx, owner, id, body)
		if err != nil {
			return err
		}
		return responseError(r, 200)
	})
}

// UpdateCommentFor preserves an edit draft owner after navigation.
func (c *Controller) UpdateCommentFor(contextID, id string, body goclient.UpdateComment) uint64 {
	body = detached(body)
	c.mu.Lock()
	job := mutation{kind: "update-comment", contextID: contextID, generation: c.generation}
	if contextID == "" {
		job.admissionError = errors.New("draft context missing")
	}
	job.run = func(ctx context.Context) error {
		r, err := c.client.UpdateCommentV2WithResponse(ctx, contextID, id, body)
		if err != nil {
			return err
		}
		return responseError(r, 200)
	}
	operation := c.admitLocked(job)
	c.mu.Unlock()
	c.emit()
	return operation
}

func (c *Controller) mutate(kind string, run func(context.Context, string, goclient.DiffMode) error) uint64 {
	c.mu.Lock()
	job := c.currentMutationLocked(kind)
	owner, scope := job.contextID, job.scope
	job.run = func(ctx context.Context) error { return run(ctx, owner, scope) }
	id := c.admitLocked(job)
	c.mu.Unlock()
	c.emit()
	return id
}

func (c *Controller) DeleteComment(id string) uint64 {
	return c.mutate("delete-comment", func(ctx context.Context, owner string, _ goclient.DiffMode) error {
		r, err := c.client.DeleteCommentV2WithResponse(ctx, owner, id)
		if err != nil {
			return err
		}
		return responseError(r, 204)
	})
}
func (c *Controller) DeleteComments(status string) uint64 {
	return c.mutate("delete-comments", func(ctx context.Context, owner string, _ goclient.DiffMode) error {
		r, err := c.client.DeleteCommentsV2WithResponse(ctx, owner, &goclient.DeleteCommentsV2Params{Status: goclient.DeleteCommentsV2ParamsStatus(status)})
		if err != nil {
			return err
		}
		return responseError(r, 200)
	})
}
func (c *Controller) CarryComment(id string) uint64 {
	return c.mutate("carry-comment", func(ctx context.Context, owner string, _ goclient.DiffMode) error {
		r, err := c.client.CarryCommentWithResponse(ctx, owner, id)
		if err != nil {
			return err
		}
		return responseError(r, 200)
	})
}
func (c *Controller) MarkReviewed(fileID string, reviewed bool) uint64 {
	c.mu.Lock()
	job := c.currentMutationLocked("mark-reviewed")
	version, fingerprint := "", ""
	if c.state.Diff != nil {
		version = c.state.Diff.VersionId
		for _, file := range c.state.Diff.Files {
			if file.Id == fileID {
				fingerprint = file.Fingerprint
				break
			}
		}
	}
	if fingerprint == "" {
		job.admissionError = errors.New("file does not belong to selected comparison")
	}
	owner, scope := job.contextID, job.scope
	job.run = func(ctx context.Context) error {
		if reviewed {
			r, err := c.client.PutReviewMarkV2WithResponse(ctx, owner, fileID, &goclient.PutReviewMarkV2Params{Scope: scope}, goclient.PutReviewMarkV2JSONRequestBody{VersionId: version, FileVersion: fingerprint})
			if err != nil {
				return err
			}
			return responseError(r, 200)
		}
		r, err := c.client.DeleteReviewMarkV2WithResponse(ctx, owner, fileID, &goclient.DeleteReviewMarkV2Params{Scope: scope})
		if err != nil {
			return err
		}
		return responseError(r, 204)
	}
	id := c.admitLocked(job)
	c.mu.Unlock()
	c.emit()
	return id
}
func (c *Controller) ClearReviewed() uint64 {
	return c.mutate("clear-reviewed", func(ctx context.Context, owner string, scope goclient.DiffMode) error {
		r, err := c.client.ClearReviewMarksV2WithResponse(ctx, owner, &goclient.ClearReviewMarksV2Params{Scope: scope})
		if err != nil {
			return err
		}
		return responseError(r, 204)
	})
}

type ExportOptions struct {
	IncludeResolved bool
	CommentID       string
}

func (c *Controller) ExportComments(done func(string, error)) {
	c.ExportCommentsWithOptions(ExportOptions{}, done)
}
func (c *Controller) ExportCommentsWithOptions(options ExportOptions, done func(string, error)) {
	c.mu.Lock()
	job := c.currentMutationLocked("export-comments")
	if !c.aliveLocked() || job.admissionError != nil {
		err := job.admissionError
		if err == nil {
			err = errors.New("workspace closed")
		}
		c.mu.Unlock()
		if done != nil {
			done("", err)
		}
		return
	}
	owner, scope, revision, ctx := job.contextID, job.scope, c.state.Diff.Revision, c.selected
	c.taskLocked(func() {
		params := &goclient.ExportCommentsV2Params{Scope: &scope, Revision: &revision, IncludeResolved: &options.IncludeResolved}
		if options.CommentID != "" {
			params.CommentId = &options.CommentID
		}
		r, err := c.client.ExportCommentsV2WithResponse(ctx, owner, params)
		text := ""
		if err == nil {
			err = responseError(r, 200)
		}
		if err == nil {
			text = string(r.Body)
		}
		if done != nil {
			done(text, err)
		}
	})
	c.mu.Unlock()
}
