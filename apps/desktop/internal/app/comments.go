package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/flexdinesh/diffx/apps/desktop/internal/design"
	"github.com/flexdinesh/diffx/apps/desktop/internal/diffview"
	"github.com/flexdinesh/diffx/apps/desktop/internal/workspace"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func commentOutdated(comment goclient.ReviewComment) bool {
	return comment.Placement == nil || comment.Placement.Applicability != "anchored" && comment.Placement.Applicability != "moved"
}

func commentAtFile(comment goclient.ReviewComment) bool {
	return comment.Target != nil && *comment.Target == "file" || commentOutdated(comment)
}

func (a *application) visibleComments() []goclient.ReviewComment {
	out := make([]goclient.ReviewComment, 0, len(a.state.Comments))
	for _, comment := range a.state.Comments {
		if a.commentFilter == "Open" && comment.Status != "open" || a.commentFilter == "Resolved" && comment.Status != "resolved" || a.commentFilter == "Outdated" && !commentOutdated(comment) {
			continue
		}
		out = append(out, comment)
	}
	slices.SortStableFunc(out, func(a, b goclient.ReviewComment) int {
		if commentOutdated(a) != commentOutdated(b) {
			if commentOutdated(a) {
				return 1
			}
			return -1
		}
		if a.CreatedAt < b.CreatedAt {
			return -1
		}
		if a.CreatedAt > b.CreatedAt {
			return 1
		}
		return strings.Compare(a.Id, b.Id)
	})
	return out
}

