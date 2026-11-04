package app

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/flexdinesh/diffx/apps/desktop/internal/design"
	"github.com/flexdinesh/diffx/apps/desktop/internal/workspace"
	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func repositoryName(c goclient.Context) string { return workspace.DisplayIdentity(c).Name }
func branchName(c goclient.Context) string {
	i := workspace.DisplayIdentity(c)
	if i.Piped {
		return capturedTime(c)
	}
	if i.Branch == "" {
		return "Unknown branch"
	}
	return i.Branch
}
func capturedTime(c goclient.Context) string {
	return time.UnixMilli(workspace.Timestamp(c)).Local().Format("02 Jan 15:04")
}
func contextMetadata(c goclient.Context) string {
	i := workspace.DisplayIdentity(c)
	if i.Piped {
		return "Piped · " + capturedTime(c)
	}
	parts := []string{i.Branch}
	if i.Worktree != "" {
		parts = append(parts, "Worktree "+i.Worktree)
	}
	if i.Host != "" {
		parts = append(parts, i.Host)
	}
	if o := c.Observation; o != nil {
		if o.Agent != "" {
			parts = append(parts, o.Agent)
		}
		if o.RunId != "" {
			parts = append(parts, o.RunId)
		}
	}
	if c.Sessions != nil {
		for _, s := range *c.Sessions {
			name := s.Id
			if s.Name != nil {
				name = *s.Name
			}
			parts = append(parts, s.Harness+" "+name)
		}
	}
	parts = append(parts, capturedTime(c))
	return strings.Join(parts, " · ")
}

func (a *application) projects(c *ui.Context) {
	ui.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
		design.Toolbar(c).Children(func() {
			design.Text(c, "Reviews", design.Heading).Grow(1)
			design.Button(c, "View", design.Quiet).Label("Review list view").Menu(func(m *ui.Menu) {
				if m.Item("Group by repo").Checked(a.prefs.GroupByRepository).Chosen() {
					a.prefs.GroupByRepository = true
					a.persist()
				}
				if m.Item("Recent first").Checked(!a.prefs.GroupByRepository).Chosen() {
					a.prefs.GroupByRepository = false
					a.persist()
				}
			})
			if design.Button(c, "Collapse", design.Quiet).Label("Collapse reviews").Clicked() {
				a.prefs.ProjectPaneCollapsed = true
				a.prefs.NavigationMode = "rail"
				a.persist()
				a.projectDrawer = false
			}
		})
		ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&a.projectScroll).Children(func() {
			groups := workspace.ProjectGroups(a.state.Contexts, a.state.Seen)
			if len(groups) == 0 {
				design.Text(c, "No current reviews with changes", design.Muted).Padding(12)
				return
			}
			if a.prefs.GroupByRepository {
				for _, group := range groups {
					ui.Column(c.Key(group.ID)).FillWidth().Children(func() {
						ui.Row(c).Gap(4).Children(func() {
							label := "Collapse " + group.Name
							if a.repositoryCollapsed[group.ID] {
								label = "Expand " + group.Name
							}
							if design.Button(c, label, design.Quiet).MaxWidth(88).Clicked() {
								a.repositoryCollapsed[group.ID] = !a.repositoryCollapsed[group.ID]
							}
							if design.Button(c, group.Name, design.Quiet).Label("Open latest in " + group.Name).Grow(1).MinWidth(0).Clicked() {
								a.openLatest(group.Streams[0].Context)
								a.repositoryCollapsed[group.ID] = false
							}
						})
						if !a.repositoryCollapsed[group.ID] {
							for _, stream := range group.Streams {
								a.projectRow(c, stream.Context, stream.Unseen, false)
							}
						}
					})
				}
			} else {
				heads := slices.DeleteFunc(slices.Clone(a.state.Contexts), func(head goclient.Context) bool { return !workspace.CurrentWork(head) })
				slices.SortFunc(heads, workspace.CompareActivity)
				for _, head := range heads {
					a.projectRow(c, head, a.state.Seen != nil && a.state.Seen[workspace.StreamKey(head)] != head.Id, true)
				}
			}
		})
	})
}

func (a *application) projectRow(c *ui.Context, head goclient.Context, unread, repo bool) {
	label := branchName(head)
	if repo {
		label = repositoryName(head) + " · " + label
	}
	if unread {
		label += " · New changes"
	}
	selected := a.state.Current != nil && workspace.StreamKey(*a.state.Current) == workspace.StreamKey(head)
	ui.Column(c.Key(head.Id)).FillWidth().Padding(4, 8).Gap(0).Children(func() {
		if design.Button(c, label, design.Quiet).Label("Open review " + head.Id).FillWidth().Selected(selected).Clicked() {
			a.openLatest(head)
		}
		design.Text(c, contextMetadata(head), design.Muted).SingleLine().Tooltip(contextMetadata(head)).FillWidth()
	})
}

