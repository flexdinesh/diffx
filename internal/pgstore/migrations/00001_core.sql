-- +goose Up
-- Binaries supporting fewer migrations than floor refuse the database. Only a
-- migration that breaks the previous release raises it, to its own version.
CREATE TABLE schema_compatibility (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  floor bigint NOT NULL
);
INSERT INTO schema_compatibility(floor) VALUES (1);

-- Every owner is a server account; owned data cascades with it.
CREATE TABLE users (
  id text PRIMARY KEY,
  name text NOT NULL,
  created_at timestamptz NOT NULL
);