func (a *application) draftKeys() []string {
	keys := make([]string, 0, len(a.drafts))
	for key := range a.drafts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (a *application) beginDraft(file goclient.ChangedFile, selection *diffview.ServerRange) {
	if !a.commentsEnabled() || a.state.Diff == nil {
		return
	}
	request := goclient.CreateComment{DiffId: a.state.Diff.Id, VersionId: a.state.Diff.VersionId, Scope: a.state.Scope, FileId: file.Id, FileVersion: file.Fingerprint, Side: goclient.CreateCommentSideAdditions}
	if selection == nil {
		target := goclient.CreateCommentTargetFile
		request.Target = &target
	} else {
		request.Start, request.End = selection.Start, selection.End
		if selection.Side == editor.DiffOld {
			request.Side = goclient.CreateCommentSideDeletions
		}
	}
	identity := []string{contextID(a.state), request.DiffId, request.VersionId, string(request.Scope), file.Id, string(request.Side), fmt.Sprint(request.Start), fmt.Sprint(request.End)}
	encoded, _ := json.Marshal(identity)
	key := string(encoded)
	if a.drafts[key] == nil {
		a.drafts[key] = &draft{ContextID: contextID(a.state), Request: request}
	}
	a.activeDraft = key
	a.selectedFile = file.Id
	a.showCommentEditor()
	a.drafts[key].Focus.Focus()
}

func (a *application) editComment(comment goclient.ReviewComment) {
	key := "edit:" + comment.Id
	if a.drafts[key] == nil {
		a.drafts[key] = &draft{ContextID: contextID(a.state), Body: comment.Body, EditingID: comment.Id, Request: goclient.CreateComment{Scope: comment.Scope, DiffId: comment.DiffId, VersionId: comment.VersionId}}
	}
	a.activeDraft = key
	a.showCommentEditor()
	a.drafts[key].Focus.Focus()
}

func (a *application) showCommentEditor() {
	// MyGo's accessibility tree treats Code as a leaf and omits its annotation
	// controls. Open the normal native pane for an accessible keyboard editor.
	a.sidebarTab = "Comments"
	a.fileDrawer = a.viewportWidth > 0 && a.viewportWidth < 1012 || a.prefs.FilePaneCollapsed
	a.projectDrawer = false
}

func (a *application) saveDraft(key string) {
	d := a.drafts[key]
	if d == nil || d.Saving != 0 || a.controller == nil {
		return
	}
	if strings.TrimSpace(d.Body) == "" {
		d.Error = "Write a comment before saving."
		d.Focus.Focus()
		return
	}
	d.Error = ""
	a.actionError = ""
	if d.EditingID != "" {
		body := d.Body
		d.Saving = a.controller.UpdateCommentFor(d.ContextID, d.EditingID, goclient.UpdateComment{Body: &body})
	} else {
		body := d.Request
		body.Body = d.Body
		d.Saving = a.controller.CreateCommentFor(d.ContextID, body)
	}
	if d.Saving == 0 {
		d.Error = "Comment was not admitted. Retry after reconnecting."
		return
	}
	a.pending[d.Saving] = pendingAction{draftKey: key, body: d.Body}
}

func (a *application) draftEditor(c *ui.Context, key string) ui.Element {
	d := a.drafts[key]
	if d == nil {
		return ui.Element{}
	}
	return ui.Column(c.Key("draft:" + key)).FillWidth().Padding(12).Gap(8).Background(c.Theme().Surface).Children(func() {
		label := "Review comment"
		if d.EditingID != "" {
			label = "Edit review comment"
		} else if d.Request.Target != nil && *d.Request.Target == "file" {
			label = "File review comment"
		} else {
			label = fmt.Sprintf("Review comment · %s %d–%d", d.Request.Side, d.Request.Start, d.Request.End)
		}
		design.Text(c, label, design.Heading)
		if d.ContextID != contextID(a.state) || d.Request.Scope != a.state.Scope {
			design.Status(c, "Draft belongs to an earlier review or comparison. Save keeps its original target.", design.StatusWarning)
		}
		input := ui.TextArea(c, &d.Body).Label("Comment body").Placeholder("Describe the change to make").Bind(&d.Focus).Lines(3, 12).Disabled(d.Saving != 0).FillWidth()
		if input.Focused() {
			a.activeDraft = key
			if c.Shortcut(ui.Cmd, ui.KeyEnter) {
				a.saveDraft(key)
			}
		}
		if d.Error != "" {
			design.Status(c, d.Error, design.StatusError)
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if design.Button(c, "Save comment", design.Primary).Disabled(d.Saving != 0).Clicked() {
				a.saveDraft(key)
			}
			if design.Button(c, "Cancel", design.Quiet).Disabled(d.Saving != 0).Clicked() {
				if d.Body != "" {
					a.requestConfirm("Discard comment draft?", "This unsaved text will be removed.", func() {
						delete(a.drafts, key)
						if a.activeDraft == key {
							a.activeDraft = ""
						}
					})
				} else {
					delete(a.drafts, key)
					if a.activeDraft == key {
						a.activeDraft = ""
					}
				}
			}
			if d.Saving != 0 {
				design.Text(c, "Saving comment", design.Muted)
			}
		})
	})
}