func (a *application) projectRail(c *ui.Context) {
	ui.Column(c).Width(44).Background(c.Theme().Surface).Gap(4).Children(func() {
		if design.Button(c, "Reviews", design.Quiet).Label("Expand reviews").Clicked() {
			a.prefs.ProjectPaneCollapsed = false
			a.prefs.NavigationMode = "list"
			a.persist()
		}
		for _, group := range workspace.ProjectGroups(a.state.Contexts, a.state.Seen) {
			letters := []rune(strings.ToUpper(group.Name))
			if len(letters) > 2 {
				letters = letters[:2]
			}
			if design.Button(c, string(letters), design.Quiet).Label("Open latest in " + group.Name).Tooltip(group.Name).Clicked() {
				a.openLatest(group.Streams[0].Context)
			}
		}
	})
}

func (a *application) openSearch() {
	if a.capture() || a.controller == nil {
		return
	}
	a.picker = true
	a.controller.Search(a.reviewQuery)
	a.searchFocus.Focus()
}

func (a *application) reviewPicker(c *ui.Context, width, height float32) {
	ui.Modal(c.Key("review-picker"), &a.picker, func() {
		ui.Column(c).Width(min(680, width-48)).MaxHeight(height - 96).Gap(8).Children(func() {
			ui.Row(c).Children(func() {
				design.Text(c, "Switch review", design.Heading).Grow(1)
				if design.Button(c, "Close", design.Quiet).Clicked() {
					a.picker = false
				}
			})
			input := ui.TextInput(c, &a.reviewQuery).Label("Search reviews").Placeholder("Repository, branch, worktree, host or session").Bind(&a.searchFocus).AutoFocus().FillWidth()
			if input.Changed() {
				a.controller.Search(a.reviewQuery)
			}
			a.pickerFilters(c)
			if a.state.SearchError != "" {
				design.Status(c, a.state.SearchError, design.StatusError)
				if design.Button(c, "Retry search", design.Default).Clicked() {
					a.controller.Search(a.reviewQuery)
				}
			}
			results := workspace.PickerResults(a.state.History, a.reviewQuery, a.state.Filters)
			design.Button(c, "Repository", design.Quiet).Label("Filter review repository").Menu(func(m *ui.Menu) {
				if m.Item("All repositories").Checked(a.pickerProject == "").Chosen() {
					a.pickerProject = ""
				}
				seen := map[string]bool{}
				for _, entry := range results {
					key := workspace.ProjectKey(entry)
					if seen[key] {
						continue
					}
					seen[key] = true
					if m.Item(repositoryName(entry)).Checked(a.pickerProject == key).Chosen() {
						a.pickerProject = key
					}
				}
			})
			if a.pickerProject != "" {
				results = slices.DeleteFunc(results, func(entry goclient.Context) bool { return workspace.ProjectKey(entry) != a.pickerProject })
			}
			if input.Submitted() {
				for _, entry := range results {
					if entry.Availability != "unavailable" {
						a.openContext(entry.Id)
						break
					}
				}
			}
			ui.Scroll(c).Grow(1).MinHeight(80).TrackScroll(&a.pickerScroll).Children(func() {
				if len(results) == 0 && !a.state.Searching {
					design.Text(c, "No matching reviews", design.Muted).Padding(12)
				}
				for _, entry := range results {
					label := repositoryName(entry) + " · " + branchName(entry)
					if entry.Stale != nil && *entry.Stale && !workspace.IsPiped(entry) {
						label += " · Stale"
					}
					if entry.Availability == "unavailable" {
						label += " · Unavailable"
					}
					if entry.Id == contextID(a.state) {
						label += " · Current review"
					}
					ui.Column(c.Key(entry.Id)).FillWidth().Padding(4, 8).Children(func() {
						if design.Button(c, label, design.Quiet).Label("Open stored review " + entry.Id).Disabled(entry.Availability == "unavailable").FillWidth().Clicked() {
							a.openContext(entry.Id)
						}
						design.Text(c, contextMetadata(entry), design.Muted).SingleLine()
						if root := workspace.DisplayIdentity(entry).Root; root != "" {
							design.Text(c, root, design.Code).SingleLine()
						}
					})
				}
				if a.state.Searching {
					design.Text(c, "Loading reviews", design.Muted)
				}
				if a.state.NextCursor != "" && design.Button(c, "Load more reviews", design.Default).Disabled(a.state.Searching).Clicked() {
					a.controller.SearchNext()
				}
			})
		})
	})
}

