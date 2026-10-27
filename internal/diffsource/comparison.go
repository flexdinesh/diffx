package diffsource

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/flexdinesh/diffx/internal/review"
)

// ErrNoDefaultBranch means no existing default branch ref can be resolved.
var ErrNoDefaultBranch = errors.New("no default branch available for comparison")

// ErrComparisonUnavailable permits replaying an already captured comparison
// after its local refs disappear. It never masks process or cancellation errors.
var ErrComparisonUnavailable = errors.New("comparison source unavailable")

type Comparison struct {
	BaseRef     string
	BaseCommit  string
	BaseOID     string
	HeadOID     string
	ObjectsOnly bool
}

type gitComparison struct {
	Comparison
	branch string
	target string
}

// ComparisonInfo reports the captured comparison, including its merge base.
// A false result denotes ordinary checkout collection against HEAD.
func ComparisonInfo(source Source) (Comparison, bool) {
	git, ok := source.(*gitSource)
	if !ok || git.comparison == nil {
		return Comparison{}, false
	}
	return git.comparison.Comparison, true
}

// DefaultBaselines resolves existing refs for the first default branch name
// (`origin/HEAD`, `main`, then `master`) with a local or remote-tracking ref:
// the local branch first, then its remote-tracking upstream or origin
// counterpart. It never fetches or consults a feature branch's upstream, which
// may simply track that same feature branch.
func DefaultBaselines(ctx context.Context, root string) ([]string, error) {
	remote, err := runGit(ctx, root, 16<<10, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	if err != nil && !comparisonGitExit(err, 1) {
		return nil, err
	}
	candidates := []string{"main", "master"}
	if branch, ok := strings.CutPrefix(strings.TrimSpace(remote), "refs/remotes/origin/"); ok {
		candidates = append([]string{branch}, candidates...)
	}
	for _, branch := range candidates {
		local := "refs/heads/" + branch
		var baselines []string
		exists, err := refExists(ctx, root, local)
		if err != nil {
			return nil, err
		}
		tracking := "refs/remotes/origin/" + branch
		if exists {
			baselines = append(baselines, local)
			upstream, err := runGit(ctx, root, 16<<10, "for-each-ref", "--format=%(upstream)", local)
			if err != nil {
				return nil, err
			}
			if upstream = strings.TrimSpace(upstream); strings.HasPrefix(upstream, "refs/remotes/") {
				if exists, err := refExists(ctx, root, upstream); err != nil {
					return nil, err
				} else if exists {
					tracking = upstream
				}
			}
		}
		if exists, err := refExists(ctx, root, tracking); err != nil {
			return nil, err
		} else if exists {
			baselines = append(baselines, tracking)
		}
		if len(baselines) > 0 {
			return baselines, nil
		}
	}
	return nil, ErrNoDefaultBranch
}

func refExists(ctx context.Context, root, ref string) (bool, error) {
	if _, err := resolveCommit(ctx, root, ref); err == nil {
		return true, nil
	} else if !comparisonGitExit(err, 128) {
		return false, err
	}
	return false, nil
}

// nearestBaseline keeps the first baseline unless a later one's merge base
// strictly descends from it; ties and divergence keep the earlier baseline.
// Baselines with unrelated histories are skipped; if none relate, the first
// remains so the caller reports its usual fallback or failure.
func nearestBaseline(ctx context.Context, root, head string, baselines []string) (string, error) {
	chosen, chosenBase := baselines[0], ""
	for _, baseline := range baselines {
		raw, err := runGit(ctx, root, 1024, "merge-base", baseline, head)
		if comparisonGitExit(err, 1) {
			continue
		}
		if err != nil {
			return "", err
		}
		mergeBase := strings.TrimSpace(raw)
		if chosenBase == "" {
			chosen, chosenBase = baseline, mergeBase
			continue
		}
		if mergeBase == chosenBase {
			continue
		}
		_, err = runGit(ctx, root, 1024, "merge-base", "--is-ancestor", chosenBase, mergeBase)
		if err == nil {
			chosen, chosenBase = baseline, mergeBase
		} else if !comparisonGitExit(err, 1) {
			return "", err
		}
	}
	return chosen, nil
}

func resolveCommit(ctx context.Context, root, ref string) (string, error) {
	raw, err := runGit(ctx, root, 1024, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(raw), err
}

// OpenComparison captures a branch comparison without switching or creating a
// checkout. An empty branch reads the current checkout; a named branch reads
// immutable objects only, ignoring the current checkout's index and files.
func OpenComparison(ctx context.Context, directory, base, branch string) (Source, error) {
	opened, err := OpenRepository(ctx, directory)
	if err != nil {
		return nil, err
	}
	source, ok := opened.(*gitSource)
	if !ok {
		return nil, errors.New("Git comparison source unavailable")
	}
	auto := base == "auto" || base == "" && branch != ""
	if (base == "" || base == "HEAD") && branch == "" {
		return source, nil
	}
	var baselines []string
	if auto {
		baselines, err = DefaultBaselines(ctx, source.root)
		if errors.Is(err, ErrNoDefaultBranch) && branch == "" {
			return source, nil
		}
		if err != nil {
			if errors.Is(err, ErrNoDefaultBranch) {
				return nil, fmt.Errorf("%w: %w", ErrComparisonUnavailable, err)
			}
			return nil, err
		}
		base = baselines[0]
	}
	target := "HEAD"
	if branch != "" {
		target = "refs/heads/" + branch
	}
	head, err := resolveCommit(ctx, source.root, target)
	if err != nil {
		if auto && branch == "" && comparisonGitExit(err, 128) {
			return source, nil // Unborn checkout: retain normal empty-tree collection.
		}
		return nil, comparisonFailure(fmt.Sprintf("resolve comparison target %q", target), err, 128)
	}
	if len(baselines) > 1 {
		if base, err = nearestBaseline(ctx, source.root, head, baselines); err != nil {
			return nil, fmt.Errorf("find comparison merge base: %w", err)
		}
	}
	baseTip, err := resolveCommit(ctx, source.root, base)
	if err != nil {
		return nil, comparisonFailure(fmt.Sprintf("resolve comparison baseline %q", base), err, 128)
	}
	// Store symbolic refs in a single form (main and refs/heads/main agree).
	// Literal commit baselines retain their resolved object ID.
	baseRef, err := runGit(ctx, source.root, 16<<10, "rev-parse", "--symbolic-full-name", "--verify", "--end-of-options", base)
	if err != nil {
		return nil, comparisonFailure(fmt.Sprintf("resolve comparison reference %q", base), err, 128)
	}
	base = strings.TrimSpace(baseRef)
	if base == "" {
		base = baseTip
	}
	mergeBase, err := runGit(ctx, source.root, 1024, "merge-base", baseTip, head)
	if err != nil {
		if auto && branch == "" && comparisonGitExit(err, 1) {
			return source, nil // Unrelated histories cannot supply a branch baseline.
		}
		return nil, comparisonFailure("find comparison merge base", err, 1)
	}
	if branch == "" {
		label, err := source.branch(ctx, &head)
		if err != nil {
			return nil, err
		}
		branch = label
	}
	source.comparison = &gitComparison{
		Comparison: Comparison{BaseRef: base, BaseCommit: baseTip, BaseOID: strings.TrimSpace(mergeBase), HeadOID: head, ObjectsOnly: target != "HEAD"},
		branch:     branch, target: target,
	}
	return source, nil
}

func comparisonFailure(operation string, err error, unavailableCode int) error {
	if comparisonGitExit(err, unavailableCode) {
		return fmt.Errorf("%s: %w: %w", operation, ErrComparisonUnavailable, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func comparisonGitExit(err error, code int) bool {
	var failure *gitFailure
	var exit *exec.ExitError
	return errors.As(err, &failure) && errors.As(failure.err, &exit) && exit.ExitCode() == code
}

func (source *gitSource) verifyComparison(ctx context.Context) error {
	if source.comparison == nil {
		return nil
	}
	comparison := source.comparison
	for _, reference := range []struct{ ref, oid string }{{comparison.target, comparison.HeadOID}, {comparison.BaseRef, comparison.BaseCommit}} {
		current, err := resolveCommit(ctx, source.root, reference.ref)
		if err != nil {
			var failure *gitFailure
			if !errors.As(err, &failure) {
				return err
			}
		}
		if current != reference.oid {
			return Error(409, "Comparison ref changed during collection: %s", reference.ref)
		}
	}
	if !comparison.ObjectsOnly {
		branch, err := source.branch(ctx, &comparison.HeadOID)
		if err != nil {
			return err
		}
		if branch != comparison.branch {
			return Error(409, "Checkout branch changed during collection")
		}
	}
	return nil
}

type endpointKind uint8

const (
	objectEndpoint endpointKind = iota
	indexEndpoint
	worktreeEndpoint
)

type gitEndpoint struct {
	kind     endpointKind
	revision string
}

// comparisonEndpoints owns scope semantics for manifests, previews and identity.
// An empty object revision is never an index: unborn HEAD uses the empty tree.
type comparisonEndpoints struct {
	before, after gitEndpoint
	kind          review.ComparisonKind
}

func (source *gitSource) endpoints(mode review.DiffMode, headBase string) (comparisonEndpoints, error) {
	if !slices.Contains(source.Support().Scopes, mode) {
		return comparisonEndpoints{}, Error(400, "Unsupported comparison scope: %s", mode)
	}
	endpoints := comparisonEndpoints{
		before: gitEndpoint{kind: objectEndpoint, revision: headBase},
		after:  gitEndpoint{kind: worktreeEndpoint},
		kind:   review.ComparisonUncommitted,
	}
	switch mode {
	case review.DiffCommit:
		endpoints.before = gitEndpoint{kind: objectEndpoint, revision: source.commit.parent}
		endpoints.after = gitEndpoint{kind: objectEndpoint, revision: source.commit.oid}
		endpoints.kind = review.ComparisonCommit
	case review.DiffAll:
		if source.comparison != nil {
			endpoints.before.revision = source.comparison.BaseOID
			endpoints.kind = review.ComparisonCombined
			if source.objectsOnly() {
				endpoints.after = gitEndpoint{kind: objectEndpoint, revision: source.comparison.HeadOID}
				endpoints.kind = review.ComparisonCommitted
			}
		}
	case review.DiffCommitted:
		endpoints.before.revision = source.comparison.BaseOID
		endpoints.after = gitEndpoint{kind: objectEndpoint, revision: source.comparison.HeadOID}
		endpoints.kind = review.ComparisonCommitted
	case review.DiffStaged:
		endpoints.after = gitEndpoint{kind: indexEndpoint}
		endpoints.kind = review.ComparisonStaged
	case review.DiffUnstaged:
		endpoints.before = gitEndpoint{kind: indexEndpoint}
		endpoints.kind = review.ComparisonUnstaged
	}
	return endpoints, nil
}

func (endpoints comparisonEndpoints) objectsOnly() bool {
	return endpoints.before.kind == objectEndpoint && endpoints.after.kind == objectEndpoint
}

func (source *gitSource) previewEndpoints(ctx context.Context, mode review.DiffMode, head *string) (comparisonEndpoints, error) {
	if err := ctx.Err(); err != nil {
		return comparisonEndpoints{}, err
	}
	if head != nil && source.commit == nil {
		return source.endpoints(mode, *head)
	}
	_, base, err := source.headAndBase(ctx)
	if err != nil {
		return comparisonEndpoints{}, err
	}
	return source.endpoints(mode, base)
}

func (endpoints comparisonEndpoints) diffArgs() []string {
	args := []string{"-c", "diff.autoRefreshIndex=false", "diff", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", "--find-renames"}
	args = append(args, patchFormat...)
	if endpoints.after.kind == indexEndpoint {
		args = append(args, "--cached")
	}
	if endpoints.before.kind == objectEndpoint {
		args = append(args, endpoints.before.revision)
	}
	if endpoints.after.kind == objectEndpoint {
		args = append(args, endpoints.after.revision)
	}
	return args
}

func (source *gitSource) objectsOnly() bool {
	return source.comparison != nil && source.comparison.ObjectsOnly
}

func (source *gitSource) objectSubmodule(ctx context.Context, endpoints comparisonEndpoints, file review.ChangedFile) (bool, error) {
	previous := file.Path
	if file.OldPath != nil {
		previous = *file.OldPath
	}
	for _, object := range []struct{ revision, path string }{{endpoints.before.revision, previous}, {endpoints.after.revision, file.Path}} {
		mode, err := runGit(ctx, source.root, 1024, "ls-tree", "--format=%(objectmode)", object.revision, "--", object.path)
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(mode) == "160000" {
			return true, nil
		}
	}
	return false, nil
}
