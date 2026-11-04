package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/flexdinesh/diffx/apps/desktop/internal/design"
	"github.com/flexdinesh/diffx/apps/desktop/internal/workspace"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func (a *application) view(c *ui.Context) {
	design.Apply(c, a.prefs.Theme)
	a.clipboard = c.Services()
	width, height := c.Size()
	a.viewportWidth = width
	palette := design.PaletteFor(a.prefs.Theme, c.Theme().Dark)
	ui.Column(c).Fill().Gap(0).Background(palette.Color("shell-canvas")).Children(func() {
		a.header(c, width)
		ui.Column(c).Grow(1).MinHeight(0).Padding(0, 6, 6).Gap(6).Children(func() {
			if a.starting || a.startupError != "" || a.needsFolder || a.controller == nil {
				a.startup(c)
			} else {
				body := func() { a.reviewPane(c) }
				if !a.prefs.FilePaneCollapsed && width >= 1012 {
					body = func() {
						ui.Split(c.Key("file-split"), &a.prefs.FilePaneWidth, func() { a.sidebar(c) }, func() { a.reviewPane(c) }).Fill()
					}
				}
				if a.showProjects && !a.capture() && !a.prefs.ProjectPaneCollapsed && width >= 1200 {
					ui.Split(c.Key("project-split"), &a.prefs.ProjectPaneWidth, func() { a.projects(c) }, body).Grow(1).MinHeight(0)
				} else {
					ui.Column(c).Grow(1).MinHeight(0).Children(func() {
						if a.showProjects && !a.capture() && a.prefs.NavigationMode == "rail" && width >= 1200 {
							ui.Row(c).Fill().Children(func() { a.projectRail(c); ui.Column(c).Grow(1).MinWidth(0).Children(body) })
						} else {
							body()
						}
					})
				}
			}
		})
		a.footer(c, width)
	})
	a.overlays(c, width, height)
	a.keyboard(c)
	widths := [2]float32{a.prefs.ProjectPaneWidth, a.prefs.FilePaneWidth}
	if widths != a.lastWidths {
		a.lastWidths = widths
		a.persistMetadata()
	}
}

func (a *application) header(c *ui.Context, width float32) {
	design.Toolbar(c).Label("Review header").Height(36).Shrink(0).Children(func() {
		design.Text(c, "diffx", design.Heading).Shrink(0)
		if a.showProjects && !a.capture() {
			if design.Button(c, "Reviews", design.Quiet).Label("Show reviews").Clicked() {
				if width < 1200 {
					a.projectDrawer = true
					a.fileDrawer = false
				} else {
					a.prefs.ProjectPaneCollapsed = !a.prefs.ProjectPaneCollapsed
					a.persist()
				}
			}
		}
		if design.Button(c, "Files", design.Quiet).Label("Toggle files").Clicked() {
			if width < 1012 {
				a.fileDrawer = true
				a.projectDrawer = false
			} else {
				a.prefs.FilePaneCollapsed = !a.prefs.FilePaneCollapsed
				a.persist()
			}
		}
		current := a.state.Current
		if a.capture() || current != nil && workspace.IsPiped(*current) {
			design.Text(c, "Piped", design.Heading)
			if current != nil {
				design.Text(c, capturedTime(*current), design.Muted).SingleLine()
			}
		} else if current != nil {
			if design.Button(c, repositoryName(*current), design.Quiet).Label("Switch repository").MaxWidth(width / 4).Clicked() {
				a.repoPicker = true
				a.repositoryQuery = ""
			}
			if design.Button(c, branchName(*current), design.Quiet).Label("Switch branch or worktree").MaxWidth(width / 4).Clicked() {
				a.branchPicker = true
				a.branchQuery = ""
			}
			if width >= 1280 {
				design.Text(c, stringValue(current.WorktreeName), design.Muted).SingleLine().MaxWidth(160)
			}
		}
		ui.Box(c).Grow(1)
		if !a.capture() {
			unseen := slices.DeleteFunc(slices.Clone(a.state.Contexts), func(head goclient.Context) bool {
				return !workspace.CurrentWork(head) || a.state.Seen == nil || a.state.Seen[workspace.StreamKey(head)] == head.Id
			})
			slices.SortFunc(unseen, workspace.CompareActivity)
			if len(unseen) > 0 {
				design.Button(c, "New changes", design.Quiet).Label("Open unread reviews").Menu(func(m *ui.Menu) {
					for _, head := range unseen {
						if m.Item(repositoryName(head) + " · " + contextMetadata(head)).Chosen() {
							a.openLatest(head)
						}
					}
				})
			}
			if design.Button(c, "Search reviews", design.Quiet).Disabled(a.controller == nil).Clicked() {
				a.openSearch()
			}
		}
		if a.state.Bootstrap != nil && a.state.Bootstrap.Collect {
			label := "Sync checkout"
			if a.state.Bootstrap.Input == "checkouts" {
				label = "Sync all checkouts"
			}
			if design.Button(c, "Sync", design.Quiet).Label(label).Tooltip(label + " (Alt+R)").Disabled(a.state.Collecting).Clicked() {
				a.controller.Sync()
			}
		}
		if a.starting || a.state.Loading || a.state.Collecting {
			design.Text(c, "Loading", design.Muted)
		}
	})
}

