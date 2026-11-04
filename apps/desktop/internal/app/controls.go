package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/flexdinesh/diffx/apps/desktop/internal/design"
	"github.com/flexdinesh/diffx/apps/desktop/internal/preferences"
	"github.com/flexdinesh/diffx/apps/desktop/internal/workspace"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func (a *application) footer(c *ui.Context, width float32) {
	design.Toolbar(c).Height(28).MinHeight(28).Label("Review status").Children(func() {
		if a.state.Diff != nil {
			label := a.baseline()
			if design.Button(c, label, design.Quiet).Label("Diff comparison details").MaxWidth(width / 2).Clicked() {
				a.comparisonDetails = true
			}
		}
		if width >= 1280 {
			hint := "Alt+J/K files"
			if a.state.CommitID == "" {
				hint += " · Alt+M reviewed"
			}
			design.Text(c, hint, design.Muted)
		}
		ui.Box(c).Grow(1)
		if a.feedback != "" {
			design.Text(c, a.feedback, design.Muted)
		}
		if len(a.drafts) > 0 {
			if design.Button(c, fmt.Sprintf("Drafts (%d)", len(a.drafts)), design.Quiet).Label("Show comment drafts").Clicked() {
				a.sidebarTab = "Comments"
				if width < 1012 || a.prefs.FilePaneCollapsed {
					a.fileDrawer = true
				} else {
					a.prefs.FilePaneCollapsed = false
				}
			}
		}
		if a.state.Saving {
			design.Text(c, "Saving", design.Muted)
		}
		label := "Connecting to server"
		if a.state.Connected {
			label = "Server connected"
		}
		if a.state.MetricsError != "" {
			label = "Server status unavailable"
		}
		if design.Button(c, "Server", design.Quiet).Label(label).Tooltip(label).Clicked() {
			a.metrics = true
			if a.controller != nil {
				a.controller.LoadMetrics()
			}
		}
		if design.Button(c, "Settings", design.Quiet).Clicked() {
			a.settings = true
		}
	})
}

func (a *application) baseline() string {
	if a.state.CommitID != "" {
		return shortOID(a.state.CommitID) + "^ → " + shortOID(a.state.CommitID)
	}
	switch a.state.Scope {
	case "staged":
		return "HEAD → Index"
	case "unstaged":
		return "Index → Working tree"
	case "uncommitted":
		return "HEAD → Working changes"
	}
	if a.state.Current != nil && workspace.IsPiped(*a.state.Current) {
		return "Supplied patch"
	}
	if a.state.Current != nil && a.state.Current.Observation != nil {
		o := a.state.Current.Observation
		if o.Comparison != nil {
			base := o.Comparison.BaseRef
			if o.Comparison.MergeBase != nil {
				base = shortOID(*o.Comparison.MergeBase)
			}
			target := "Working changes"
			if a.state.Scope == "committed" {
				target = shortOID(stringValue(o.Head))
			}
			return base + " → " + target
		}
	}
	return "Captured HEAD → Working changes"
}

func shortOID(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}

func (a *application) commitStepper(c *ui.Context) {
	i := slices.IndexFunc(a.state.Commits, func(commit goclient.CommitSummary) bool { return commit.Oid == a.state.CommitID })
	design.Toolbar(c).Children(func() {
		if design.Button(c, "Older", design.Default).Label("Older commit").Disabled(i <= 0).Clicked() {
			a.stepCommit(-1)
		}
		label := shortOID(a.state.CommitID)
		if i >= 0 {
			label += " " + a.state.Commits[i].Subject
		}
		label += fmt.Sprintf(" · %d/%d", i+1, len(a.state.Commits))
		if design.Button(c, label, design.Default).Label("Choose commit").Grow(1).MinWidth(0).Clicked() {
			a.commitPicker = true
			a.commitQuery = ""
		}
		if design.Button(c, "Newer", design.Default).Label("Newer commit").Disabled(i < 0 || i >= len(a.state.Commits)-1).Clicked() {
			a.stepCommit(1)
		}
	})
	if a.state.CommitsOmitted > 0 {
		design.Text(c, fmt.Sprintf("%d older commits were not captured", a.state.CommitsOmitted), design.Muted).Padding(4, 12)
	}
}

