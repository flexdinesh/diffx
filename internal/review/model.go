package review

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode"
)

type DiffMode string

const (
	DiffAll         DiffMode = "all"
	DiffStaged      DiffMode = "staged"
	DiffUnstaged    DiffMode = "unstaged"
	DiffUncommitted DiffMode = "uncommitted"
	DiffCommitted   DiffMode = "committed"
)

func ParseDiffMode(value string) (DiffMode, error) {
	mode := DiffMode(value)
	switch mode {
	case DiffAll, DiffStaged, DiffUnstaged, DiffUncommitted, DiffCommitted:
		return mode, nil
	default:
		return "", errors.New("invalid diff scope")
	}
}

// ComparisonKind describes captured endpoints independently of deployment or scope aliases.
type ComparisonKind string

const (
	ComparisonCombined    ComparisonKind = "combined"
	ComparisonUncommitted ComparisonKind = "uncommitted"
	ComparisonStaged      ComparisonKind = "staged"
	ComparisonUnstaged    ComparisonKind = "unstaged"
	ComparisonCommitted   ComparisonKind = "committed"
	ComparisonPatch       ComparisonKind = "patch"
)

type ScopeSummary struct {
	Scope          DiffMode       `json:"scope"`
	ComparisonKind ComparisonKind `json:"comparisonKind"`
	FileCount      int            `json:"fileCount"`
}

type ChangedFile struct {
	ID             string  `json:"id"`
	Path           string  `json:"path"`
	OldPath        *string `json:"oldPath"`
	Status         string  `json:"status"`
	IndexStatus    string  `json:"indexStatus"`
	WorktreeStatus string  `json:"worktreeStatus"`
	Additions      int     `json:"additions"`
	Deletions      int     `json:"deletions"`
	Binary         bool    `json:"binary"`
	Fingerprint    string  `json:"fingerprint"`
	Recreated      bool    `json:"recreated,omitempty"`
}

type RepositoryDiff struct {
	ID             string         `json:"id"`
	VersionID      string         `json:"versionId"`
	LocationID     *string        `json:"locationId"`
	RepositoryID   *string        `json:"repositoryId"`
	Source         string         `json:"source"`
	Root           string         `json:"root"`
	Name           string         `json:"name"`
	Branch         string         `json:"branch"`
	Head           *string        `json:"head"`
	Mode           DiffMode       `json:"mode"`
	ComparisonKind ComparisonKind `json:"comparisonKind,omitempty"`
	Files          []ChangedFile  `json:"files"`
	Revision       string         `json:"revision"`
}

type FileContents struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

type FilePatch struct {
	Patch    string        `json:"patch"`
	Message  *string       `json:"message"`
	Contents *FileContents `json:"contents,omitempty"`
}

type ReviewFileOrigin struct {
	Status  string  `json:"status"`
	OldPath *string `json:"oldPath"`
}

type ReviewOrigin struct {
	DiffID     string           `json:"diffId"`
	VersionID  string           `json:"versionId"`
	Source     string           `json:"source"`
	Repository string           `json:"repository"`
	Branch     string           `json:"branch"`
	Head       *string          `json:"head"`
	Revision   string           `json:"revision"`
	File       ReviewFileOrigin `json:"file"`
}

type ReviewComment struct {
	ID          string        `json:"id"`
	DiffID      string        `json:"diffId"`
	VersionID   string        `json:"versionId"`
	Path        string        `json:"path"`
	Scope       DiffMode      `json:"scope"`
	Fingerprint string        `json:"fingerprint"`
	Target      string        `json:"target,omitempty"`
	Side        string        `json:"side"`
	Start       int           `json:"start"`
	End         int           `json:"end"`
	Code        string        `json:"code"`
	Body        string        `json:"body"`
	Status      string        `json:"status"`
	CreatedAt   float64       `json:"createdAt"`
	Origin      *ReviewOrigin `json:"origin,omitempty"`
	// Anchor holds same-side content lines around the selection, captured at creation.
	Anchor *CommentAnchor `json:"anchor,omitempty"`
	// CarriedFrom records the original selection of a comment kept open at file level.
	CarriedFrom *CommentCarriedFrom `json:"carriedFrom,omitempty"`
	// Author wrote the comment; absent in legacy comments, which read as the user.
	Author *CommentAuthor `json:"author,omitempty"`
	// ResolvedBy and ResolvedAt record the latest resolution; reopening clears them.
	ResolvedBy *CommentAuthor `json:"resolvedBy,omitempty"`
	ResolvedAt float64        `json:"resolvedAt,omitempty"`
	// Replies form one level of discussion in creation order. Stores keep them
	// apart from the comment so adding one never rewrites the comment.
	Replies []CommentReply `json:"replies,omitempty"`
	// Placement is computed for the viewed snapshot and never persisted.
	Placement *CommentPlacement `json:"placement,omitempty"`
}