func (a *application) startup(c *ui.Context) {
	ui.Column(c).Grow(1).FillWidth().Background(c.Theme().Background).Padding(24).Gap(16).Children(func() {
		title := "Opening local review"
		detail := "Connecting to the matching local Diffx server."
		if a.needsFolder {
			title = "Choose a checkout or workspace"
			detail = "No compatible running session is available. Choose the folder you want to review."
		}
		if a.startupError != "" && !a.needsFolder {
			title = "Could not open local review"
		}
		design.Text(c, title, design.Heading)
		design.Text(c, detail, design.Secondary)
		if a.startupError != "" {
			design.Status(c, a.startupError, design.StatusError).Selectable()
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if design.Button(c, "Retry", design.Default).Disabled(a.starting).Clicked() {
				a.start(a.lastArgs)
			}
			if design.Button(c, "Choose folder", design.Primary).Disabled(a.starting).Clicked() {
				a.chooseFolder()
			}
			attach := slices.Contains(a.lastArgs, "--attach")
			if a.startupError != "" && !attach {
				if design.Button(c, "Replace running session", design.Default).Disabled(a.starting).Clicked() {
					a.requestConfirm("Replace running session?", "The running local server will stop. Other viewers must reconnect. The selected checkout or capture will replace its scope.", func() {
						args := slices.Clone(a.lastArgs)
						if !slices.Contains(args, "--replace") {
							args = append(args, "--replace")
						}
						a.start(args)
					})
				}
			}
		})
	})
}

func (a *application) reviewPane(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Background(c.Theme().Background).Children(func() {
		a.notices(c)
		a.comparisonToolbar(c)
		if a.state.CommitID != "" {
			a.commitBanner(c)
		}
		if a.state.Loading && a.state.Diff == nil {
			a.empty(c, "Loading captured comparison", "Stored manifests and previews are loading.", "", nil)
			return
		}
		if a.state.Error != "" && a.state.Diff == nil {
			a.empty(c, "Could not load comparison", a.state.Error, "Retry", func() { a.controller.Retry() })
			return
		}
		if a.state.Current == nil {
			title, detail := "No stored reviews", "Sync the selected checkout to collect a review."
			if a.state.Collecting {
				title, detail = "Collecting latest changes", "Reviews appear as each checkout finishes."
			}
			a.empty(c, title, detail, "Retry", func() { a.controller.Retry() })
			return
		}
		if a.state.Diff == nil {
			a.empty(c, "Comparison unavailable", "This comparison was not captured. Choose an available comparison.", "Retry", func() { a.controller.Retry() })
			return
		}
		files := a.visibleFiles()
		if len(files) == 0 {
			if a.state.CommitID != "" {
				title, detail := "No captured commit diff", "This commit has no captured file changes. Merge commits have no captured diff."
				a.empty(c, title, detail, "View Branch changes", a.branchChanges)
			} else if len(a.state.Diff.Files) > 0 && a.prefs.HideReviewed && a.fileFilter == "" {
				a.empty(c, "All files reviewed", "Reviewed files are hidden.", "Show reviewed files", func() { a.prefs.HideReviewed = false; a.persist() })
			} else if a.fileFilter != "" {
				a.empty(c, "No matching files", "Try a different path or file name.", "Clear file filter", func() { a.fileFilter = "" })
			} else {
				a.empty(c, "No changes in this comparison", "Other captured comparisons may contain changes, including staged and unstaged changes that cancel here.", "", nil)
			}
			return
		}
		a.drawDiffs(c, files)
	})
}

func (a *application) notices(c *ui.Context) {
	for _, notice := range []struct {
		text string
		role design.StatusRole
	}{
		{a.state.Error, design.StatusError}, {a.state.ReviewError, design.StatusError}, {a.state.CollectionError, design.StatusError}, {a.state.CatalogError, design.StatusError}, {a.actionError, design.StatusError},
	} {
		if notice.text == "" {
			continue
		}
		ui.Row(c).Padding(8, 12).Gap(8).Children(func() {
			design.Status(c, notice.text, notice.role).Grow(1)
			if design.Button(c, "Retry", design.Quiet).Clicked() {
				a.controller.Retry()
				a.actionError = ""
			}
		})
	}
	if a.state.Collecting {
		design.Status(c, "Collecting latest changes", design.StatusNeutral).Padding(8, 12)
	}
	if a.state.Notice != "" {
		design.Status(c, a.state.Notice, design.StatusNeutral).Padding(8, 12)
	}
	if a.state.Collection != nil && len(a.state.Collection.Failures) > 0 {
		ui.Column(c).Padding(8, 12).Gap(4).Children(func() {
			for _, failure := range a.state.Collection.Failures {
				text := failure.Path + ": " + failure.Detail
				if failure.DiagnosticId != nil {
					text += " (diagnostic " + *failure.DiagnosticId + ")"
				}
				design.Status(c, text, design.StatusWarning).Selectable()
			}
			if design.Button(c, "Retry failed collection", design.Default).Disabled(a.state.Collecting).Clicked() {
				a.controller.Retry()
			}
		})
	}
	if a.state.NewerContextID != "" {
		ui.Row(c).Padding(8, 12).Gap(8).Background(c.Theme().Selection).Children(func() {
			design.Text(c, "Newer snapshot available", design.Body).Grow(1)
			if design.Button(c, "View latest", design.Default).Clicked() {
				if a.state.Current != nil {
					a.openLatest(*a.state.Current)
				}
			}
		})
	}
}

