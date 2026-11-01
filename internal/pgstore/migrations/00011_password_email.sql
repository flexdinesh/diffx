-- +goose Up
-- New password accounts retain their email for future verification and recovery.
-- Legacy handle accounts have no known email; do not invent one during upgrade.
ALTER TABLE remote_accounts ADD COLUMN email text;