func (a *application) commitBanner(c *ui.Context) {
	for _, commit := range a.state.Commits {
		if commit.Oid != a.state.CommitID {
			continue
		}
		ui.Column(c).Padding(8, 12).Gap(8).Background(c.Theme().Surface).Children(func() {
			design.Text(c, shortOID(commit.Oid)+" "+commit.Subject, design.Heading)
			detail := commit.AuthorName + " · Read-only captured commit"
			if commit.Merge {
				detail += " · Merge commit"
			}
			design.Text(c, detail, design.Muted)
			if commit.Body != "" {
				label := "Show message"
				if a.commitMessage {
					label = "Hide message"
				}
				if design.Button(c, label, design.Quiet).Clicked() {
					a.commitMessage = !a.commitMessage
				}
				if a.commitMessage {
					ui.Scroll(c).MaxHeight(160).Children(func() { design.Text(c, commit.Body, design.Code).Selectable() })
				}
			}
		})
		return
	}
	if !a.state.Loading {
		design.Status(c, "Selected commit is missing from this snapshot", design.StatusWarning).Padding(8, 12)
	}
}

func (a *application) displayMenu(m *ui.Menu) {
	if m.Item("Wrap code").Checked(a.prefs.Wrap).Chosen() {
		a.prefs.Wrap = !a.prefs.Wrap
		a.persist()
	}
	if m.Item("Line numbers").Checked(!a.prefs.NoLineNumbers).Chosen() {
		a.prefs.NoLineNumbers = !a.prefs.NoLineNumbers
		a.persist()
	}
	if m.Item("Changed line backgrounds").Checked(!a.prefs.NoBackground).Chosen() {
		a.prefs.NoBackground = !a.prefs.NoBackground
		a.persist()
	}
	if m.Item("Syntax highlighting").Checked(a.prefs.Syntax).Chosen() {
		a.prefs.Syntax = !a.prefs.Syntax
		a.persist()
	}
	m.Submenu("Change indicators", func(m *ui.Menu) {
		for _, style := range []string{"bars", "classic", "none"} {
			if m.Item(style).Checked(a.prefs.Indicators == style).Chosen() {
				a.prefs.Indicators = style
				a.persist()
			}
		}
	})
	m.Submenu("Inline changes", func(m *ui.Menu) {
		for _, detail := range []string{"word-alt", "word", "char", "none"} {
			if m.Item(detail).Checked(a.prefs.LineDiff == detail).Chosen() {
				a.prefs.LineDiff = detail
				a.persist()
			}
		}
	})
	m.Submenu("Code theme", func(m *ui.Menu) {
		for _, theme := range design.CodeThemes {
			if m.Item(theme.Label).Checked(a.prefs.CodeTheme == theme.ID).Chosen() {
				a.prefs.CodeTheme = theme.ID
				a.persist()
			}
		}
	})
	if m.Item("Display and font settings").Chosen() {
		a.settings = true
	}
}

