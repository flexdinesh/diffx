-- +goose Up
-- Delivery claims: a replica leases a pending notification, and its token
-- fences the worker's transitions, so replicas never deliver one concurrently.
-- An expired lease is claimable again.
ALTER TABLE push_notifications
  ADD COLUMN lease_token text NOT NULL DEFAULT '',
  ADD COLUMN lease_until timestamptz NOT NULL DEFAULT '-infinity';
