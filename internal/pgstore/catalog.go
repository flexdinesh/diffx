package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/pgstore/pgdb"
	"github.com/flexdinesh/diffx/internal/review"
	"github.com/flexdinesh/diffx/internal/reviewdata"
	"github.com/jackc/pgx/v5"
)

var ErrExpired = reviewdata.ErrExpired

// The catalog is hand-written pgx rather than sqlc: filters, text search and
// checkout selection add predicates per request, and one statement still
// reads every listed context with its scopes, sessions, head and commits.

// contextSelect reads contexts with their stream head and winning submission.
// It uses @now for the head's expiry.
const contextSelect = `SELECT c.id, c.kind, o.metadata->>'root', o.repository_id, o.metadata->>'checkoutKey', c.submitted_from,
	c.created_at, c.last_submitted_at, d.expires_at,
	CASE WHEN head.context_id = c.id THEN COALESCE(NULLIF((head_submission.metadata->>'activityAt')::bigint, 0), (o.metadata->>'collectedAt')::bigint, (extract(epoch FROM c.created_at) * 1000)::bigint)
	ELSE COALESCE(NULLIF((o.metadata->>'activityAt')::bigint, 0), (o.metadata->>'collectedAt')::bigint, (extract(epoch FROM c.created_at) * 1000)::bigint) END,
	(SELECT count(DISTINCT f.path) FROM observation_scopes s JOIN diff_files f ON f.version_id = s.version_id WHERE s.context_id = c.id),
	(SELECT COALESCE(jsonb_agg(jsonb_build_object('scope', s.mode, 'comparisonKind', v.comparison_kind,
		'fileCount', (SELECT count(*) FROM diff_files f WHERE f.version_id = s.version_id))
		ORDER BY CASE s.mode WHEN 'all' THEN 0 WHEN 'staged' THEN 1 WHEN 'unstaged' THEN 2 WHEN 'uncommitted' THEN 3 ELSE 4 END), '[]')
	 FROM observation_scopes s JOIN diff_versions v ON v.id = s.version_id WHERE s.context_id = c.id),
	(SELECT v.source FROM diff_versions v WHERE v.diff_id = c.anchor_diff_id LIMIT 1), o.metadata,
	COALESCE(head.context_id <> c.id, false),
	(SELECT COALESCE(jsonb_agg(jsonb_build_object('sourceId', s.source_id, 'harness', s.harness, 'id', s.session_id, 'name', s.name,
		'firstObservedAt', a.first_observed_at, 'lastObservedAt', a.last_observed_at)
		ORDER BY a.source_id COLLATE "C", a.harness COLLATE "C", a.session_id COLLATE "C"), '[]')
	 FROM observation_sessions a JOIN agent_sessions s ON s.owner_id = a.owner_id AND s.source_id = a.source_id AND s.harness = a.harness AND s.session_id = a.session_id
	 WHERE a.context_id = c.id),
	head.source_id, head.repository_key, head.checkout_key, head.branch, head.comparison_policy,
	(SELECT hc.id FROM contexts hc JOIN diffs hd ON hd.id = hc.anchor_diff_id WHERE hc.owner_id = c.owner_id AND hc.id = head.context_id
	 AND (hd.expires_at IS NULL OR hd.expires_at > @now)),
	CASE WHEN head.context_id = c.id THEN head_submission.metadata->'pullRequest' END,
	(SELECT count(*) FROM observation_commits k WHERE k.context_id = c.id),
	(SELECT r.total FROM observation_commit_ranges r WHERE r.context_id = c.id)
	FROM contexts c
	LEFT JOIN diffs d ON d.id = c.anchor_diff_id LEFT JOIN observations o ON o.context_id = c.id
	LEFT JOIN observation_stream_heads head ON head.owner_id = o.owner_id AND head.source_id = o.source_id
		AND head.repository_key = o.metadata->>'repositoryKey' AND head.checkout_key = o.metadata->>'checkoutKey'
		AND head.comparison_policy = o.comparison_policy
		AND head.branch = COALESCE(NULLIF(o.metadata->>'branchId', ''), o.metadata->>'branch')
	LEFT JOIN ingestion_records head_record ON head_record.sequence = head.arrival_sequence AND head_record.owner_id = head.owner_id
	LEFT JOIN observation_submissions head_submission ON head_submission.owner_id = head_record.owner_id AND head_submission.source_id = head_record.source_id
		AND head_submission.submission_id = head_record.submission_id AND head_submission.context_id = head.context_id`