func (a *application) overlays(c *ui.Context, width, height float32) {
	a.reviewPicker(c, width, height)
	a.identityPicker(c, true, width, height)
	a.identityPicker(c, false, width, height)
	a.commitsPicker(c, width, height)
	ui.Modal(c.Key("file-drawer"), &a.fileDrawer, func() {
		ui.Column(c).Width(min(400, width-48)).Height(height - 96).Children(func() {
			if design.Button(c, "Close files", design.Quiet).Clicked() {
				a.fileDrawer = false
			}
			a.sidebar(c)
		})
	})
	ui.Modal(c.Key("project-drawer"), &a.projectDrawer, func() {
		ui.Column(c).Width(min(400, width-48)).Height(height - 96).Children(func() {
			if design.Button(c, "Close reviews", design.Quiet).Clicked() {
				a.projectDrawer = false
			}
			a.projects(c)
		})
	})
	a.settingsDialog(c, width, height)
	ui.Modal(c.Key("server-status"), &a.metrics, func() {
		ui.Column(c).Width(min(420, width-48)).Gap(12).Children(func() {
			design.Text(c, "Diffx server process", design.Heading)
			if a.state.MetricsLoading {
				design.Text(c, "Loading server status", design.Muted)
			} else if a.state.MetricsError != "" {
				design.Status(c, a.state.MetricsError, design.StatusError)
			} else if metrics := a.state.Metrics; metrics != nil {
				design.Text(c, fmt.Sprintf("CPU %.1f%%", metrics.CpuUsage), design.Body)
				design.Text(c, fmt.Sprintf("RAM %.1f MiB", metrics.RssBytes/(1024*1024)), design.Body)
			}
			if a.ready != nil {
				owner := "Attached session. Existing server remains running on quit."
				if a.ready.Owned {
					owner = "Owned session. Quit stops this server."
				}
				design.Text(c, owner, design.Secondary)
				design.Text(c, a.ready.ViewerURL, design.Code).Selectable()
			}
			ui.Row(c).Gap(8).Children(func() {
				if design.Button(c, "Refresh server status", design.Default).Disabled(a.controller == nil || a.state.MetricsLoading).Clicked() {
					a.controller.LoadMetrics()
				}
				if design.Button(c, "Close", design.Quiet).Clicked() {
					a.metrics = false
				}
			})
		})
	})
	ui.Modal(c.Key("comparison-details"), &a.comparisonDetails, func() {
		ui.Column(c).Width(min(600, width-48)).Gap(8).Children(func() {
			design.Text(c, "Diff comparison", design.Heading)
			design.Text(c, a.baseline(), design.Body)
			if a.state.Current != nil && a.state.Current.Observation != nil {
				o := a.state.Current.Observation
				if comp := o.Comparison; comp != nil {
					design.Text(c, "Base ref: "+comp.BaseRef, design.Code).Selectable()
					if comp.BaseCommit != nil {
						design.Text(c, "Base commit: "+*comp.BaseCommit, design.Code).Selectable()
					}
					if comp.MergeBase != nil {
						design.Text(c, "Merge base: "+*comp.MergeBase, design.Code).Selectable()
					}
				}
				design.Text(c, "HEAD: "+stringValue(o.Head), design.Code).Selectable()
			}
			design.Text(c, "These facts belong to the captured snapshot. Opening a comparison does not read current Git refs.", design.Secondary)
			if design.Button(c, "Close", design.Quiet).Clicked() {
				a.comparisonDetails = false
			}
		})
	})
	ui.Modal(c.Key("shortcuts"), &a.shortcuts, func() {
		ui.Column(c).Width(min(560, width-48)).Gap(8).Children(func() {
			design.Text(c, "Keyboard shortcuts", design.Heading)
			for _, line := range []string{"Cmd/Ctrl+K  Search reviews", "Cmd/Ctrl+Shift+F  Filter files", "Alt+J / Alt+K  Next / previous file", "Alt+M  Mark reviewed and advance", "Alt+R  Sync checkouts", "Alt+[ / Alt+]  Older / newer commit", "Alt+Up / Alt+Down  Select adjacent line", "Alt+Shift+Up / Down  Extend line selection", "Cmd/Ctrl+Enter  Save active comment", "Cmd/Ctrl+Shift+C  Copy review", "Cmd/Ctrl+,  Settings", "Escape  Close dialog"} {
				design.Text(c, line, design.Body)
			}
			design.Text(c, "Select line ranges with click then Shift-click. Ranges must be contiguous and at most 200 lines.", design.Secondary)
			if design.Button(c, "Close", design.Quiet).Clicked() {
				a.shortcuts = false
			}
		})
	})
	confirm := a.confirmAction != nil
	if result := ui.AlertDialog(c.Key("confirmation"), &confirm, a.confirmTitle, a.confirmDetail, "Cancel", "Confirm"); result == 1 {
		action := a.confirmAction
		a.confirmAction = nil
		if action != nil {
			action()
		}
	} else if !confirm {
		a.confirmAction = nil
	}
}

