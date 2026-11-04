package workspace

import (
	"context"
	"errors"
	"slices"

	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func (c *Controller) OpenContext(id string) { c.open(id, "", "") }
func (c *Controller) openIfEmpty(id string) { c.openSelected(id, "", "", true) }
func (c *Controller) open(id string, scope goclient.DiffMode, commit string) {
	c.openSelected(id, scope, commit, false)
}
func (c *Controller) openSelected(id string, scope goclient.DiffMode, commit string, onlyEmpty bool) {
	c.mu.Lock()
	if !c.aliveLocked() || id == "" || onlyEmpty && c.selectedID != "" {
		c.mu.Unlock()
		return
	}
	c.replaceOwnerLocked(id, scope, commit)
	ctx, generation := c.selected, c.generation
	c.state.Current = nil
	c.state.Session = nil
	c.state.Commits = nil
	c.state.CommitsOmitted = 0
	c.taskLocked(func() { c.loadContext(ctx, generation, id, scope, commit) })
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) replaceOwnerLocked(id string, scope goclient.DiffMode, commit string) {
	if c.selectCancel != nil {
		c.selectCancel()
	}
	c.selected, c.selectCancel = context.WithCancel(c.ctx)
	c.generation++
	c.reviewEpoch++
	c.reviewActive, c.reviewAgain = false, false
	c.selectedID = id
	c.state.Scope = scope
	c.state.CommitID = commit
	c.state.Diff = nil
	c.state.Patches = map[string]goclient.FilePatch{}
	c.state.Contents = map[string]goclient.FileContents{}
	c.state.PatchLoading = map[string]bool{}
	c.state.ContentLoading = map[string]bool{}
	c.state.FileErrors = map[string]string{}
	c.cacheOrder = nil
	c.state.Comments = nil
	c.state.Marks = nil
	c.state.NewerContextID = ""
	c.state.Error = ""
	c.state.ReviewError = ""
	c.state.Loading = true
}

func (c *Controller) loadContext(ctx context.Context, generation uint64, id string, scope goclient.DiffMode, commit string) {
	r, err := c.client.GetContextWithResponse(ctx, id)
	if err == nil {
		err = responseError(r, 200)
	}
	if err == nil && r.JSON200 == nil {
		err = errors.New("context response missing")
	}
	if err != nil {
		c.loadFailure(generation, err)
		return
	}
	current := r.JSON200
	if scope == "" {
		c.mu.Lock()
		saved := c.state.SavedScopes[StreamKey(*current)]
		c.mu.Unlock()
		if slices.Contains(current.Capabilities.Diff.Scopes.Values, saved) {
			scope = saved
		} else {
			scope = DefaultScope(*current)
		}
	}
	c.mu.Lock()
	if !c.ownsLocked(generation) {
		c.mu.Unlock()
		return
	}
	c.state.Current = current
	c.state.Scope = scope
	c.updateNewerLocked()
	c.mu.Unlock()
	c.emit()
	if current.Availability == "unavailable" {
		c.loadFailure(generation, errors.New("snapshot unavailable"))
		return
	}
	rsession, err := c.client.GetSessionV2WithResponse(ctx, id)
	if err == nil {
		err = responseError(rsession, 200)
	}
	if err == nil && rsession.JSON200 == nil {
		err = errors.New("session response missing")
	}
	if err != nil {
		c.loadFailure(generation, err)
		return
	}
	c.mu.Lock()
	if !c.ownsLocked(generation) {
		c.mu.Unlock()
		return
	}
	c.state.Session = rsession.JSON200
	if current.Commits != nil && current.Commits.Count > 0 {
		c.taskLocked(func() { c.loadCommits(ctx, generation, id) })
	}
	c.mu.Unlock()
	c.emit()
	if commit == "" && !ScopeAvailable(rsession.JSON200.Capabilities, scope) {
		c.loadFailure(generation, errors.New("comparison scope unavailable in this snapshot"))
		return
	}
	c.loadDiff(ctx, generation, id, scope, commit)
}

func (c *Controller) loadCommits(ctx context.Context, generation uint64, id string) {
	r, err := c.client.ListCommitsV2WithResponse(ctx, id)
	if err == nil {
		err = responseError(r, 200)
	}
	if err == nil && r.JSON200 == nil {
		err = errors.New("commit list response missing")
	}
	c.mu.Lock()
	if !c.ownsLocked(generation) {
		c.mu.Unlock()
		return
	}
	if err != nil {
		c.state.Error = err.Error()
	} else {
		c.state.Commits = r.JSON200.Commits
		c.state.CommitsOmitted = r.JSON200.Omitted
	}
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) loadDiff(ctx context.Context, generation uint64, id string, scope goclient.DiffMode, commit string) {
	var diff *goclient.RepositoryDiff
	var err error
	if commit != "" {
		r, e := c.client.GetCommitDiffV2WithResponse(ctx, id, commit)
		err = e
		if err == nil {
			err = responseError(r, 200)
			diff = r.JSON200
		}
	} else {
		r, e := c.client.GetCurrentDiffV2WithResponse(ctx, id, &goclient.GetCurrentDiffV2Params{Scope: scope})
		err = e
		if err == nil {
			err = responseError(r, 200)
			diff = r.JSON200
		}
	}
	if err == nil && diff == nil {
		err = errors.New("diff response missing")
	}
	if err == nil && (commit != "" && diff.Mode != "commit" || commit == "" && string(diff.Mode) != string(scope)) {
		err = errors.New("server returned a different comparison")
	}
	if err != nil {
		c.loadFailure(generation, err)
		return
	}
	c.mu.Lock()
	if !c.ownsLocked(generation) {
		c.mu.Unlock()
		return
	}
	diff.Files = FilesInTreeOrder(diff.Files)
	c.state.Diff = diff
	c.state.Loading = false
	c.state.Error = ""
	c.mu.Unlock()
	c.emit()
	c.reloadReview()
}

func (c *Controller) loadFailure(generation uint64, err error) {
	c.mu.Lock()
	if !c.ownsLocked(generation) {
		c.mu.Unlock()
		return
	}
	c.state.Loading = false
	c.state.Error = err.Error()
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) SetScope(scope goclient.DiffMode) {
	c.mu.Lock()
	if !c.aliveLocked() || c.state.Current == nil || c.state.Session == nil {
		c.mu.Unlock()
		return
	}
	if !ScopeAvailable(c.state.Session.Capabilities, scope) {
		c.state.Error = "comparison scope unavailable in this snapshot"
		c.mu.Unlock()
		c.emit()
		return
	}
	if c.state.Scope == scope && c.state.CommitID == "" {
		c.mu.Unlock()
		return
	}
	id := c.state.Current.Id
	c.state.SavedScopes[StreamKey(*c.state.Current)] = scope
	c.replaceOwnerLocked(id, scope, "")
	ctx, generation := c.selected, c.generation
	c.taskLocked(func() { c.loadDiff(ctx, generation, id, scope, "") })
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) OpenCommit(oid string) {
	c.mu.Lock()
	if !c.aliveLocked() || c.state.Current == nil {
		c.mu.Unlock()
		return
	}
	if oid == "" {
		scope := c.state.Scope
		c.mu.Unlock()
		c.SetScope(scope)
		return
	}
	if !slices.ContainsFunc(c.state.Commits, func(commit goclient.CommitSummary) bool { return commit.Oid == oid }) {
		c.state.Error = "commit was not captured in this snapshot"
		c.mu.Unlock()
		c.emit()
		return
	}
	id, scope := c.state.Current.Id, c.state.Scope
	c.replaceOwnerLocked(id, scope, oid)
	ctx, generation := c.selected, c.generation
	c.taskLocked(func() { c.loadDiff(ctx, generation, id, scope, oid) })
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) reloadReview() {
	c.mu.Lock()
	if !c.aliveLocked() || c.state.Current == nil || c.state.Session == nil || c.state.Diff == nil || c.state.CommitID != "" {
		c.mu.Unlock()
		return
	}
	writePending := false
	for _, result := range c.state.Results {
		if !result.Done && result.ContextID == c.state.Current.Id {
			writePending = true
			break
		}
	}
	if c.reviewActive || writePending {
		c.reviewAgain = true
		c.mu.Unlock()
		return
	}
	c.reviewActive = true
	id, scope, ctx, generation := c.state.Current.Id, c.state.Scope, c.selected, c.generation
	commentsEnabled := c.state.Session.Capabilities.Review.Comments.State == "enabled"
	c.reviewEpoch++
	epoch := c.reviewEpoch
	c.taskLocked(func() {
		var list []goclient.ReviewComment
		var e1 error
		if commentsEnabled {
			comments, err := c.client.ListCommentsV2WithResponse(ctx, id)
			e1 = err
			if e1 == nil {
				e1 = responseError(comments, 200)
			}
			if e1 == nil && comments.JSON200 == nil {
				e1 = errors.New("comments response missing")
			}
			if e1 == nil {
				list = comments.JSON200.Comments
			}
		}
		marks, e2 := c.client.ListReviewMarksV2WithResponse(ctx, id, &goclient.ListReviewMarksV2Params{Scope: scope})
		if e2 == nil {
			e2 = responseError(marks, 200)
		}
		if e2 == nil && marks.JSON200 == nil {
			e2 = errors.New("review marks response missing")
		}
		c.mu.Lock()
		if !c.ownsLocked(generation) {
			c.mu.Unlock()
			return
		}
		c.reviewActive = false
		again := c.reviewAgain || epoch != c.reviewEpoch
		c.reviewAgain = false
		if epoch != c.reviewEpoch {
			c.mu.Unlock()
			c.reloadReview()
			return
		}
		c.state.ReviewError = ""
		if e1 == nil {
			c.state.Comments = list
		} else {
			c.state.ReviewError = e1.Error()
		}
		if e2 == nil {
			c.state.Marks = marks.JSON200.Marks
		} else {
			if c.state.ReviewError != "" {
				c.state.ReviewError += "\n"
			}
			c.state.ReviewError += e2.Error()
		}
		c.mu.Unlock()
		c.emit()
		if again {
			c.reloadReview()
		}
	})
	c.mu.Unlock()
}
