package workspace

import (
	"cmp"
	"slices"
	"strings"

	"github.com/flexdinesh/diffx/packages/api/goclient"
	"github.com/oapi-codegen/nullable"
)

func value[T any](n nullable.Nullable[T]) T { v, _ := n.Get(); return v }
func IsPiped(c goclient.Context) bool       { return c.Source != nil && *c.Source == "stdin" }
func StreamKey(c goclient.Context) string {
	if id := value(c.StreamId); id != "" {
		return id
	}
	return c.Id
}
func ProjectKey(c goclient.Context) string {
	if IsPiped(c) {
		return c.Id
	}
	if id := value(c.RepositoryId); id != "" {
		return id
	}
	return "name:" + c.Name
}
func Activity(c goclient.Context) int64 {
	if c.LastChangedAt > 0 {
		return c.LastChangedAt
	}
	if c.Observation != nil {
		return c.Observation.CollectedAt
	}
	return c.CreatedAt
}
func Timestamp(c goclient.Context) int64 {
	if c.Observation != nil {
		return c.Observation.CollectedAt
	}
	return c.CreatedAt
}
func CompareActivity(a, b goclient.Context) int {
	if n := cmp.Compare(Activity(b), Activity(a)); n != 0 {
		return n
	}
	if n := strings.Compare(StreamKey(a), StreamKey(b)); n != 0 {
		return n
	}
	return strings.Compare(a.Id, b.Id)
}
func HasChanges(c goclient.Context) bool {
	return c.Availability != "unavailable" && value(c.ChangedFileCount) > 0
}
func stale(c goclient.Context) bool { return !IsPiped(c) && c.Stale != nil && *c.Stale }
func CurrentWork(c goclient.Context) bool {
	if stale(c) || !HasChanges(c) {
		return false
	}
	if !IsPiped(c) && c.Observation != nil && c.Observation.PullRequest != nil {
		state := c.Observation.PullRequest.State
		return state != "closed" && state != "merged"
	}
	return true
}

// ComparisonKind reads captured facts, including legacy captures, never Git.
func ComparisonKind(c goclient.Context, scope goclient.DiffMode) goclient.ComparisonKind {
	if c.ScopeSummaries != nil {
		for _, summary := range *c.ScopeSummaries {
			if summary.Scope == scope {
				return summary.ComparisonKind
			}
		}
	}
	if scope != "all" {
		return goclient.ComparisonKind(scope)
	}
	if IsPiped(c) {
		return "patch"
	}
	if c.Observation != nil && c.Observation.Comparison != nil && c.Observation.Comparison.Kind == "branch" {
		if c.Capabilities.Diff.StagingMetadata.State == "enabled" {
			return "combined"
		}
		return "committed"
	}
	return "uncommitted"
}

func DefaultScope(c goclient.Context) goclient.DiffMode {
	find := func(kind goclient.ComparisonKind) goclient.DiffMode {
		for _, scope := range c.Capabilities.Diff.Scopes.Values {
			if ComparisonKind(c, scope) == kind {
				return scope
			}
		}
		return ""
	}
	uncommitted := find("uncommitted")
	if c.ScopeSummaries != nil {
		for _, summary := range *c.ScopeSummaries {
			if summary.FileCount > 0 && (summary.ComparisonKind == "staged" || summary.ComparisonKind == "unstaged") && uncommitted != "" {
				return uncommitted
			}
		}
	}
	if committed := find("committed"); committed != "" {
		return committed
	}
	if uncommitted != "" {
		return uncommitted
	}
	if len(c.Capabilities.Diff.Scopes.Values) > 0 {
		return c.Capabilities.Diff.Scopes.Values[0]
	}
	return "all"
}

func ScopeAvailable(capabilities goclient.SessionCapabilities, scope goclient.DiffMode) bool {
	return capabilities.Diff.Scopes.State == "enabled" && slices.Contains(capabilities.Diff.Scopes.Values, scope)
}

type ProjectStream struct {
	ID      string
	Context goclient.Context
	Unseen  bool
}
type ProjectGroup struct {
	ID            string
	Name          string
	Streams       []ProjectStream
	LastActivity  int64
	MultipleHosts bool
}