// Comment and reply author kinds. Data is single-owner per stream, so the user
// needs no account identity; agents report their own name.
const (
	AuthorUser  = "user"
	AuthorAgent = "agent"
)

// MaxAuthorName bounds an agent's self-reported name.
const MaxAuthorName = 100

// CommentAuthor names who wrote a comment, reply or resolution. Name and
// ClientID are reported by agents; Verified is set only when the server
// established the agent's identity.
type CommentAuthor struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	Verified bool   `json:"verified,omitempty"`
	ClientID string `json:"clientId,omitempty"`
}

// UserAuthor is the stream owner writing through the review UI or REST.
func UserAuthor() CommentAuthor { return CommentAuthor{Kind: AuthorUser} }

// AgentAuthor is an agent with a self-reported, unverified name.
func AgentAuthor(name, clientID string) CommentAuthor {
	return CommentAuthor{Kind: AuthorAgent, Name: cleanLabel(name), ClientID: cleanLabel(clientID)}
}

// cleanLabel drops control characters and bounds a reported label.
func cleanLabel(value string) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
	if runes := []rune(value); len(runes) > MaxAuthorName {
		value = string(runes[:MaxAuthorName])
	}
	return value
}

func (author CommentAuthor) Valid() bool {
	return (author.Kind == AuthorUser || author.Kind == AuthorAgent) &&
		len([]rune(author.Name)) <= MaxAuthorName && len([]rune(author.ClientID)) <= MaxAuthorName &&
		cleanLabel(author.Name) == author.Name && cleanLabel(author.ClientID) == author.ClientID &&
		(author.Kind == AuthorAgent || author.Name == "" && author.ClientID == "" && !author.Verified)
}

// CommentReply is one reply in a comment's thread.
type CommentReply struct {
	ID        string        `json:"id"`
	Author    CommentAuthor `json:"author"`
	Body      string        `json:"body"`
	CreatedAt float64       `json:"createdAt"`
	EditedAt  float64       `json:"editedAt,omitempty"`
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func (reply CommentReply) Valid() bool {
	return reply.ID != "" && reply.Author.Valid() && strings.TrimSpace(reply.Body) != "" &&
		finite(reply.CreatedAt) && finite(reply.EditedAt)
}

// StoredComment decodes a stored comment and attaches its replies. Legacy
// comments without an author read as written by the user.
func StoredComment(raw []byte, replies []CommentReply) (ReviewComment, error) {
	var comment ReviewComment
	if err := json.Unmarshal(raw, &comment); err != nil {
		return ReviewComment{}, err
	}
	if comment.Author == nil {
		author := UserAuthor()
		comment.Author = &author
	}
	comment.Replies = replies
	return comment, nil
}

// StorableComment is the comment's stored representation: placement is
// computed per snapshot and replies are stored separately.
func StorableComment(comment ReviewComment) ([]byte, error) {
	comment.Placement, comment.Replies = nil, nil
	return json.Marshal(comment)
}

type CommentAnchor struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

type CommentCarriedFrom struct {
	VersionID string `json:"versionId"`
	Side      string `json:"side"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
}

func validLines(side string, start, end int) bool {
	return (side == "additions" || side == "deletions") && start > 0 && end >= start && end-start < 200
}

func (comment ReviewComment) Valid() bool {
	_, modeError := ParseDiffMode(string(comment.Scope))
	validSelection := (comment.Target == "" || comment.Target == "lines") && validLines(comment.Side, comment.Start, comment.End)
	if comment.Target == "file" {
		// Carried comments keep their original code; other file comments have none.
		validSelection = comment.Side == "additions" && comment.Start == 0 && comment.End == 0 && (comment.Code == "" || comment.CarriedFrom != nil)
	}
	if carried := comment.CarriedFrom; carried != nil && (comment.Target != "file" || !validLines(carried.Side, carried.Start, carried.End)) {
		validSelection = false
	}
	return comment.Scope != "" && modeError == nil &&
		validSelection &&
		strings.TrimSpace(comment.Body) != "" && (comment.Status == "open" || comment.Status == "resolved") &&
		finite(comment.CreatedAt) &&
		(comment.Origin == nil || comment.Origin.Source == "local" || comment.Origin.Source == "stdin") &&
		(comment.Author == nil || comment.Author.Valid()) &&
		(comment.ResolvedBy == nil || comment.Status == "resolved" && comment.ResolvedBy.Valid()) &&
		finite(comment.ResolvedAt) && validReplies(comment.Replies)
}

func validReplies(replies []CommentReply) bool {
	seen := make(map[string]bool, len(replies))
	for _, reply := range replies {
		if !reply.Valid() || seen[reply.ID] {
			return false
		}
		seen[reply.ID] = true
	}
	return true
}

type ReviewMark struct {
	DiffID      string   `json:"diffId"`
	VersionID   string   `json:"versionId"`
	FileID      string   `json:"fileId"`
	FileVersion string   `json:"fileVersion"`
	Scope       DiffMode `json:"scope"`
}