func (a *application) commitsPicker(c *ui.Context, width, height float32) {
	ui.Modal(c.Key("commit-picker"), &a.commitPicker, func() {
		ui.Column(c).Width(min(640, width-48)).MaxHeight(height - 96).Gap(8).Children(func() {
			design.Text(c, "Captured commits", design.Heading)
			input := ui.TextInput(c, &a.commitQuery).AutoFocus().Label("Filter commits").Placeholder("Subject, author or SHA prefix").FillWidth()
			ui.Scroll(c).Grow(1).MinHeight(80).Children(func() {
				count := 0
				for _, commit := range a.state.Commits {
					match := true
					for _, word := range strings.Fields(strings.ToLower(a.commitQuery)) {
						if !strings.Contains(strings.ToLower(commit.Subject+" "+commit.AuthorName), word) && !strings.HasPrefix(strings.ToLower(commit.Oid), word) {
							match = false
							break
						}
					}
					if !match {
						continue
					}
					label := shortOID(commit.Oid) + " " + commit.Subject + fmt.Sprintf(" +%d -%d", commit.Additions, commit.Deletions)
					if commit.Merge {
						label += " · Merge commit"
					}
					if design.Button(c.Key(commit.Oid), label, design.Quiet).Label("Open commit "+commit.Oid).FillWidth().Selected(commit.Oid == a.state.CommitID).Clicked() || input.Submitted() && count == 0 {
						a.controller.OpenCommit(commit.Oid)
						a.commitPicker = false
					}
					design.Text(c, commit.AuthorName, design.Muted)
					count++
				}
				if count == 0 {
					design.Text(c, "No matching captured commits", design.Muted)
				}
			})
			if a.state.CommitsOmitted > 0 {
				design.Text(c, fmt.Sprintf("%d older commits were not captured", a.state.CommitsOmitted), design.Muted)
			}
			if design.Button(c, "Close commits", design.Quiet).Clicked() {
				a.commitPicker = false
			}
		})
	})
}

func (a *application) settingsDialog(c *ui.Context, width, height float32) {
	ui.Modal(c.Key("settings"), &a.settings, func() {
		ui.Column(c).Width(min(560, width-48)).MaxHeight(height - 96).Gap(12).Children(func() {
			ui.Row(c).Children(func() {
				design.Text(c, "Settings", design.Heading).Grow(1)
				if design.Button(c, "Close settings", design.Quiet).Clicked() {
					a.settings = false
				}
			})
			ui.Scroll(c).Grow(1).MinHeight(120).TrackScroll(&a.settingsScroll).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					if a.preferenceError != "" {
						design.Status(c, a.preferenceError, design.StatusError)
						if design.Button(c, "Save current settings", design.Default).Clicked() {
							a.persist()
						}
					}
					design.Text(c, "Theme", design.Heading)
					for _, theme := range design.Themes {
						value := a.prefs.Theme
						if ui.Radio(c.Key(theme.ID), &value, theme.ID, theme.Label).Changed() {
							a.prefs.Theme = value
							a.persist()
						}
					}
					design.Text(c, "Code display", design.Heading)
					code := a.prefs.CodeTheme
					options := make([]string, 0, len(design.CodeThemes))
					for _, theme := range design.CodeThemes {
						options = append(options, theme.ID)
					}
					if ui.Select(c, &code, options).Label("Code theme").Changed() {
						a.prefs.CodeTheme = code
						a.persist()
					}
					layout := a.prefs.Layout
					if ui.Select(c, &layout, []string{"split", "unified"}).Label("Diff layout").Changed() {
						a.prefs.Layout = layout
						a.persist()
					}
					line := a.prefs.LineDiff
					if ui.Select(c, &line, []string{"word-alt", "word", "char", "none"}).Label("Inline change detail").Changed() {
						a.prefs.LineDiff = line
						a.persist()
					}
					wrap := a.prefs.Wrap
					if ui.Checkbox(c, &wrap, "Wrap code").Changed() {
						a.prefs.Wrap = wrap
						a.persist()
					}
					font := a.prefs.FontFamily
					if font == "" {
						font = "System monospace"
					}
					fonts := []string{"System monospace"}
					for _, family := range ui.FontFamilies() {
						if family.Monospaced {
							fonts = append(fonts, family.Name)
						}
					}
					if ui.Combobox(c, &font, fonts).Label("Code font").Changed() {
						if font == "System monospace" {
							font = ""
						}
						a.prefs.FontFamily = font
						a.persist()
					}
					design.Text(c, fmt.Sprintf("Code font size: %.0f", a.prefs.FontSize), design.Body)
					size := float64(a.prefs.FontSize)
					if ui.Slider(c, &size, 8, 32).Step(1).Label("Code font size").Changed() {
						a.prefs.FontSize = float32(size)
						a.persist()
					}
					tab := fmt.Sprint(a.prefs.TabSize)
					if ui.Select(c, &tab, []string{"1", "2", "3", "4", "6", "8", "12", "16"}).Label("Tab size").Changed() {
						var n int
						if _, err := fmt.Sscan(tab, &n); err == nil {
							a.prefs.TabSize = n
							a.persist()
						}
					}
					design.Text(c, "Navigation", design.Heading)
					view := a.prefs.FileView
					if ui.Select(c, &view, []string{"tree", "list"}).Label("File navigation").Changed() {
						a.prefs.FileView = view
						a.persist()
					}
					group := a.prefs.GroupByRepository
					if ui.Checkbox(c, &group, "Group reviews by repository").Changed() {
						a.prefs.GroupByRepository = group
						a.persist()
					}
					if design.Button(c, "Keyboard shortcuts", design.Default).Clicked() {
						a.shortcuts = true
					}
					if design.Button(c, "Reset native preferences", design.Default).Clicked() {
						a.requestConfirm("Reset native preferences?", "Themes, display and navigation preferences return to defaults. Server comments and reviewed marks remain.", func() { a.prefs = preferences.Defaults(); a.persist() })
					}
				})
			})
		})
	})
}

