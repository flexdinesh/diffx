-- +goose Up
-- Authentication attempts per client in fixed windows shared by every replica.
-- Counters are disposable: UNLOGGED skips WAL, and a crash merely resets them.
CREATE UNLOGGED TABLE auth_attempts (
  client text PRIMARY KEY,
  window_start timestamptz NOT NULL,
  attempts integer NOT NULL
);
CREATE INDEX auth_attempts_window ON auth_attempts(window_start);
