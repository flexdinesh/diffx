package workspace

import (
	"errors"
	"slices"

	"github.com/flexdinesh/diffx/packages/api/goclient"
)

const maxCachedFiles = 32
const maxPreviewBytes = 16 * 1024 * 1024

func (c *Controller) LoadPatch(fileID string)    { c.preview(fileID, false) }
func (c *Controller) LoadContents(fileID string) { c.preview(fileID, true) }

func (c *Controller) preview(fileID string, contents bool) {
	c.mu.Lock()
	if !c.aliveLocked() || c.state.Current == nil || c.state.Diff == nil {
		c.mu.Unlock()
		return
	}
	if contents && (c.state.Session == nil || c.state.Session.Capabilities.Files.Contents.State != "enabled") {
		c.state.FileErrors[fileID] = "complete file contents unavailable"
		c.mu.Unlock()
		c.emit()
		return
	}
	if contents {
		if _, ok := c.state.Contents[fileID]; ok {
			c.touchLocked(fileID)
			c.mu.Unlock()
			return
		}
		if c.state.ContentLoading[fileID] {
			c.mu.Unlock()
			return
		}
	} else {
		if _, ok := c.state.Patches[fileID]; ok {
			c.touchLocked(fileID)
			c.mu.Unlock()
			return
		}
		if c.state.PatchLoading[fileID] {
			c.mu.Unlock()
			return
		}
	}
	index := slices.IndexFunc(c.state.Diff.Files, func(file goclient.ChangedFile) bool { return file.Id == fileID })
	if index < 0 {
		c.mu.Unlock()
		return
	}
	file := c.state.Diff.Files[index]
	id, diffID, version, scope, commit, ctx, generation := c.state.Current.Id, c.state.Diff.Id, c.state.Diff.VersionId, c.state.Scope, c.state.CommitID, c.selected, c.generation
	if contents {
		c.state.ContentLoading[fileID] = true
	} else {
		c.state.PatchLoading[fileID] = true
	}
	delete(c.state.FileErrors, fileID)
	c.taskLocked(func() {
		select {
		case c.previews <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-c.previews }()
		if ctx.Err() != nil {
			return
		}
		var patch *goclient.FilePatch
		var full *goclient.FileContents
		var err error
		if contents {
			if commit != "" {
				r, e := c.client.GetCommitFileContentsV2WithResponse(ctx, id, commit, fileID, &goclient.GetCommitFileContentsV2Params{FileVersion: file.Fingerprint})
				err = e
				if err == nil {
					err = responseError(r, 200)
					full = r.JSON200
				}
			} else {
				r, e := c.client.GetFileContentsV2WithResponse(ctx, id, diffID, fileID, &goclient.GetFileContentsV2Params{VersionId: version, Scope: scope, FileVersion: file.Fingerprint})
				err = e
				if err == nil {
					err = responseError(r, 200)
					full = r.JSON200
				}
			}
			if err == nil && full == nil {
				err = errors.New("complete contents response missing")
			}
		} else {
			if commit != "" {
				r, e := c.client.GetCommitFilePatchV2WithResponse(ctx, id, commit, fileID, &goclient.GetCommitFilePatchV2Params{FileVersion: file.Fingerprint})
				err = e
				if err == nil {
					err = responseError(r, 200)
					patch = r.JSON200
				}
			} else {
				r, e := c.client.GetFilePatchV2WithResponse(ctx, id, diffID, fileID, &goclient.GetFilePatchV2Params{VersionId: version, Scope: scope, FileVersion: file.Fingerprint})
				err = e
				if err == nil {
					err = responseError(r, 200)
					patch = r.JSON200
				}
			}
			if err == nil && patch == nil {
				err = errors.New("patch response missing")
			}
		}
		c.mu.Lock()
		if !c.ownsLocked(generation) {
			c.mu.Unlock()
			return
		}
		if contents {
			delete(c.state.ContentLoading, fileID)
		} else {
			delete(c.state.PatchLoading, fileID)
		}
		if err == nil {
			if patch != nil {
				if patchBytes(*patch) > maxPreviewBytes {
					err = errors.New("patch exceeds native preview cache limit")
				} else {
					c.state.Patches[fileID] = *patch
				}
			}
			if full != nil {
				if len(full.Before)+len(full.After) > maxPreviewBytes {
					err = errors.New("contents exceed native preview cache limit")
				} else {
					c.state.Contents[fileID] = *full
				}
			}
		}
		if err != nil {
			c.state.FileErrors[fileID] = err.Error()
		} else {
			c.touchLocked(fileID)
			c.trimCacheLocked()
		}
		c.mu.Unlock()
		c.emit()
	})
	c.mu.Unlock()
	c.emit()
}

func patchBytes(p goclient.FilePatch) int {
	size := len(p.Patch)
	if p.Contents != nil {
		size += len(p.Contents.Before) + len(p.Contents.After)
	}
	return size
}
func (c *Controller) touchLocked(id string) {
	c.cacheOrder = slices.DeleteFunc(c.cacheOrder, func(key string) bool { return key == id })
	c.cacheOrder = append(c.cacheOrder, id)
}
func (c *Controller) trimCacheLocked() {
	size := 0
	for _, patch := range c.state.Patches {
		size += patchBytes(patch)
	}
	for _, contents := range c.state.Contents {
		size += len(contents.Before) + len(contents.After)
	}
	for len(c.cacheOrder) > maxCachedFiles || size > maxPreviewBytes {
		id := c.cacheOrder[0]
		c.cacheOrder = c.cacheOrder[1:]
		if patch, ok := c.state.Patches[id]; ok {
			size -= patchBytes(patch)
			delete(c.state.Patches, id)
		}
		if contents, ok := c.state.Contents[id]; ok {
			size -= len(contents.Before) + len(contents.After)
			delete(c.state.Contents, id)
		}
	}
}
