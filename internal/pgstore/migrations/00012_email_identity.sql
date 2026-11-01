-- +goose Up
-- Email is every account's identity; usernames are gone. Google accounts take
-- the email Google reported, email-named password accounts keep theirs, and
-- the sole legacy handle account (the operator's admin) gets the operator's
-- email. A second legacy handle account fails the unique constraint instead
-- of sharing it.
UPDATE remote_accounts a SET email = g.email FROM google_identities g
WHERE g.user_id = a.user_id AND a.email IS NULL;
UPDATE remote_accounts SET email = username WHERE email IS NULL AND username LIKE '%@%';
UPDATE remote_accounts SET email = 'flexdinesh@gmail.com' WHERE email IS NULL;
ALTER TABLE remote_accounts ALTER COLUMN email SET NOT NULL;
ALTER TABLE remote_accounts ADD CONSTRAINT remote_accounts_email_key UNIQUE (email);
ALTER TABLE remote_accounts DROP COLUMN username;
UPDATE users u SET name = a.email FROM remote_accounts a WHERE a.user_id = u.id;

-- The previous release reads remote_accounts.username, so it must refuse this schema.
UPDATE schema_compatibility SET floor = 12;