func (a *application) pickerFilters(c *ui.Context) {
	filters := a.state.Filters
	ui.Row(c).Gap(8).Wrap().Children(func() {
		for _, freshness := range []string{"all", "latest", "stale"} {
			label := strings.ToUpper(freshness[:1]) + freshness[1:]
			if design.Button(c, label, design.Quiet).Label("Freshness " + label).Selected(filters.Freshness == freshness).Clicked() {
				filters.Freshness = freshness
				a.controller.SetFilters(filters)
			}
		}
		for _, field := range []string{"Host", "Branch", "Worktree"} {
			design.Button(c, field, design.Default).Menu(func(m *ui.Menu) {
				values := []string{}
				for _, entry := range a.state.History {
					if workspace.IsPiped(entry) {
						continue
					}
					i := workspace.DisplayIdentity(entry)
					v := i.Host
					if field == "Branch" {
						v = i.Branch
					}
					if field == "Worktree" {
						v = i.Worktree
					}
					if v != "" && !slices.Contains(values, v) {
						values = append(values, v)
					}
				}
				slices.Sort(values)
				selected := filters.Hosts
				if field == "Branch" {
					selected = filters.Branches
				}
				if field == "Worktree" {
					selected = filters.Worktrees
				}
				if m.Item("All " + strings.ToLower(field) + "s").Checked(len(selected) == 0).Chosen() {
					selected = nil
				}
				for _, value := range values {
					if m.Item(value).Checked(slices.Contains(selected, value)).Chosen() {
						selected = slices.Clone(selected)
						if i := slices.Index(selected, value); i >= 0 {
							selected = slices.Delete(selected, i, i+1)
						} else {
							selected = append(selected, value)
						}
					}
				}
				updated := filters
				switch field {
				case "Host":
					updated.Hosts = selected
				case "Branch":
					updated.Branches = selected
				case "Worktree":
					updated.Worktrees = selected
				}
				if !slices.Equal(updated.Hosts, filters.Hosts) || !slices.Equal(updated.Branches, filters.Branches) || !slices.Equal(updated.Worktrees, filters.Worktrees) {
					a.controller.SetFilters(updated)
				}
			})
		}
		all := filters.IncludeAll
		if ui.Checkbox(c, &all, "All snapshots").Tooltip("Include empty, unknown-status and unavailable reviews").Changed() {
			filters.IncludeAll = all
			a.controller.SetFilters(filters)
		}
	})
}

func (a *application) identityPicker(c *ui.Context, repository bool, width, height float32) {
	open, query, title := &a.branchPicker, &a.branchQuery, "Switch branch or worktree"
	if repository {
		open, query, title = &a.repoPicker, &a.repositoryQuery, "Switch repository"
	}
	ui.Modal(c.Key(title), open, func() {
		ui.Column(c).Width(min(560, width-48)).MaxHeight(height - 96).Gap(8).Children(func() {
			design.Text(c, title, design.Heading)
			input := ui.TextInput(c, query).AutoFocus().Label("Filter " + strings.ToLower(title[7:])).FillWidth()
			ui.Scroll(c).Grow(1).MinHeight(80).Children(func() {
				count := 0
				for _, group := range workspace.ProjectGroups(a.state.Contexts, a.state.Seen) {
					if repository {
						if !strings.Contains(strings.ToLower(group.Name), strings.ToLower(*query)) {
							continue
						}
						if design.Button(c, group.Name, design.Quiet).FillWidth().Clicked() || input.Submitted() && count == 0 {
							a.openLatest(group.Streams[0].Context)
						}
						count++
						continue
					}
					if a.state.Current == nil || group.ID != workspace.ProjectKey(*a.state.Current) {
						continue
					}
					for _, stream := range group.Streams {
						if len(workspace.PickerResults([]goclient.Context{stream.Context}, *query, workspace.Filters{Freshness: "all", IncludeAll: true})) == 0 {
							continue
						}
						label := branchName(stream.Context) + " · " + contextMetadata(stream.Context)
						if stream.Unseen {
							label += " · New changes"
						}
						if design.Button(c, label, design.Quiet).FillWidth().Clicked() || input.Submitted() && count == 0 {
							a.openLatest(stream.Context)
						}
						count++
					}
				}
				if count == 0 {
					design.Text(c, "No matching reviews", design.Muted)
				}
			})
		})
	})
}

