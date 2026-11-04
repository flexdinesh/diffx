package workspace

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"mime"
	"slices"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/packages/api/goclient"
)

// Reload reads durable heads. Events coalesce while a reload is running.
func (c *Controller) Reload() {
	c.mu.Lock()
	if !c.aliveLocked() {
		c.mu.Unlock()
		return
	}
	if c.catalogActive {
		c.catalogAgain = true
		c.mu.Unlock()
		return
	}
	c.catalogActive = true
	c.taskLocked(c.reload)
	c.mu.Unlock()
}

func (c *Controller) reload() {
	for {
		heads, err := c.heads(c.ctx)
		c.mu.Lock()
		if !c.aliveLocked() {
			c.catalogActive = false
			c.mu.Unlock()
			return
		}
		if err != nil {
			c.state.CatalogError = err.Error()
		} else {
			c.state.CatalogError = ""
			c.state.Contexts = heads
			if c.state.Seen == nil {
				c.state.Seen = map[string]string{}
				for _, head := range heads {
					c.state.Seen[StreamKey(head)] = head.Id
				}
			}
			c.updateNewerLocked()
		}
		openID := ""
		if err == nil && c.selectedID == "" && c.state.Bootstrap != nil && (!c.state.Bootstrap.Collect || c.collectionCompleted) && len(heads) > 0 {
			openID = defaultHead(heads)
		}
		again := c.catalogAgain
		c.catalogAgain = false
		if !again {
			c.catalogActive = false
		}
		c.mu.Unlock()
		c.emit()
		if openID != "" {
			c.openIfEmpty(openID)
		}
		if !again {
			return
		}
	}
}

func (c *Controller) heads(ctx context.Context) ([]goclient.Context, error) {
	var all []goclient.Context
	latest, limit, cursor := true, 200, ""
	seen := map[string]bool{}
	for {
		r, err := c.client.ListContextsWithResponse(ctx, &goclient.ListContextsParams{Latest: &latest, Limit: &limit, Cursor: &cursor})
		if err != nil {
			return nil, err
		}
		if err = responseError(r, 200); err != nil {
			return nil, err
		}
		if r.JSON200 == nil {
			return nil, errors.New("catalog response missing")
		}
		all = append(all, r.JSON200.Contexts...)
		cursor = value(r.JSON200.NextCursor)
		if cursor == "" {
			break
		}
		if seen[cursor] {
			return nil, errors.New("catalog cursor repeated")
		}
		seen[cursor] = true
	}
	slices.SortFunc(all, CompareActivity)
	return all, nil
}

func defaultHead(heads []goclient.Context) string {
	for _, head := range heads {
		if CurrentWork(head) {
			return head.Id
		}
	}
	for _, head := range heads {
		if head.Availability == "available" {
			return head.Id
		}
	}
	return ""
}

func (c *Controller) updateNewerLocked() {
	c.state.NewerContextID = ""
	if current := c.state.Current; current != nil {
		for _, head := range c.state.Contexts {
			if head.Id == current.Id {
				if c.state.Seen != nil {
					c.state.Seen[StreamKey(head)] = head.Id
				}
				continue
			}
			if !IsPiped(*current) && value(current.StreamId) != "" && value(head.StreamId) == value(current.StreamId) {
				c.state.NewerContextID = head.Id
			}
		}
	}
}

func (c *Controller) OpenLatest(streamID string) {
	c.mu.Lock()
	id := ""
	for _, head := range c.state.Contexts {
		if StreamKey(head) == streamID && head.Availability == "available" {
			id = head.Id
			break
		}
	}
	c.mu.Unlock()
	if id != "" {
		c.OpenContext(id)
	}
}

