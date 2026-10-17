package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/flexdinesh/diffx/internal/diffsource"
)

// Discovery bounds keep a broad workspace from scanning or collecting without limit.
const (
	directoryLimit = 4096
	checkoutLimit  = 256
)

type DiscoverySource struct {
	InputPath string
	Path      string
	Branch    string
	Base      string
	Identity  string
}

type Discovery struct {
	Sources     []DiscoverySource
	Diagnostics []string
}

// SourceIdentity uses persisted Git identities. Paths and branch labels are
// location hints, not the source's identity. Branch identifies object-only
// recovery; an empty branch identifies the live checkout.
func SourceIdentity(ctx context.Context, root, sourceID, branch, base string) (string, error) {
	if base == "" {
		base = "auto"
	}
	label := branch
	if label == "" {
		var err error
		label, err = diffsource.CurrentBranch(ctx, root)
		if err != nil {
			return "", err
		}
	}
	identity, err := diffsource.StableIdentity(ctx, root, sourceID, label)
	if err != nil {
		return "", err
	}
	checkout := identity.CheckoutKey
	if branch != "" {
		checkout = diffsource.BranchCheckoutKey(identity.RepositoryKey, identity.BranchID)
	}
	return hash("collector-source-v1", sourceID, identity.RepositoryKey, checkout, identity.BranchID, base), nil
}

// DiscoverCheckout resolves only the selected live checkout. Non-Git input
// returns ErrNotRepository; callers decide whether to ignore a hook event.
func DiscoverCheckout(ctx context.Context, input, sourceID, base string) (Discovery, error) {
	var result Discovery
	input, err := filepath.Abs(input)
	if err != nil {
		return result, err
	}
	if base == "" {
		base = "auto"
	}
	source, err := diffsource.OpenRepository(ctx, input)
	if err != nil {
		return result, err
	}
	identity, err := SourceIdentity(ctx, source.Root(), sourceID, "", base)
	if err != nil {
		return result, err
	}
	result.Sources = []DiscoverySource{{InputPath: input, Path: source.Root(), Base: base, Identity: identity}}
	return result, nil
}

// DiscoverWorkspaces finds every live checkout under the supplied paths. A path
// inside a repository resolves to its root; otherwise directories are walked up
// to depth levels, stopping at repositories and skipping hidden, node_modules
// and vendor directories. Each repository contributes all registered worktrees,
// including those outside the paths. It never fetches, switches branches,
// creates worktrees or follows symbolic links.
func DiscoverWorkspaces(ctx context.Context, inputs []string, depth int, sourceID, base string) (Discovery, error) {
	var result Discovery
	if base == "" {
		base = "auto"
	}
	type repository struct{ input, root string }
	roots := make([]repository, 0)
	visited := 0
	visitLimitReported := false
	var scan func(input, path string, remaining int) error
	scan = func(input, path string, remaining int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if visited == directoryLimit {
			if !visitLimitReported {
				result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("workspace discovery reached %d-directory limit; narrow --path or --scan-depth", directoryLimit))
				visitLimitReported = true
			}
			return nil
		}
		visited++
		entries, err := os.ReadDir(path)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("read workspace %s: %v", path, err))
			return nil
		}
		for _, entry := range entries {
			if entry.Name() != ".git" {
				continue
			}
			// Only a .git entry can start a repository below a non-repository parent.
			source, err := diffsource.OpenRepository(ctx, path)
			if err == nil {
				roots = append(roots, repository{input, source.Root()})
				return nil
			}
			if !errors.Is(err, diffsource.ErrNotRepository) {
				result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("inspect %s: %v", path, err))
				return nil
			}
		}
		if remaining == 0 {
			return nil
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "vendor" {
				continue
			}
			if err := scan(input, filepath.Join(path, entry.Name()), remaining-1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, input := range inputs {
		input, err := filepath.Abs(input)
		if err != nil {
			return result, err
		}
		if info, err := os.Stat(input); err != nil || !info.IsDir() {
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("workspace unavailable %s: not a readable directory", input))
			continue
		}
		// An explicit path may sit anywhere inside a repository.
		source, err := diffsource.OpenRepository(ctx, input)
		if err == nil {
			roots = append(roots, repository{input, source.Root()})
			continue
		}
		if !errors.Is(err, diffsource.ErrNotRepository) {
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("inspect %s: %v", input, err))
			continue
		}
		if err := scan(input, input, depth); err != nil {
			return result, err
		}
	}
	seenRepositories, seenSources := make(map[string]bool), make(map[string]string)
	sourceLimitReported := false
	limitReached := func() bool {
		if len(result.Sources) < checkoutLimit {
			return false
		}
		if !sourceLimitReported {
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("checkout discovery reached %d-checkout limit; narrow --path or --scan-depth", checkoutLimit))
			sourceLimitReported = true
		}
		return true
	}
	for _, found := range roots {
		if limitReached() {
			break
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		facts, err := diffsource.Metadata(ctx, found.root)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("identify repository %s: %v", found.root, err))
			continue
		}
		// Enumeration deduplicates physical Git stores only. Independent clones
		// can share a repository key while retaining distinct checkout identities.
		if seenRepositories[facts.CommonDir] {
			continue
		}
		seenRepositories[facts.CommonDir] = true
		_, worktrees, err := diffsource.RepositoryWorktrees(ctx, found.root)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("list worktrees %s: %v", found.root, err))
			continue
		}
		for _, worktree := range worktrees {
			if limitReached() {
				break
			}
			if _, err := os.Stat(worktree.Root); err != nil {
				result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("worktree unavailable %s: %v", worktree.Root, err))
				continue
			}
			identity, err := SourceIdentity(ctx, worktree.Root, sourceID, "", base)
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("identify %s: %v", worktree.Root, err))
				continue
			}
			if previous, exists := seenSources[identity]; exists {
				if previous != worktree.Root {
					result.Diagnostics = append(result.Diagnostics, fmt.Sprintf("collector identity copied across %s and %s; duplicate source skipped, use separate source IDs", previous, worktree.Root))
				}
				continue
			}
			seenSources[identity] = worktree.Root
			result.Sources = append(result.Sources, DiscoverySource{InputPath: found.input, Path: worktree.Root, Base: base, Identity: identity})
		}
	}
	return result, ctx.Err()
}