func (a *application) commentCard(c *ui.Context, comment goclient.ReviewComment, sidebar bool) ui.Element {
	return ui.Column(c.Key("comment:" + comment.Id)).FillWidth().Padding(12).Gap(8).Background(c.Theme().Surface).Children(func() {
		ui.Row(c).Gap(8).Children(func() {
			label := "Unknown status"
			if comment.Status != "" {
				label = strings.ToUpper(string(comment.Status)[:1]) + string(comment.Status)[1:]
			}
			role := design.StatusNeutral
			if comment.Status == "resolved" {
				role = design.StatusSuccess
			}
			design.Status(c, label, role)
			if sidebar {
				design.Text(c, "Comparison: "+string(comment.Scope), design.Muted)
			}
			if commentOutdated(comment) {
				design.Status(c, "Outdated", design.StatusWarning)
			} else if comment.Placement != nil && comment.Placement.Applicability == "moved" {
				design.Status(c, "Moved", design.StatusNeutral)
			}
			ui.Box(c).Grow(1)
			if sidebar && design.Button(c, comment.Path, design.Quiet).Label("Go to comment "+comment.Id).MaxWidth(200).Clicked() {
				a.navigateComment(comment)
			}
		})
		if comment.Status == "resolved" && !a.commentExpanded[comment.Id] {
			if design.Button(c, "Show resolved comment", design.Quiet).Clicked() {
				a.commentExpanded[comment.Id] = true
			}
			return
		}
		if d := a.drafts["edit:"+comment.Id]; d != nil && !sidebar && !a.draftsInSidebar() {
			a.draftEditor(c, "edit:"+comment.Id)
			return
		}
		design.Text(c, comment.Body, design.Body).Selectable().FillWidth()
		if commentOutdated(comment) {
			placement := "Placement unknown in this snapshot"
			if comment.Placement != nil {
				switch comment.Placement.Applicability {
				case "outdated":
					placement = "Code changed or left this diff"
				case "unchanged":
					placement = "Code still exists outside the changed lines"
				case "other-scope":
					placement = "Comment belongs to another comparison"
				}
			}
			design.Text(c, placement, design.Muted)
			if comment.Code != "" {
				design.Text(c, comment.Code, design.Code).Selectable().FillWidth()
			}
		}
		if comment.CarriedFrom != nil {
			design.Text(c, fmt.Sprintf("Kept open from %s lines %d–%d", comment.CarriedFrom.Side, comment.CarriedFrom.Start, comment.CarriedFrom.End), design.Muted)
		}
		if !a.commentsEnabled() {
			return
		}
		ui.Row(c).Gap(4).Wrap().Children(func() {
			if design.Button(c, "Edit", design.Quiet).Disabled(a.state.Saving).Clicked() {
				a.editComment(comment)
			}
			label := "Resolve"
			status := goclient.UpdateCommentStatusResolved
			if comment.Status == "resolved" {
				label = "Reopen"
				status = goclient.UpdateCommentStatusOpen
			}
			if design.Button(c, label, design.Quiet).Disabled(a.state.Saving).Clicked() {
				a.commentExpanded[comment.Id] = true
				a.track(a.controller.UpdateComment(comment.Id, goclient.UpdateComment{Status: &status}))
			}
			if comment.Status == "open" && comment.Placement != nil && (comment.Placement.Applicability == "outdated" || comment.Placement.Applicability == "unchanged") {
				if design.Button(c, "Keep open", design.Quiet).Disabled(a.state.Saving).Clicked() {
					a.track(a.controller.CarryComment(comment.Id))
				}
			}
			if design.Button(c, "Copy", design.Quiet).Label("Copy comment " + comment.Id).Clicked() {
				a.exportReview(workspace.ExportOptions{CommentID: comment.Id, IncludeResolved: comment.Status == "resolved"})
			}
			if design.Button(c, "Delete", design.Destructive).Disabled(a.state.Saving).Clicked() {
				a.requestConfirm("Delete comment?", comment.Body, func() { a.track(a.controller.DeleteComment(comment.Id)) })
			}
		})
	})
}

