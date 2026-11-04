package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/flexdinesh/diffx/apps/desktop/internal/design"
	"github.com/flexdinesh/diffx/apps/desktop/internal/diffview"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func (a *application) drawDiffs(c *ui.Context, files []goclient.ChangedFile) {
	a.diffList.Key = func(i int) any { return files[i].Id }
	a.diffList.Label = func(i int) string { return files[i].Path }
	if a.jumpFile != "" {
		if i := slices.IndexFunc(files, func(f goclient.ChangedFile) bool { return f.Id == a.jumpFile }); i >= 0 {
			a.diffList.ScrollTo(i, ui.Start)
		}
		a.jumpFile = ""
	}
	ui.List(c.Key("diff-files"), &a.diffList, len(files), func(i int) {
		file := files[i]
		ui.Column(c.Key(file.Id)).FillWidth().Shrink(0).Padding(0, 12, 12).Children(func() { a.drawFile(c, file) })
	}).Label("File diffs").Grow(1).MinHeight(0).FillWidth()
	first, _ := a.diffList.Visible()
	if first >= 0 && first < len(files) && first != a.lastVisibleFirst {
		a.lastVisibleFirst = first
		a.selectedFile = files[first].Id
		a.revealFile = files[first].Id
		c.Invalidate()
	}
}