func (a *application) comparisonToolbar(c *ui.Context) {
	design.Toolbar(c).Label("Diff display").Shrink(0).Children(func() {
		label := a.scopeLabel(a.state.Scope)
		if a.state.CommitID != "" {
			label = fmt.Sprintf("Commits (%d)", len(a.state.Commits))
		}
		design.Button(c, label, design.Default).Label("Diff comparison").Menu(func(m *ui.Menu) {
			for _, scope := range []goclient.DiffMode{"uncommitted", "committed", "all", "staged", "unstaged"} {
				if !a.capturedScope(scope) {
					continue
				}
				if m.Item(a.scopeLabel(scope)).Checked(a.state.CommitID == "" && a.state.Scope == scope).Chosen() {
					a.controller.SetScope(scope)
				}
			}
			if len(a.state.Commits) > 0 {
				m.Separator()
				if m.Item(fmt.Sprintf("Commits (%d)", len(a.state.Commits))).Chosen() {
					a.controller.OpenCommit(a.state.Commits[0].Oid)
				}
			}
		})
		ui.Box(c).Grow(1)
		for _, layout := range []string{"split", "unified"} {
			if design.Button(c, strings.ToUpper(layout[:1])+layout[1:], design.Quiet).Selected(a.prefs.Layout == layout).Clicked() {
				a.prefs.Layout = layout
				a.persist()
			}
		}
		design.Button(c, "View options", design.Quiet).Menu(a.displayMenu)
		design.Button(c, "Collapse", design.Quiet).Label("Collapse or expand all files").Menu(func(m *ui.Menu) {
			if m.Item("Collapse all files").Chosen() {
				for _, f := range a.visibleFiles() {
					a.fileCollapsed[f.Id] = true
				}
			}
			if m.Item("Expand all files").Chosen() {
				for _, f := range a.visibleFiles() {
					a.fileCollapsed[f.Id] = false
				}
			}
		})
	})
	if a.state.CommitID != "" {
		a.commitStepper(c)
	} else if a.state.Scope == "uncommitted" || a.state.Scope == "staged" || a.state.Scope == "unstaged" {
		design.Toolbar(c).Children(func() {
			for _, scope := range []goclient.DiffMode{"uncommitted", "staged", "unstaged"} {
				if a.capturedScope(scope) && design.Button(c, map[goclient.DiffMode]string{"uncommitted": "All changes", "staged": "Staged", "unstaged": "Unstaged"}[scope], design.Quiet).Selected(a.state.Scope == scope).Clicked() {
					a.controller.SetScope(scope)
				}
			}
		})
	}
}

func (a *application) capturedScope(scope goclient.DiffMode) bool {
	if a.state.Session == nil || a.state.Session.Capabilities.Diff.Scopes.State != goclient.CapabilityStateEnabled {
		return false
	}
	return slices.Contains(a.state.Session.Capabilities.Diff.Scopes.Values, scope)
}

func (a *application) scopeLabel(scope goclient.DiffMode) string {
	if scope == "staged" {
		return "Staged"
	}
	if scope == "unstaged" {
		return "Unstaged"
	}
	if scope == "uncommitted" {
		return "Uncommitted"
	}
	if scope == "committed" {
		return "Branch changes"
	}
	if a.state.Current != nil && workspace.IsPiped(*a.state.Current) {
		return "Piped patch"
	}
	if a.state.Current != nil {
		switch workspace.ComparisonKind(*a.state.Current, scope) {
		case "committed":
			return "Branch changes"
		case "uncommitted":
			return "Uncommitted"
		case "combined":
			return "Combined"
		case "patch":
			return "Piped patch"
		}
	}
	return "Combined"
}

func (a *application) branchChanges() {
	if a.controller == nil {
		return
	}
	if a.capturedScope("committed") {
		a.controller.SetScope("committed")
	} else {
		a.controller.SetScope("all")
	}
}

func (a *application) empty(c *ui.Context, title, detail, action string, run func()) {
	ui.Column(c).Grow(1).FillWidth().Padding(24).Gap(12).Children(func() {
		design.Text(c, title, design.Heading)
		design.Text(c, detail, design.Secondary)
		if action != "" && design.Button(c, action, design.Default).Clicked() && run != nil {
			run()
		}
	})
}
