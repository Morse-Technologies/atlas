-- A person whose password was set by someone else (a new account, or a reset) must choose their own at
-- the first sign-in. Until they do, the API lets them do nothing else.

-- +goose Up
ALTER TABLE access.users ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE access.users DROP COLUMN must_change_password;
