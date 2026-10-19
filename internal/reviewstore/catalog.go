package reviewstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/flexdinesh/diffx/internal/ingestion"
	"github.com/flexdinesh/diffx/internal/reviewdata"
)

var (
	ErrExpired            = reviewdata.ErrExpired
	ErrSubmissionConflict = reviewdata.ErrSubmissionConflict
)

type ContextInfo = reviewdata.ContextInfo

func initializeCatalog(transaction *sql.Tx) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS contexts(id TEXT PRIMARY KEY,owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,kind TEXT NOT NULL CHECK(kind='observation'),anchor_diff_id TEXT NOT NULL UNIQUE REFERENCES diffs(id) ON DELETE CASCADE,created_at INTEGER NOT NULL,last_submitted_at INTEGER NOT NULL,submitted_from TEXT)`,
		`CREATE INDEX IF NOT EXISTS contexts_owner_order ON contexts(owner_id,last_submitted_at DESC,id DESC)`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func putObservationContext(transaction *sql.Tx, ownerID, id, submittedFrom string, now int64) error {
	var provenance *string
	if submittedFrom != "" {
		provenance = &submittedFrom
	}
	_, err := transaction.Exec(`INSERT INTO contexts(id,owner_id,kind,anchor_diff_id,created_at,last_submitted_at,submitted_from)
		VALUES(?,?,'observation',?,?,?,?)`, id, ownerID, id, now, now, provenance)
	return err
}

var contextSelect = `SELECT c.id,c.kind,json_extract(o.metadata,'$.root'),NULL,o.repository_id,NULL,json_extract(o.metadata,'$.checkoutKey'),c.submitted_from,
	c.created_at,c.last_submitted_at,d.expires_at,
	CASE WHEN head.context_id=c.id THEN COALESCE(NULLIF(json_extract(head_submission.metadata,'$.activityAt'),0),json_extract(o.metadata,'$.collectedAt'),c.created_at)
	ELSE COALESCE(NULLIF(json_extract(o.metadata,'$.activityAt'),0),json_extract(o.metadata,'$.collectedAt'),c.created_at) END,
	(SELECT json_array_length(v.manifest, '$.files') FROM diff_versions v WHERE v.diff_id=c.anchor_diff_id LIMIT 1),
	(SELECT json_extract(v.manifest, '$.source') FROM diff_versions v WHERE v.diff_id=c.anchor_diff_id LIMIT 1),o.metadata,
	COALESCE(head.context_id<>c.id,0),
	(SELECT json_group_array(json_object('sourceId',s.source_id,'harness',s.harness,'id',s.session_id,'name',s.name,'firstObservedAt',a.first_observed_at,'lastObservedAt',a.last_observed_at))
	 FROM observation_sessions a JOIN agent_sessions s ON s.owner_id=a.owner_id AND s.source_id=a.source_id AND s.harness=a.harness AND s.session_id=a.session_id WHERE a.context_id=c.id),
	head.source_id,head.repository_key,head.checkout_key,head.branch,head.comparison_policy,
	(SELECT hc.id FROM contexts hc JOIN diffs hd ON hd.id=hc.anchor_diff_id WHERE hc.owner_id=c.owner_id AND hc.id=head.context_id
	 AND (hd.expires_at IS NULL OR hd.expires_at>CAST(strftime('%s','now') AS INTEGER)*1000))
	FROM contexts c
	LEFT JOIN diffs d ON d.id=c.anchor_diff_id LEFT JOIN observations o ON o.context_id=c.id
	LEFT JOIN observation_stream_heads head ON head.owner_id=o.owner_id AND head.source_id=o.source_id
		AND head.repository_key=json_extract(o.metadata,'$.repositoryKey') AND head.checkout_key=json_extract(o.metadata,'$.checkoutKey')
		AND head.comparison_policy=` + comparisonPolicySQL("o.metadata") + `
		AND head.branch=COALESCE(NULLIF(json_extract(o.metadata,'$.branchId'),''),json_extract(o.metadata,'$.branch'))
	LEFT JOIN ingestion_records head_record ON head_record.sequence=head.arrival_sequence AND head_record.owner_id=head.owner_id
	LEFT JOIN observation_submissions head_submission ON head_submission.owner_id=head_record.owner_id AND head_submission.source_id=head_record.source_id
		AND head_submission.submission_id=head_record.submission_id AND head_submission.context_id=head.context_id`

type scanner interface{ Scan(...any) error }

// streamID names one stream head without exposing its keys or depending on row identity.
func streamID(sourceID, repositoryKey, checkoutKey, branch, comparisonPolicy string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{sourceID, repositoryKey, checkoutKey, branch, comparisonPolicy}, "\x00")))
	return hex.EncodeToString(digest[:12])
}

func scanContext(row scanner) (ContextInfo, error) {
	var item ContextInfo
	var metadata *string
	var sessions string
	var head [5]sql.NullString
	err := row.Scan(&item.ID, &item.Kind, &item.Root, &item.LocationID, &item.RepositoryID, &item.CommonDir, &item.WorktreeKey,
		&item.SubmittedFrom, &item.CreatedAt, &item.LastSubmittedAt, &item.ExpiresAt, &item.ActivityAt, &item.ChangedFileCount, &item.Source, &metadata, &item.Stale, &sessions,
		&head[0], &head[1], &head[2], &head[3], &head[4], &item.LatestContextID)
	if errors.Is(err, sql.ErrNoRows) {
		return ContextInfo{}, ErrNotFound
	}
	if err == nil && head[0].Valid {
		id := streamID(head[0].String, head[1].String, head[2].String, head[3].String, head[4].String)
		item.StreamID = &id
	}
	if err == nil {
		if err := json.Unmarshal([]byte(sessions), &item.Sessions); err != nil {
			return ContextInfo{}, err
		}
	}
	if err == nil && metadata != nil {
		var decoded ingestion.Metadata
		if err := json.Unmarshal([]byte(*metadata), &decoded); err != nil {
			return ContextInfo{}, err
		}
		item.Metadata = &decoded
		item.Kind = "observation"
	}
	return item, err
}

func (store *Store) Context(ownerID, id string, now time.Time) (ContextInfo, error) {
	item, err := scanContext(store.db.QueryRow(contextSelect+` WHERE c.owner_id=? AND c.id=?`, ownerID, id))
	if err != nil {
		return ContextInfo{}, err
	}
	if item.ExpiresAt != nil && *item.ExpiresAt <= now.UnixMilli() {
		return ContextInfo{}, ErrExpired
	}
	return item, nil
}

// DeleteContext removes the context and every scope's snapshot. Review state
// survives while its stream has another observation. Stream heads intentionally
// survive so older observations remain stale.
func (store *Store) DeleteContext(ownerID, id string) error {
	transaction, err := store.db.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var anchor string
	err = transaction.QueryRow("SELECT anchor_diff_id FROM contexts WHERE owner_id=? AND id=?", ownerID, id).Scan(&anchor)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = transaction.Exec("DELETE FROM diffs WHERE owner_id=? AND id<>? AND id IN (SELECT diff_id FROM observation_scopes WHERE context_id=?)", ownerID, anchor, id); err != nil {
		return err
	}
	if _, err = transaction.Exec("DELETE FROM diffs WHERE owner_id=? AND id=?", ownerID, anchor); err != nil {
		return err
	}
	if err := endStreams(transaction); err != nil {
		return err
	}
	return transaction.Commit()
}

// Contexts uses a keyset cursor; concurrent submissions may move an item across pages.
func (store *Store) Contexts(ownerID string, limit int, beforeTime int64, beforeID string, now time.Time) ([]ContextInfo, error) {
	query := contextSelect + ` WHERE c.owner_id=? AND (d.expires_at IS NULL OR d.expires_at>?)`
	arguments := []any{ownerID, now.UnixMilli()}
	if beforeID != "" {
		query += ` AND (c.last_submitted_at<? OR (c.last_submitted_at=? AND c.id<?))`
		arguments = append(arguments, beforeTime, beforeTime, beforeID)
	}
	query += ` ORDER BY c.last_submitted_at DESC,c.id DESC LIMIT ?`
	arguments = append(arguments, limit)
	rows, err := store.db.Query(query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ContextInfo, 0)
	for rows.Next() {
		item, err := scanContext(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ContextCount(ownerID string, selection ingestion.Selection, now time.Time) (int, error) {
	var count int
	predicate, values := selectionPredicate(selection)
	err := store.db.QueryRow(`SELECT COUNT(*)
		FROM contexts c LEFT JOIN diffs d ON d.id=c.anchor_diff_id LEFT JOIN observations o ON o.context_id=c.id
		WHERE c.owner_id=? AND (d.expires_at IS NULL OR d.expires_at>?)`+predicate,
		append([]any{ownerID, now.UnixMilli()}, values...)...).Scan(&count)
	return count, err
}

type queryer interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}

func bindingFor(db queryer, ownerID, id string, now time.Time) (Binding, error) {
	item, err := scanContext(db.QueryRow(contextSelect+` WHERE c.owner_id=? AND c.id=?`, ownerID, id))
	if err != nil {
		return Binding{}, err
	}
	if item.ExpiresAt != nil && *item.ExpiresAt <= now.UnixMilli() {
		return Binding{}, ErrExpired
	}
	return observationBinding(db, ownerID, id)
}

func (store *Store) ContextBinding(ownerID, id string, now time.Time) (Binding, error) {
	return bindingFor(store.db, ownerID, id, now)
}