func (a *application) drawFile(c *ui.Context, file goclient.ChangedFile) {
	ui.Column(c).FillWidth().MinWidth(0).Background(c.Theme().Background).Children(func() {
		design.Toolbar(c).FillWidth().Children(func() {
			label := "Collapse " + file.Path
			if a.fileCollapsed[file.Id] {
				label = "Expand " + file.Path
			}
			if design.Button(c, file.Path, design.Quiet).Label(label).Tooltip(file.Path).Grow(1).MinWidth(0).Justify(ui.Start).Clicked() {
				a.fileCollapsed[file.Id] = !a.fileCollapsed[file.Id]
				a.selectedFile = file.Id
			}
			design.Text(c, fmt.Sprintf("%s +%d -%d", file.Status, file.Additions, file.Deletions), design.Code).Shrink(0)
			if a.state.CommitID != "" {
				if design.Button(c, "Open in Branch changes", design.Default).Clicked() {
					a.branchChanges()
				}
			} else {
				if a.mutableReview() {
					label := "Reviewed"
					if a.reviewed(file) {
						label = "Reviewed: yes"
					}
					if design.Button(c, label, design.Default).Label("Toggle reviewed " + file.Path).Selected(a.reviewed(file)).Disabled(a.markPending(file.Id)).Clicked() {
						a.markFile(file, false)
					}
				}
				if a.commentsEnabled() && design.Button(c, "Comment", design.Quiet).Label("Leave review comment on file "+file.Path).Clicked() {
					a.beginDraft(file, nil)
				}
			}
		})
		if a.fileCollapsed[file.Id] {
			return
		}
		if old := stringValue(file.OldPath); old != "" && old != file.Path {
			design.Text(c, old+" → "+file.Path, design.Code).Padding(8, 12)
		}
		if a.state.Session != nil && a.state.Session.Capabilities.Diff.StagingMetadata.State == "enabled" && a.state.CommitID == "" {
			design.Text(c, fmt.Sprintf("Index %s · Working tree %s", stagingLabel(file.IndexStatus), stagingLabel(file.WorktreeStatus)), design.Muted).Padding(4, 12)
		}
		if a.commentsEnabled() {
			a.fileAnnotations(c, file)
		}
		if file.Binary {
			design.Text(c, "Binary file. Text preview and line comments are unavailable.", design.Secondary).Padding(12)
			return
		}
		patch, loaded := a.state.Patches[file.Id]
		if !loaded {
			if err := a.state.FileErrors[file.Id]; err != "" {
				design.Status(c, err, design.StatusError).Padding(12)
				if design.Button(c, "Retry preview", design.Default).Label("Retry preview " + file.Path).Clicked() {
					a.controller.LoadPatch(file.Id)
				}
			} else {
				design.Text(c, "Loading captured patch", design.Muted).Padding(12)
				if !a.state.PatchLoading[file.Id] {
					a.controller.LoadPatch(file.Id)
				}
			}
			return
		}
		if message := stringValue(patch.Message); message != "" {
			design.Status(c, message, design.StatusWarning).Padding(12)
		}
		if patch.Patch == "" {
			design.Text(c, "No text patch was captured for this file. File metadata and review actions remain available.", design.Secondary).Padding(12)
			return
		}
		fv := a.fileViews[file.Id]
		if fv == nil || fv.raw != patch.Patch {
			fv = &fileView{raw: patch.Patch}
			a.fileViews[file.Id] = fv
			p, err := diffview.NewPresentation(patch.Patch)
			if err != nil {
				fv.error = err.Error()
			} else {
				fv.view = diffview.NewReadOnlyView(p, editor.DiffSplit, a.prefs.Wrap, func(r diffview.ServerRange, selected bool) {
					a.selectedFile = file.Id
					if selected {
						a.lineSelections[file.Id] = r
					} else {
						delete(a.lineSelections, file.Id)
					}
				})
				if r, ok := a.lineSelections[file.Id]; ok {
					if err := fv.view.SetSelection(r); err != nil {
						delete(a.lineSelections, file.Id)
					}
				}
			}
		}
		if fv.error != "" {
			design.Status(c, "Stored patch cannot be rendered: "+fv.error, design.StatusError).Padding(12)
			return
		}
		v := fv.view
		options := a.renderOptions(c, file.Path)
		if err := v.SetOptions(options); err != nil {
			design.Status(c, err.Error(), design.StatusError)
		}
		v.SetLineSelectionEnabled(a.commentsEnabled())
		v.SetGutterCommentHandler(nil)
		if a.commentsEnabled() {
			v.SetGutterCommentHandler(func(r diffview.ServerRange) { a.beginDraft(file, &r) })
		}
		contents := patch.Contents
		if full, ok := a.state.Contents[file.Id]; ok {
			contents = &full
		}
		if contents != nil {
			if err := v.SetCompleteContents(diffview.CompleteContentsFromAPI(contents)); err != nil {
				design.Status(c, "Full context does not match the stored patch: "+err.Error(), design.StatusError).Padding(8, 12)
			} else {
				fv.contents = contents
			}
		}
		ui.Row(c).Gap(8).Padding(4, 12).Wrap().Children(func() {
			if a.contentsEnabled() && fv.contents == nil {
				label := "Load full context"
				if a.state.ContentLoading[file.Id] {
					label = "Loading full context"
				}
				if design.Button(c, label, design.Quiet).Label("Load full context " + file.Path).Disabled(a.state.ContentLoading[file.Id]).Clicked() {
					a.controller.LoadContents(file.Id)
				}
			} else if !a.contentsEnabled() {
				design.Text(c, "Full context unavailable for this source", design.Muted)
			}
			if fv.contents != nil && design.Button(c, "Collapse context", design.Quiet).Clicked() {
				v.Collapse()
			}
			if a.commentsEnabled() {
				if r, ok := v.Selection(); ok {
					if design.Button(c, fmt.Sprintf("Comment on lines %d–%d", r.Start, r.End), design.Default).Clicked() {
						a.beginDraft(file, &r)
					}
				}
			}
		})
		if err := a.state.FileErrors[file.Id]; err != "" {
			design.Status(c, err, design.StatusError).Padding(8, 12)
		}
		a.bindAnnotations(v, file)
		v.DrawContent(c).FillWidth().Shrink(0)
		if err := v.Error(); err != nil {
			design.Status(c, err.Error(), design.StatusError).Padding(8, 12)
		}
		if len(v.Presentation().Patch().Hunks) == 0 {
			design.Text(c, "Metadata-only change. No changed text lines.", design.Secondary).Padding(12)
		}
	})
}

func stagingLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unchanged"
	}
	return value
}

func (a *application) markPending(fileID string) bool {
	for _, pending := range a.pending {
		if pending.advanceFile == fileID {
			return true
		}
	}
	return false
}