func (a *application) commentsPanel(c *ui.Context) {
	if a.state.CommitID != "" {
		design.Text(c, "Commits are read-only. Open Branch changes to review.", design.Secondary).Padding(12)
		if design.Button(c, "View Branch changes", design.Default).Clicked() {
			a.branchChanges()
		}
		return
	}
	if !a.commentsEnabled() {
		design.Text(c, "Comments are unavailable for this source.", design.Secondary).Padding(12)
		return
	}
	ui.Row(c).Padding(8).Gap(4).Wrap().Children(func() {
		for _, filter := range []string{"Open", "Resolved", "Outdated", "All"} {
			if design.Button(c, filter, design.Quiet).Selected(a.commentFilter == filter).Clicked() {
				a.commentFilter = filter
			}
		}
	})
	ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&a.commentScroll).Children(func() {
		keys := make([]string, 0, len(a.drafts))
		for key := range a.drafts {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			a.draftEditor(c, key)
		}
		comments := a.visibleComments()
		outdatedHeading := false
		if len(comments) == 0 {
			design.Text(c, "No "+strings.ToLower(a.commentFilter)+" comments", design.Muted).Padding(12)
		}
		for _, comment := range comments {
			if !commentOutdated(comment) && !outdatedHeading && comment.Id == comments[0].Id {
				design.Text(c, "Current comments", design.Heading).Padding(8, 12)
			}
			if commentOutdated(comment) && !outdatedHeading {
				design.Text(c, "Outdated comments", design.Heading).Padding(8, 12)
				outdatedHeading = true
			}
			a.commentCard(c, comment, true)
		}
	})
	ui.Row(c).Padding(8).Gap(4).Wrap().Children(func() {
		if design.Button(c, "Copy review", design.Default).Disabled(a.state.Saving).Clicked() {
			a.exportReview(workspace.ExportOptions{})
		}
		design.Button(c, "More", design.Quiet).Label("Comment actions").Menu(func(m *ui.Menu) {
			if m.Item("Copy including resolved").Chosen() {
				a.exportReview(workspace.ExportOptions{IncludeResolved: true})
			}
			if m.Item("Resolve visible open comments").Disabled(a.state.Saving).Chosen() {
				status := goclient.UpdateCommentStatusResolved
				for _, comment := range a.visibleComments() {
					if comment.Status == "open" {
						a.track(a.controller.UpdateComment(comment.Id, goclient.UpdateComment{Status: &status}))
					}
				}
			}
			m.Separator()
			for _, status := range []string{"open", "resolved", "outdated", "all"} {
				if m.Item("Delete " + status + " comments").Disabled(a.state.Saving).Chosen() {
					a.requestConfirm("Delete "+status+" comments?", "Comments will be permanently removed from this review.", func() { a.track(a.controller.DeleteComments(status)) })
				}
			}
		})
	})
}

func (a *application) navigateComment(comment goclient.ReviewComment) {
	if a.state.Current == nil || a.state.Session == nil || a.state.CommitID != "" || a.controller == nil {
		return
	}
	if !workspace.ScopeAvailable(a.state.Session.Capabilities, comment.Scope) {
		a.actionError = "Comment comparison is unavailable in this snapshot. Original code remains in Comments."
		return
	}
	target := &commentTarget{owner: reviewOwner{ContextID: contextID(a.state), Scope: comment.Scope}, path: comment.Path}
	if comment.Placement != nil && !commentAtFile(comment) {
		r := commentRange(comment)
		target.rangeTarget = &r
	}
	a.commentTarget = target
	if comment.Scope != a.state.Scope {
		a.controller.SetScope(comment.Scope)
		return
	}
	a.resolveCommentTarget()
}

func (a *application) resolveCommentTarget() {
	target := a.commentTarget
	if target == nil || target.owner != a.snapshotKey || a.state.Diff == nil {
		return
	}
	for _, file := range a.state.Diff.Files {
		if file.Path == target.path || stringValue(file.OldPath) == target.path {
			if a.prefs.HideReviewed {
				a.prefs.HideReviewed = false
				a.persist()
			}
			a.fileFilter = ""
			a.selectFile(file.Id)
			a.commentTarget = target
			return
		}
	}
	a.commentTarget = nil
	a.actionError = "Comment file is outside this captured comparison. Original code remains in Comments."
}

func commentRange(comment goclient.ReviewComment) diffview.ServerRange {
	r := diffview.ServerRange{}
	if comment.Placement != nil {
		r.Start, r.End = comment.Placement.Start, comment.Placement.End
		if comment.Placement.Side == "deletions" {
			r.Side = editor.DiffOld
		} else {
			r.Side = editor.DiffNew
		}
	}
	return r
}

func (a *application) exportReview(options workspace.ExportOptions) {
	if a.controller == nil || !a.commentsEnabled() {
		return
	}
	owner := a.snapshotKey
	a.controller.ExportCommentsWithOptions(options, func(text string, err error) {
		a.dispatch(func() {
			if a.closed || a.snapshotKey != owner {
				return
			}
			if err != nil {
				a.actionError = "Copy review: " + err.Error()
				return
			}
			a.clipboard.WriteClipboard(text)
			a.feedback = "Review copied"
		})
	})
}