func (a *application) keyboard(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyK) {
		a.openSearch()
	}
	if c.Shortcut(ui.Cmd, ui.KeyComma) {
		a.settings = true
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyF) {
		a.fileFocus.Focus()
		a.prefs.FilePaneCollapsed = false
	}
	if c.Shortcut(ui.Alt, ui.KeyJ) {
		a.stepFile(1)
	}
	if c.Shortcut(ui.Alt, ui.KeyK) {
		a.stepFile(-1)
	}
	if c.Shortcut(ui.Alt, ui.KeyBracketLeft) {
		a.stepCommit(-1)
	}
	if c.Shortcut(ui.Alt, ui.KeyBracketRight) {
		a.stepCommit(1)
	}
	if c.Shortcut(ui.Alt, ui.KeyM) && a.mutableReview() {
		for _, file := range a.visibleFiles() {
			if file.Id == a.selectedFile && !a.reviewed(file) {
				a.markFile(file, true)
				break
			}
		}
	}
	if c.Shortcut(ui.Alt, ui.KeyR) && a.controller != nil && a.state.Bootstrap != nil && a.state.Bootstrap.Collect && !a.state.Collecting {
		a.controller.Sync()
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) && a.activeDraft != "" {
		a.saveDraft(a.activeDraft)
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyC) {
		a.exportReview(workspace.ExportOptions{})
	}
	if c.Shortcut(ui.Shift, ui.KeyF1) {
		a.shortcuts = true
	}
}

func (a *application) installMenu() {
	action := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(_ *mygo.MenuItem, _ *mygo.Window) {
			fn()
			if a.window != nil {
				a.window.Invalidate()
			}
		}
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Diffx", Submenu: []*mygo.MenuItem{{Label: "Settings", Accelerator: "CmdOrCtrl+,", Click: action(func() { a.settings = true })}, {Role: mygo.RoleQuit}}},
		{Label: "File", Submenu: []*mygo.MenuItem{{Label: "Choose folder", Accelerator: "CmdOrCtrl+O", Click: action(a.chooseFolder)}, {Label: "Retry connection", Click: action(func() {
			if a.startupError != "" {
				a.start(a.lastArgs)
			} else if a.controller != nil {
				a.controller.Retry()
			}
		})}, {Role: mygo.RoleClose}}},
		{Role: mygo.RoleEditMenu},
		{Label: "Review", Submenu: []*mygo.MenuItem{{Label: "Search reviews", Accelerator: "CmdOrCtrl+K", Click: action(a.openSearch)}, {Label: "Copy review", Accelerator: "CmdOrCtrl+Shift+C", Click: action(func() { a.exportReview(workspace.ExportOptions{}) })}, {Label: "Sync", Accelerator: "Alt+R", Click: action(func() {
			if a.controller != nil && a.state.Bootstrap != nil && a.state.Bootstrap.Collect && !a.state.Collecting {
				a.controller.Sync()
			}
		})}}},
		{Label: "Help", Submenu: []*mygo.MenuItem{{Label: "Keyboard shortcuts", Click: action(func() { a.shortcuts = true })}}},
	}))
}