func ProjectGroups(heads []goclient.Context, seen map[string]string) []ProjectGroup {
	groups := map[string]*ProjectGroup{}
	for _, head := range heads {
		if !CurrentWork(head) {
			continue
		}
		key := ProjectKey(head)
		if groups[key] == nil {
			name := head.Name
			if IsPiped(head) {
				name = "Piped"
			}
			groups[key] = &ProjectGroup{ID: key, Name: name}
		}
		group := groups[key]
		stream := StreamKey(head)
		group.Streams = append(group.Streams, ProjectStream{ID: stream, Context: head, Unseen: seen != nil && seen[stream] != head.Id})
		group.LastActivity = max(group.LastActivity, Activity(head))
	}
	result := make([]ProjectGroup, 0, len(groups))
	for _, group := range groups {
		slices.SortFunc(group.Streams, func(a, b ProjectStream) int { return CompareActivity(a.Context, b.Context) })
		hosts := map[string]bool{}
		for _, stream := range group.Streams {
			host := ""
			if !IsPiped(stream.Context) && stream.Context.Observation != nil {
				host = stream.Context.Observation.Hostname
				if host == "" {
					host = stream.Context.Observation.SourceId
				}
			}
			hosts[host] = true
		}
		group.MultipleHosts = len(hosts) > 1
		result = append(result, *group)
	}
	slices.SortFunc(result, func(a, b ProjectGroup) int {
		if n := CompareActivity(a.Streams[0].Context, b.Streams[0].Context); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return result
}

type Filters struct {
	Freshness  string
	IncludeAll bool
	Hosts      []string
	Branches   []string
	Worktrees  []string
}

func DefaultFilters() Filters { return Filters{Freshness: "latest"} }
func (c *Controller) SetFilters(filters Filters) {
	c.change(func(s *State) { s.Filters = detached(filters) })
}

// PickerResults mirrors project-picker.ts: OR within filters, AND between;
// changed snapshots first, then submission time, then fuzzy metadata score.
func PickerResults(contexts []goclient.Context, query string, filters Filters) []goclient.Context {
	type scored struct {
		context goclient.Context
		score   int
	}
	entries := []scored{}
	for _, c := range contexts {
		if !filters.IncludeAll && !HasChanges(c) {
			continue
		}
		if !IsPiped(c) {
			if filters.Freshness == "latest" && stale(c) || filters.Freshness == "stale" && !stale(c) {
				continue
			}
			host, branch, worktree := "", value(c.Branch), value(c.WorktreeName)
			if c.Observation != nil {
				host = c.Observation.Hostname
				if branch == "" {
					branch = c.Observation.Branch
				}
				if worktree == "" {
					worktree = c.Observation.WorktreeName
				}
			}
			if len(filters.Hosts) > 0 && !slices.Contains(filters.Hosts, host) || len(filters.Branches) > 0 && !slices.Contains(filters.Branches, branch) || len(filters.Worktrees) > 0 && !slices.Contains(filters.Worktrees, worktree) {
				continue
			}
		}
		values := []string{c.Name, value(c.Branch), value(c.WorktreeName), value(c.Root)}
		if IsPiped(c) {
			values = []string{"Piped"}
		} else if o := c.Observation; o != nil {
			values = append(values, o.RepositoryName, o.RemoteUrl, o.Hostname, o.SourceId, o.RunId, o.Agent, o.Trigger)
		}
		if c.Sessions != nil {
			for _, session := range *c.Sessions {
				values = append(values, session.SourceId, session.Harness, session.Id)
				if session.Name != nil {
					values = append(values, *session.Name)
				}
			}
		}
		score, matched := 0, true
		for _, word := range strings.Fields(strings.ToLower(query)) {
			best := 0
			for _, text := range values {
				best = max(best, matchScore(strings.ToLower(text), word))
			}
			if best == 0 {
				matched = false
			}
			score += best
		}
		if matched {
			entries = append(entries, scored{c, score})
		}
	}
	slices.SortFunc(entries, func(a, b scored) int {
		if IsPiped(a.context) && IsPiped(b.context) {
			if n := cmp.Compare(Timestamp(b.context), Timestamp(a.context)); n != 0 {
				return n
			}
		}
		if HasChanges(a.context) != HasChanges(b.context) {
			if HasChanges(a.context) {
				return -1
			}
			return 1
		}
		if n := cmp.Compare(b.context.LastSubmittedAt, a.context.LastSubmittedAt); n != 0 {
			return n
		}
		if n := cmp.Compare(b.score, a.score); n != 0 {
			return n
		}
		if n := strings.Compare(a.context.Name, b.context.Name); n != 0 {
			return n
		}
		if n := strings.Compare(value(a.context.Branch), value(b.context.Branch)); n != 0 {
			return n
		}
		return strings.Compare(a.context.Id, b.context.Id)
	})
	result := make([]goclient.Context, len(entries))
	for i, entry := range entries {
		result[i] = entry.context
	}
	return result
}
func matchScore(text, word string) int {
	if text == word {
		return 4
	}
	if strings.HasPrefix(text, word) {
		return 3
	}
	if strings.Contains(text, word) {
		return 2
	}
	remaining := []rune(word)
	position := 0
	for _, letter := range text {
		if position < len(remaining) && letter == remaining[position] {
			position++
		}
	}
	if position == len(remaining) {
		return 1
	}
	return 0
}

// FilesInTreeOrder keeps diff, file list and keyboard progression aligned with
// the web file-tree traversal: folders before leaves at every path segment.
func FilesInTreeOrder(files []goclient.ChangedFile) []goclient.ChangedFile {
	ordered := slices.Clone(files)
	slices.SortStableFunc(ordered, func(a, b goclient.ChangedFile) int {
		left, right := strings.Split(a.Path, "/"), strings.Split(b.Path, "/")
		for i := 0; i < min(len(left), len(right)); i++ {
			folderA, folderB := i < len(left)-1, i < len(right)-1
			if folderA != folderB {
				if folderA {
					return -1
				}
				return 1
			}
			if n := strings.Compare(left[i], right[i]); n != 0 {
				return n
			}
		}
		return strings.Compare(a.Id, b.Id)
	})
	return ordered
}

// Identity is safe for display even when a stdin capture carries producer paths.
type Identity struct {
	Name, Branch, Worktree, Root, Host string
	Piped                              bool
}

func DisplayIdentity(c goclient.Context) Identity {
	if IsPiped(c) {
		return Identity{Name: "Piped", Piped: true}
	}
	i := Identity{Name: c.Name, Branch: value(c.Branch), Worktree: value(c.WorktreeName), Root: value(c.Root)}
	if o := c.Observation; o != nil {
		i.Host = o.Hostname
		if i.Branch == "" {
			i.Branch = o.Branch
		}
		if i.Worktree == "" {
			i.Worktree = o.WorktreeName
		}
		if i.Root == "" {
			i.Root = o.Root
		}
	}
	return i
}
