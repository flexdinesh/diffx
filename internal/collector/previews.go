package collector

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/flexdinesh/diffx/internal/diffsource"
	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/review"
)

// previewLimits bound one observation's previews. Per-file and file-count
// limits are primary; requestBytes is a safety net measured on the encoded
// request so every observation fits one ingestion request.
type previewLimits struct {
	requestBytes  int
	patchBytes    int
	contentsBytes int
	files         int
}

var defaultPreviewLimits = previewLimits{
	requestBytes:  ingestion.MaxRequestBytes - 1<<20,
	patchBytes:    1 << 20,
	contentsBytes: 1 << 20,
	files:         1000,
}

var (
	overBudgetMessage         = "Comparison exceeds the preview size limit."
	contentsOverBudgetMessage = "Full contents unavailable: comparison exceeds the preview size limit."
)

// previewCandidate is a captured patch that may still receive full contents;
// base is its preview before the contents budget fallback message.
type previewCandidate struct {
	position int
	file     review.ChangedFile
	base     review.FilePatch
}

// withPreviews captures every scope's patches before any full contents, so
// full-file views degrade before files lose their patch. Each pass shares
// what remains of the request budget between scopes; see allocate.
func withPreviews(ctx context.Context, source diffsource.Source, request ingestion.Request, limits previewLimits) (ingestion.Request, error) {
	scopes := make([]ingestion.Scope, len(request.Scopes))
	counts := make([]int, len(scopes))
	for index, scope := range request.Scopes {
		patches := make(map[string]review.FilePatch, len(scope.Snapshot.Files))
		for position, file := range scope.Snapshot.Files {
			patches[file.ID] = review.FilePatch{Message: diffsource.Message(overBudgetMessage)}
			if position >= limits.files {
				patches[file.ID] = review.FilePatch{Message: diffsource.Message("Not captured: comparison exceeds 1000 files.")}
			}
		}
		scopes[index] = ingestion.Scope{Snapshot: scope.Snapshot, Patches: patches}
		counts[index] = min(len(scope.Snapshot.Files), limits.files)
	}
	request.Scopes = scopes
	skeleton, err := json.Marshal(request)
	if err != nil {
		return ingestion.Request{}, err
	}
	budget := max(limits.requestBytes-len(skeleton), 0)
	candidates := make([][]previewCandidate, len(scopes))
	budget, err = allocate(counts, budget, func(index, position, limit int) (int, bool, error) {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		snapshot := scopes[index].Snapshot
		file := snapshot.Files[position]
		direct, err := source.Patch(ctx, snapshot.Mode, file, snapshot.Head)
		if err != nil {
			return 0, false, err
		}
		patch := direct
		if len(patch.Patch) > limits.patchBytes {
			patch = review.FilePatch{Message: diffsource.Message("Diff exceeds the 1 MiB preview limit.")}
		}
		wantsContents := source.Support().FileContents && patch.Contents == nil && !file.Binary && file.Status != "U" && !nonTextPreview(patch)
		if patch.Contents != nil && !contentsFit(*patch.Contents, limits) {
			patch.Contents, patch.Message = nil, diffsource.Message("Full contents unavailable: file exceeds the 1 MiB preview limit.")
		}
		base := patch
		if wantsContents && patch.Message == nil {
			patch.Message = diffsource.Message(contentsOverBudgetMessage)
		} else if !source.Support().FileContents && patch.Message == nil {
			patch.Message = diffsource.Message("Full contents unavailable for this snapshot.")
		}
		cost := growth(file.ID, scopes[index].Patches[file.ID], patch)
		if cost > limit {
			return cost, false, nil
		}
		scopes[index].Patches[file.ID] = patch
		if wantsContents {
			candidates[index] = append(candidates[index], previewCandidate{position: position, file: file, base: base})
		}
		return cost, true, nil
	})
	if err != nil {
		return ingestion.Request{}, err
	}
	for index := range candidates {
		slices.SortFunc(candidates[index], func(left, right previewCandidate) int { return left.position - right.position })
		counts[index] = len(candidates[index])
	}
	_, err = allocate(counts, budget, func(index, position, limit int) (int, bool, error) {
		if err := ctx.Err(); err != nil {
			return 0, false, err
		}
		snapshot := scopes[index].Snapshot
		candidate := candidates[index][position]
		patch := candidate.base
		contents, err := source.Contents(ctx, snapshot.Mode, candidate.file, snapshot.Head)
		switch {
		case err == nil && !contentsFit(contents, limits):
			if patch.Message == nil {
				patch.Message = diffsource.Message("Full contents unavailable: file exceeds the 1 MiB preview limit.")
			}
		case err == nil:
			patch.Contents, patch.Message = &contents, nil
		case diffsource.PreviewUnavailable(err):
			if err := ctx.Err(); err != nil {
				return 0, false, err
			}
			if patch.Message == nil {
				patch.Message = diffsource.Message("Full contents unavailable: file is binary or exceeds the preview limit.")
			}
		default:
			return 0, false, err
		}
		cost := growth(candidate.file.ID, scopes[index].Patches[candidate.file.ID], patch)
		if cost > limit {
			return cost, false, nil
		}
		scopes[index].Patches[candidate.file.ID] = patch
		return cost, true, nil
	})
	if err != nil {
		return ingestion.Request{}, err
	}
	return request, nil
}