func scanContext(row pgx.Row) (reviewdata.ContextInfo, error) {
	var item reviewdata.ContextInfo
	var createdAt, lastSubmittedAt time.Time
	var expiresAt *time.Time
	var changed int64
	var source *string
	var metadata, summaries, sessions, headPull []byte
	var head [5]*string
	var commits int64
	var commitsTotal *int64
	err := row.Scan(&item.ID, &item.Kind, &item.Root, &item.RepositoryID, &item.WorktreeKey, &item.SubmittedFrom,
		&createdAt, &lastSubmittedAt, &expiresAt, &item.ActivityAt, &changed, &summaries, &source, &metadata, &item.Stale, &sessions,
		&head[0], &head[1], &head[2], &head[3], &head[4], &item.LatestContextID, &headPull, &commits, &commitsTotal)
	if err != nil {
		return reviewdata.ContextInfo{}, noRows(err)
	}
	item.CreatedAt, item.LastSubmittedAt = createdAt.UnixMilli(), lastSubmittedAt.UnixMilli()
	if expiresAt != nil {
		expires := expiresAt.UnixMilli()
		item.ExpiresAt = &expires
	}
	count := int(changed)
	item.ChangedFileCount = &count
	if source != nil {
		item.Source = *source
	}
	if commits > 0 {
		total := int64(0)
		if commitsTotal != nil {
			total = *commitsTotal
		}
		item.Commits = &review.CommitRange{Count: int(commits), Omitted: max(int(total)-int(commits), 0)}
	}
	if head[0] != nil && head[1] != nil && head[2] != nil && head[3] != nil && head[4] != nil {
		id := ingestion.StreamID(*head[0], *head[1], *head[2], *head[3], *head[4])
		item.StreamID = &id
	}
	if err := json.Unmarshal(sessions, &item.Sessions); err != nil {
		return reviewdata.ContextInfo{}, err
	}
	if metadata != nil {
		var decoded ingestion.Metadata
		if err := json.Unmarshal(metadata, &decoded); err != nil {
			return reviewdata.ContextInfo{}, err
		}
		// A current head shows its winning submission's pull request, so edits
		// and state changes appear without changing captured metadata.
		if headPull != nil {
			var pull ingestion.PullRequest
			if err := json.Unmarshal(headPull, &pull); err != nil {
				return reviewdata.ContextInfo{}, err
			}
			decoded.PullRequest = &pull
		}
		item.Metadata = &decoded
		item.Kind = "observation"
	}
	if err := json.Unmarshal(summaries, &item.ScopeSummaries); err != nil {
		return reviewdata.ContextInfo{}, err
	}
	modes := make([]review.DiffMode, 0, len(item.ScopeSummaries))
	for _, summary := range item.ScopeSummaries {
		modes = append(modes, summary.Scope)
	}
	if item.Metadata != nil {
		for i := range item.ScopeSummaries {
			summary := &item.ScopeSummaries[i]
			if summary.ComparisonKind == "" {
				summary.ComparisonKind = ingestion.LegacyComparisonKind(summary.Scope, item.Source, *item.Metadata, modes)
			}
		}
	}
	return item, nil
}

// query accumulates predicates with named arguments.
type query struct {
	sql       strings.Builder
	arguments pgx.NamedArgs
}

func newQuery(base string, arguments pgx.NamedArgs) *query {
	built := &query{arguments: arguments}
	built.sql.WriteString(base)
	return built
}

// bind names a new argument and returns its placeholder.
func (built *query) bind(value any) string {
	name := fmt.Sprintf("a%d", len(built.arguments))
	built.arguments[name] = value
	return "@" + name
}

func (built *query) add(sql string) { built.sql.WriteString(sql) }

