-- A second-factor code works once: the last time step accepted is kept, and a code of that step or an older one is refused.

-- +goose Up
ALTER TABLE access.users ADD COLUMN totp_last_step bigint;

-- +goose Down
ALTER TABLE access.users DROP COLUMN totp_last_step;