// allocate spends budget on each scope's candidates in file order and returns
// what is left. Rounds split the remaining budget equally between scopes with
// candidates that might still fit, so no scope can consume another's share and
// a share a scope does not need goes to the others in later rounds. Within its
// share a scope skips a candidate that does not fit and continues. When no
// share fits anything, one shared pool gives the rest to scopes in order.
// try measures a candidate and keeps it when its cost fits limit; known costs
// avoid measuring a candidate again until it can fit.
func allocate(counts []int, budget int, try func(scope, position, limit int) (int, bool, error)) (int, error) {
	costs := make([][]int, len(counts))
	taken := make([][]bool, len(counts))
	for index, count := range counts {
		costs[index], taken[index] = make([]int, count), make([]bool, count)
		for position := range costs[index] {
			costs[index][position] = -1
		}
	}
	pooled := false
	for {
		active := make([]int, 0, len(counts))
		for index := range counts {
			for position := range counts[index] {
				if !taken[index][position] && costs[index][position] <= budget {
					active = append(active, index)
					break
				}
			}
		}
		if len(active) == 0 {
			return budget, nil
		}
		share := budget / len(active)
		progress := false
		for _, index := range active {
			limit := share
			if pooled {
				limit = budget
			}
			for position := range counts[index] {
				if limit == 0 {
					break
				}
				if taken[index][position] || costs[index][position] > limit {
					continue
				}
				cost, kept, err := try(index, position, limit)
				if err != nil {
					return budget, err
				}
				costs[index][position] = cost
				if kept {
					taken[index][position], progress = true, true
					limit -= cost
					budget -= cost
				}
			}
		}
		if !progress {
			if pooled || len(active) == 1 {
				return budget, nil
			}
			pooled = true
		}
	}
}

func contentsFit(contents review.FileContents, limits previewLimits) bool {
	return len(contents.Before) <= limits.contentsBytes && len(contents.After) <= limits.contentsBytes
}

// growth is the encoded request bytes replacing one file's preview adds.
func growth(id string, current, next review.FilePatch) int {
	return max(encodedPatch(id, next)-encodedPatch(id, current), 0)
}

// encodedPatch measures a file's patches map entry as json.Marshal encodes
// the request, including HTML escaping. Encoding strings and FilePatch values
// cannot fail.
func encodedPatch(id string, patch review.FilePatch) int {
	key, _ := json.Marshal(id)
	value, _ := json.Marshal(patch)
	return len(key) + len(value)
}