func (a *application) sidebar(c *ui.Context) {
	ui.Column(c).Fill().Background(c.Theme().Surface).Children(func() {
		design.Toolbar(c).Children(func() {
			for _, tab := range []string{"Files", "Comments"} {
				if design.Button(c, tab, design.Quiet).Selected(a.sidebarTab == tab).Clicked() {
					a.sidebarTab = tab
				}
			}
		})
		if a.sidebarTab == "Comments" {
			a.commentsPanel(c)
			return
		}
		if a.state.Diff == nil {
			design.Text(c, "No captured files", design.Muted).Padding(12)
			return
		}
		ui.Row(c).Padding(8).Gap(4).Children(func() {
			ui.TextInput(c, &a.fileFilter).Label("Filter files").Placeholder("Filter files").Bind(&a.fileFocus).Grow(1).MinWidth(0)
			if a.state.CommitID == "" && design.Button(c, "Hide reviewed", design.Quiet).Selected(a.prefs.HideReviewed).Clicked() {
				a.prefs.HideReviewed = !a.prefs.HideReviewed
				a.persist()
			}
		})
		design.Toolbar(c).Children(func() {
			design.Text(c, fmt.Sprintf("%d files changed", len(a.state.Diff.Files)), design.Muted).Grow(1)
			design.Button(c, "File view", design.Quiet).Menu(func(m *ui.Menu) {
				for _, view := range []string{"tree", "list"} {
					if m.Item(view).Checked(a.prefs.FileView == view).Chosen() {
						a.prefs.FileView = view
						a.persist()
					}
				}
				m.Separator()
				if m.Item("Collapse folders").Chosen() {
					for _, f := range a.visibleFiles() {
						for dir := path.Dir(f.Path); dir != "." && dir != "/"; dir = path.Dir(dir) {
							a.folderCollapsed[dir] = true
						}
					}
				}
				if m.Item("Expand folders").Chosen() {
					clear(a.folderCollapsed)
				}
			})
		})
		files := a.visibleFiles()
		ui.Scroll(c).Grow(1).MinHeight(0).TrackScroll(&a.fileScroll).Children(func() {
			if a.prefs.FileView == "tree" {
				ui.Tree(c, func() { a.fileTree(c, files, "") }).FillWidth()
			} else {
				for _, file := range files {
					a.fileRow(c, file, false)
				}
			}
			if len(files) == 0 {
				design.Text(c, "No visible files", design.Muted).Padding(12)
			}
		})
		if a.state.CommitID == "" {
			count := 0
			for _, f := range a.state.Diff.Files {
				if a.reviewed(f) {
					count++
				}
			}
			design.Toolbar(c).Children(func() {
				design.Text(c, fmt.Sprintf("%d / %d reviewed", count, len(a.state.Diff.Files)), design.Muted).Grow(1)
				if design.Button(c, "Reset", design.Quiet).Label("Reset reviewed files").Disabled(count == 0 || a.state.Saving).Clicked() {
					a.requestConfirm("Reset reviewed files?", "All reviewed marks in this comparison will be cleared.", func() { a.track(a.controller.ClearReviewed()) })
				}
			})
		}
	})
}

func (a *application) fileTree(c *ui.Context, files []goclient.ChangedFile, prefix string) {
	folders := map[string][]goclient.ChangedFile{}
	var leaves []goclient.ChangedFile
	for _, file := range files {
		relative := strings.TrimPrefix(file.Path, prefix)
		if i := strings.IndexByte(relative, '/'); i >= 0 {
			folder := relative[:i]
			folders[folder] = append(folders[folder], file)
		} else {
			leaves = append(leaves, file)
		}
	}
	names := make([]string, 0, len(folders))
	for name := range folders {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		key := prefix + name
		open := !a.folderCollapsed[key]
		ui.TreeItem(c.Key(key), name, &open, func() { a.fileTree(c, folders[name], key+"/") }).FillWidth()
		a.folderCollapsed[key] = !open
	}
	for _, file := range leaves {
		a.fileRow(c, file, true)
	}
}

func (a *application) fileRow(c *ui.Context, file goclient.ChangedFile, tree bool) {
	label := file.Path
	if tree {
		label = path.Base(file.Path)
	}
	label = fmt.Sprintf("%s  %s  +%d -%d", label, file.Status, file.Additions, file.Deletions)
	if a.reviewed(file) {
		label += "  Reviewed"
	}
	current, outdated := 0, 0
	for _, comment := range a.state.Comments {
		if comment.Path == file.Path {
			if commentOutdated(comment) {
				outdated++
			} else {
				current++
			}
		}
	}
	if current > 0 {
		label += fmt.Sprintf("  %d comments", current)
	}
	if outdated > 0 {
		label += fmt.Sprintf("  %d outdated", outdated)
	}
	var row ui.Element
	if tree {
		row = ui.TreeItem(c.Key(file.Id), label, nil, nil)
	} else {
		row = design.Button(c.Key(file.Id), label, design.Quiet)
	}
	row = row.Label("Open file " + file.Path).Tooltip(file.Path + " · " + label).Selected(a.selectedFile == file.Id).FillWidth()
	if row.Clicked() {
		a.selectFile(file.Id)
	}
	if a.selectedFile == file.Id && (a.jumpFile != "" || a.revealFile == file.Id) {
		row.ScrollIntoView()
		a.revealFile = ""
	}
}

func (a *application) track(id uint64) {
	if id != 0 {
		a.pending[id] = pendingAction{}
	}
}