func (a *application) renderOptions(c *ui.Context, path string) diffview.Options {
	layout := editor.DiffSplit
	if a.prefs.Layout == "unified" {
		layout = editor.DiffUnified
	}
	inline := editor.DiffInlineWordAlt
	switch a.prefs.LineDiff {
	case "word":
		inline = editor.DiffInlineWord
	case "char":
		inline = editor.DiffInlineChar
	case "none":
		inline = editor.DiffInlineNone
	}
	family := a.prefs.FontFamily
	if family == "" {
		family = "monospace"
	}
	var language *editor.Language
	if a.prefs.Syntax {
		language = editor.LanguageFor(path)
	}
	indicators := editor.DiffBars
	if a.prefs.Indicators == "classic" {
		indicators = editor.DiffClassic
	} else if a.prefs.Indicators == "none" {
		indicators = editor.DiffNoIndicators
	}
	return diffview.Options{Layout: layout, Wrap: a.prefs.Wrap, Inline: inline, Language: language, Theme: design.CodeTheme(a.prefs.CodeTheme, a.prefs.Theme, c.Theme().Dark), Font: ui.Font{Family: family, Size: a.prefs.FontSize}, LineHeight: 1.5, TabSize: a.prefs.TabSize, NoBackground: a.prefs.NoBackground, NoLineNumbers: a.prefs.NoLineNumbers, Indicators: indicators}
}

func (a *application) fileAnnotations(c *ui.Context, file goclient.ChangedFile) {
	for _, comment := range a.visibleComments() {
		if comment.Path != file.Path && comment.Path != stringValue(file.OldPath) {
			continue
		}
		atFile := commentAtFile(comment)
		if !atFile {
			if fv := a.fileViews[file.Id]; fv != nil && fv.view != nil {
				mapped, err := fv.view.Presentation().Remap(commentRange(comment))
				atFile = err != nil || !mapped.Visible
			}
		}
		if atFile {
			a.commentCard(c, comment, false)
		}
	}
	for _, key := range a.draftKeys() {
		d := a.drafts[key]
		if !a.draftsInSidebar() && d.EditingID == "" && d.ContextID == contextID(a.state) && d.Request.Scope == a.state.Scope && d.Request.FileId == file.Id && (d.Request.Target != nil && *d.Request.Target == "file") {
			a.draftEditor(c, key)
		}
	}
}

func (a *application) bindAnnotations(v *diffview.ReadOnlyView, file goclient.ChangedFile) {
	placements := []diffview.Placement{}
	if a.commentsEnabled() {
		for _, comment := range a.visibleComments() {
			if (comment.Path == file.Path || comment.Path == stringValue(file.OldPath)) && !commentAtFile(comment) {
				r := commentRange(comment)
				if _, err := v.Presentation().Remap(r); err == nil {
					placements = append(placements, diffview.Placement{Key: comment.Id, Label: comment.Body, Range: r, Height: 120})
				}
			}
		}
		for _, key := range a.draftKeys() {
			d := a.drafts[key]
			if !a.draftsInSidebar() && d.EditingID == "" && d.ContextID == contextID(a.state) && d.Request.Scope == a.state.Scope && d.Request.FileId == file.Id && d.Request.Target == nil {
				side := editor.DiffNew
				if d.Request.Side == "deletions" {
					side = editor.DiffOld
				}
				placements = append(placements, diffview.Placement{Key: "draft:" + key, Label: "Review comment draft", Range: diffview.ServerRange{Side: side, Start: d.Request.Start, End: d.Request.End}, Height: 200})
			}
		}
	}
	if err := v.SetPlacements(placements); err != nil {
		a.actionError = "Comment placement: " + err.Error()
	}
	v.SetAnnotationRenderer(func(c *ui.Context, p diffview.Placement, width float32) float32 {
		var box ui.Element
		if len(p.Key) > 6 && p.Key[:6] == "draft:" {
			box = a.draftEditor(c, p.Key[6:])
		} else {
			for _, comment := range a.state.Comments {
				if comment.Id == p.Key {
					box = a.commentCard(c, comment, false)
					break
				}
			}
		}
		if !box.Valid() {
			return 48
		}
		box.Width(width)
		return max(48, box.LayoutBounds().H)
	})
}
