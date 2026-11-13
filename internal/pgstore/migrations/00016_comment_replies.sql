-- +goose Up
-- Comment threads. Additive only: the previous release ignores the table, and
-- its comment and review deletions cascade to replies, so upgrades stay rolling.
-- Authorship and resolution live in the comment's data.

-- One level of replies per comment, ending with it.
CREATE TABLE comment_replies (
  comment_id text NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
  id text NOT NULL,
  data bytea NOT NULL,
  -- The reply's own createdAt milliseconds; seq orders replies created within one.
  created_at bigint NOT NULL,
  seq bigint GENERATED ALWAYS AS IDENTITY,
  PRIMARY KEY(comment_id, id)
);
CREATE INDEX comment_replies_comment_created ON comment_replies(comment_id, created_at, seq);