func (c *Controller) Sync() { c.collect(false) }
func (c *Controller) collect(initial bool) {
	c.mu.Lock()
	if !c.aliveLocked() || c.state.Bootstrap == nil || !c.state.Bootstrap.Collect || c.state.Collecting {
		c.mu.Unlock()
		return
	}
	if initial && c.initialJoined {
		c.mu.Unlock()
		return
	}
	if initial {
		c.initialJoined = true
	}
	c.state.Collecting = true
	c.state.CollectionError = ""
	c.collectionFailed = false
	known := map[string]bool{}
	for _, head := range c.state.Contexts {
		known[head.Id] = true
	}
	c.taskLocked(func() {
		params := &goclient.CollectCheckoutsParams{}
		if initial {
			params.Initial = &initial
		}
		r, err := c.client.CollectCheckoutsWithResponse(c.ctx, params)
		if err == nil {
			err = responseError(r, 200)
		}
		if err == nil && r.JSON200 == nil {
			err = errors.New("collection response missing")
		}
		c.mu.Lock()
		if !c.aliveLocked() {
			c.mu.Unlock()
			return
		}
		c.state.Collecting = false
		c.collectionCompleted = true
		id := ""
		if err != nil {
			c.collectionFailed = true
			c.state.CollectionError = err.Error()
		} else {
			c.state.Collection = r.JSON200
			updated := 0
			for _, head := range r.JSON200.Contexts {
				if !known[head.Id] {
					updated++
				}
			}
			c.state.Notice = fmt.Sprintf("Synced %d checkouts; %d updated; %d failed", len(r.JSON200.Contexts)+len(r.JSON200.Failures), updated, len(r.JSON200.Failures))
			if len(r.JSON200.Failures) > 0 {
				c.collectionFailed = true
				failures := []string{}
				for _, failure := range r.JSON200.Failures {
					failures = append(failures, failure.Path+": "+failure.Detail)
				}
				c.state.CollectionError = strings.Join(failures, "\n")
			}
			if c.selectedID == "" {
				id = r.JSON200.DefaultContextId
				if id == "" {
					sorted := slices.Clone(r.JSON200.Contexts)
					slices.SortFunc(sorted, CompareActivity)
					id = defaultHead(sorted)
				}
			}
		}
		// Failed collection may still have previously stored usable heads.
		if id == "" && c.selectedID == "" {
			id = defaultHead(c.state.Contexts)
		}
		c.mu.Unlock()
		c.emit()
		c.Reload()
		if id != "" {
			c.openIfEmpty(id)
		}
	})
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) Search(query string) {
	c.mu.Lock()
	if !c.aliveLocked() {
		c.mu.Unlock()
		return
	}
	if c.searchCancel != nil {
		c.searchCancel()
	}
	c.searchGeneration++
	generation := c.searchGeneration
	ctx, cancel := context.WithCancel(c.ctx)
	c.searchCancel = cancel
	c.state.Query = query
	c.state.History = nil
	c.state.NextCursor = ""
	c.state.SearchError = ""
	c.state.Searching = true
	c.taskLocked(func() { c.searchPage(ctx, generation, "") })
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) SearchNext() {
	c.mu.Lock()
	if !c.aliveLocked() || c.state.Searching || c.state.NextCursor == "" {
		c.mu.Unlock()
		return
	}
	c.state.Searching = true
	cursor, generation := c.state.NextCursor, c.searchGeneration
	ctx, cancel := context.WithCancel(c.ctx)
	c.searchCancel = cancel
	c.taskLocked(func() { c.searchPage(ctx, generation, cursor) })
	c.mu.Unlock()
	c.emit()
}

// History is the stored page, not prefiltered, so native multi-select filters
// remain independent of server catalog rules. PickerResults applies the query.
func (c *Controller) searchPage(ctx context.Context, generation uint64, cursor string) {
	limit := 200
	r, err := c.client.ListContextsWithResponse(ctx, &goclient.ListContextsParams{Limit: &limit, Cursor: &cursor})
	if err == nil {
		err = responseError(r, 200)
	}
	if err == nil && r.JSON200 == nil {
		err = errors.New("history response missing")
	}
	c.mu.Lock()
	if !c.aliveLocked() || generation != c.searchGeneration {
		c.mu.Unlock()
		return
	}
	c.state.Searching = false
	if err != nil {
		c.state.SearchError = err.Error()
	} else {
		c.state.SearchError = ""
		c.state.History = append(c.state.History, r.JSON200.Contexts...)
		c.state.NextCursor = value(r.JSON200.NextCursor)
		if cursor != "" && cursor == c.state.NextCursor {
			c.state.SearchError = "history cursor repeated"
			c.state.NextCursor = ""
		}
	}
	c.mu.Unlock()
	c.emit()
}

func (c *Controller) events() {
	backoff := 250 * time.Millisecond
	for c.ctx.Err() == nil {
		r, err := c.client.WatchIngestions(c.ctx)
		if err == nil {
			media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if r.StatusCode == 200 && media == "text/event-stream" {
				c.change(func(s *State) { s.Connected = true })
				c.Reload()
				c.reloadReview()
				scanner := bufio.NewScanner(r.Body)
				scanner.Buffer(make([]byte, 4096), 256*1024)
				hasData := false
				for scanner.Scan() {
					line := scanner.Text()
					if strings.HasPrefix(line, "data:") {
						hasData = true
					}
					if line == "" && hasData {
						hasData = false
						backoff = 250 * time.Millisecond
						c.Reload()
						c.reloadReview()
					}
				}
			}
			r.Body.Close()
		}
		c.change(func(s *State) { s.Connected = false })
		timer := time.NewTimer(backoff)
		select {
		case <-c.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 15*time.Second)
	}
}

func (c *Controller) LoadMetrics() {
	c.mu.Lock()
	if !c.aliveLocked() || c.state.MetricsLoading {
		c.mu.Unlock()
		return
	}
	c.state.MetricsLoading = true
	c.taskLocked(func() {
		r, err := c.client.GetServerMetricsV2WithResponse(c.ctx)
		if err == nil {
			err = responseError(r, 200)
		}
		if err == nil && r.JSON200 == nil {
			err = errors.New("metrics response missing")
		}
		c.change(func(s *State) {
			s.MetricsLoading = false
			s.MetricsError = ""
			if err != nil {
				s.MetricsError = err.Error()
			} else {
				s.Metrics = r.JSON200
			}
		})
	})
	c.mu.Unlock()
	c.emit()
}
