#!/bin/sh
# Runs once, when the Postgres volume is created. The app role logs in with its own password and is subject
# to RLS; schema objects and grants come from `atlas migrate`, which runs as the owner.
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -v pw="$ATLAS_APP_PASSWORD" <<'SQL'
CREATE ROLE atlas_app LOGIN NOBYPASSRLS PASSWORD :'pw';
SQL