func (store *Store) contexts(ctx context.Context, built *query) ([]reviewdata.ContextInfo, error) {
	rows, err := store.pool.Query(ctx, built.sql.String(), built.arguments)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]reviewdata.ContextInfo, 0)
	for rows.Next() {
		item, err := scanContext(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// page orders by recency with a keyset cursor; concurrent submissions may move an item across pages.
func (built *query) page(limit int, beforeTime int64, beforeID string) {
	if beforeID != "" {
		before := built.bind(time.UnixMilli(beforeTime))
		built.add(` AND (c.last_submitted_at < ` + before + ` OR (c.last_submitted_at = ` + before + ` AND c.id < ` + built.bind(beforeID) + `))`)
	}
	built.add(` ORDER BY c.last_submitted_at DESC, c.id DESC LIMIT ` + built.bind(limit))
}

// ObservationContexts filters immutable observations using captured metadata.
func (store *Store) ObservationContexts(ctx context.Context, ownerID string, limit int, beforeTime int64, beforeID string, filter ingestion.Filter) ([]reviewdata.ContextInfo, error) {
	built := newQuery(contextSelect+` WHERE c.owner_id = @owner AND o.context_id IS NOT NULL AND d.expires_at > @now`, pgx.NamedArgs{"owner": ownerID, "now": time.Now()})
	// All predicates match one collection, rather than mixing facts from
	// unrelated submissions of the same immutable snapshot.
	built.add(` AND EXISTS (SELECT 1 FROM observation_submissions p
		LEFT JOIN agent_sessions session ON session.owner_id = p.owner_id AND session.source_id = p.source_id
		AND session.harness = COALESCE(p.metadata->'agentSession'->>'harness', p.metadata->>'agent')
		AND session.session_id = COALESCE(p.metadata->'agentSession'->>'id', p.metadata->>'runId')
		WHERE p.owner_id = c.owner_id AND p.context_id = c.id`)
	fields := []struct{ expression, value string }{
		{"p.metadata->>'repositoryName'", filter.Repository},
		{"p.metadata->>'branch'", filter.Branch},
		{"p.metadata->>'worktreeName'", filter.Worktree},
		{"p.metadata->>'hostname'", filter.Hostname},
		{"p.source_id", filter.SourceID},
		{"p.metadata->>'runId'", filter.RunID},
		{"session.harness", filter.Harness},
		{"session.session_id", filter.SessionID},
		{"session.name", filter.SessionName},
	}
	for _, field := range fields {
		if field.value != "" {
			built.add(` AND ` + field.expression + ` = ` + built.bind(field.value))
		}
	}
	if filter.Query != "" {
		// The C collation folds ASCII only, matching SQLite's lower() on every server locale.
		search := `lower(` + built.bind(filter.Query) + ` COLLATE "C")`
		terms := []string{}
		for _, field := range []string{"repositoryName", "remoteUrl", "branch", "worktreeName", "hostname", "sourceId", "runId", "root", "triggerRoot", "agent"} {
			terms = append(terms, `strpos(lower(COALESCE(p.metadata->>'`+field+`', '') COLLATE "C"), `+search+`) > 0`)
		}
		for _, field := range []string{"harness", "session_id", "name"} {
			terms = append(terms, `strpos(lower(COALESCE(session.`+field+`, '') COLLATE "C"), `+search+`) > 0`)
		}
		built.add(` AND (` + strings.Join(terms, " OR ") + `)`)
	}
	built.add(`)`)
	built.selection(filter.Selection)
	if filter.Latest {
		built.add(` AND (head.context_id IS NULL OR head.context_id = c.id)`)
	}
	built.page(limit, beforeTime, beforeID)
	return store.contexts(ctx, built)
}

// selection matches any selected checkout identity, or a capture by any
// submission, including fresh submissions that reused its snapshot. A scoped
// selection without checkouts or a submission matches nothing.
func (built *query) selection(selection ingestion.Selection) {
	if selection.Scoped {
		built.add(` AND o.source_id = ` + built.bind(selection.SourceID))
		switch {
		case selection.SubmissionID != "":
			built.add(` AND EXISTS (SELECT 1 FROM observation_submissions selected WHERE selected.owner_id = c.owner_id AND selected.context_id = c.id
			AND selected.source_id = o.source_id AND selected.submission_id = ` + built.bind(selection.SubmissionID) + `)`)
		case len(selection.Checkouts) == 0:
			built.add(` AND false`)
		default:
			repositories := make([]string, 0, len(selection.Checkouts))
			checkouts := make([]string, 0, len(selection.Checkouts))
			for _, checkout := range selection.Checkouts {
				repositories = append(repositories, checkout.RepositoryKey)
				checkouts = append(checkouts, checkout.CheckoutKey)
			}
			built.add(` AND (o.metadata->>'repositoryKey', o.metadata->>'checkoutKey') IN (SELECT * FROM unnest(` + built.bind(repositories) + `::text[], ` + built.bind(checkouts) + `::text[]))`)
		}
	}
	if selection.ContextID != "" {
		built.add(` AND c.id = ` + built.bind(selection.ContextID))
	}
}

func (store *Store) Context(ctx context.Context, ownerID, id string, now time.Time) (reviewdata.ContextInfo, error) {
	item, err := scanContext(store.pool.QueryRow(ctx, contextSelect+` WHERE c.owner_id = @owner AND c.id = @id`, pgx.NamedArgs{"owner": ownerID, "id": id, "now": time.Now()}))
	if err != nil {
		return reviewdata.ContextInfo{}, err
	}
	if item.ExpiresAt != nil && *item.ExpiresAt <= now.UnixMilli() {
		return reviewdata.ContextInfo{}, ErrExpired
	}
	return item, nil
}

// Contexts uses a keyset cursor; concurrent submissions may move an item across pages.
func (store *Store) Contexts(ctx context.Context, ownerID string, limit int, beforeTime int64, beforeID string, now time.Time) ([]reviewdata.ContextInfo, error) {
	built := newQuery(contextSelect+` WHERE c.owner_id = @owner AND (d.expires_at IS NULL OR d.expires_at > @expiry)`, pgx.NamedArgs{"owner": ownerID, "expiry": millis(now), "now": time.Now()})
	built.page(limit, beforeTime, beforeID)
	return store.contexts(ctx, built)
}

func (store *Store) ContextCount(ctx context.Context, ownerID string, selection ingestion.Selection, now time.Time) (int, error) {
	built := newQuery(`SELECT count(*) FROM contexts c LEFT JOIN diffs d ON d.id = c.anchor_diff_id LEFT JOIN observations o ON o.context_id = c.id
		WHERE c.owner_id = @owner AND (d.expires_at IS NULL OR d.expires_at > @expiry)`, pgx.NamedArgs{"owner": ownerID, "expiry": millis(now)})
	built.selection(selection)
	var count int
	err := store.pool.QueryRow(ctx, built.sql.String(), built.arguments).Scan(&count)
	return count, err
}

func (store *Store) ContextBinding(ctx context.Context, ownerID, id string, now time.Time) (reviewdata.Binding, error) {
	expiresAt, err := store.queries.ContextExpiry(ctx, pgdb.ContextExpiryParams{OwnerID: ownerID, ID: id})
	if err != nil {
		return reviewdata.Binding{}, noRows(err)
	}
	if expiresAt != nil && expiresAt.UnixMilli() <= now.UnixMilli() {
		return reviewdata.Binding{}, ErrExpired
	}
	return observationBinding(ctx, store.queries, ownerID, id, millis(time.Now()))
}

// DeleteContext removes the context and every scope's snapshot. Review state
// survives while its stream has another observation. Stream heads intentionally
// survive so older observations remain stale.
func (store *Store) DeleteContext(ctx context.Context, ownerID, id string) error {
	return store.inTx(ctx, func(_ pgx.Tx, queries *pgdb.Queries) error {
		row, err := queries.ContextForDelete(ctx, pgdb.ContextForDeleteParams{OwnerID: ownerID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if row.StreamKey != nil {
			if err := queries.LockStream(ctx, pgdb.LockStreamParams{OwnerID: ownerID, StreamKey: *row.StreamKey}); err != nil {
				return err
			}
			if err := queries.LockStreamReviews(ctx, pgdb.LockStreamReviewsParams{OwnerID: ownerID, StreamKey: *row.StreamKey}); err != nil {
				return err
			}
		}
		if err := queries.DeleteContextDiffs(ctx, pgdb.DeleteContextDiffsParams{OwnerID: ownerID, AnchorID: row.AnchorDiffID, ContextID: id}); err != nil {
			return err
		}
		if err := queries.DeleteDiff(ctx, pgdb.DeleteDiffParams{OwnerID: ownerID, ID: row.AnchorDiffID}); err != nil {
			return err
		}
		if row.StreamKey == nil {
			return nil
		}
		return queries.EndStream(ctx, pgdb.EndStreamParams{OwnerID: ownerID, StreamKey: *row.StreamKey})
	})
}
