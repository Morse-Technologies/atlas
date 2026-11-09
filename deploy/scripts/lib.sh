# Shared by backup.sh, restore.sh and upgrade.sh. Source it; do not run it.
set -euo pipefail

COMPOSE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../compose" && pwd)"
cd "$COMPOSE_DIR"

# ATLAS_ENV_FILE picks another env file (dev-env.sh writes .env.dev); without it Compose reads .env as usual.
# The project name decides the volume names, so it is read from there too.
ENV_ARGS=()
if [ -n "${ATLAS_ENV_FILE:-}" ]; then
  ENV_ARGS=(--env-file "$ATLAS_ENV_FILE")
  set -a; source "$ATLAS_ENV_FILE"; set +a
fi
PROJECT="${COMPOSE_PROJECT_NAME:-atlas}"
BLOBS_VOLUME="${ATLAS_BLOBS_VOLUME:-${PROJECT}_blobs}"
OWNER=atlas_owner
DB=atlas

compose() { docker compose "${ENV_ARGS[@]}" "$@"; }

# SHA-256 of files with the same output and `-c` on Linux (GNU sha256sum) and macOS (its own sha256sum is BSD and
# cannot check a list, so shasum is used there).
if sha256sum --version 2>/dev/null | grep -q GNU; then sha256() { sha256sum "$@"; }; else sha256() { shasum -a 256 "$@"; }; fi

# psql as the database owner, inside the postgres container. Arguments are passed to psql.
owner_psql() { compose exec -T postgres psql -U "$OWNER" -v ON_ERROR_STOP=1 "$@"; }

# The image of the postgres service has tar, which the distroless atlas image does not.
postgres_image() { compose config --images | grep -m1 pgvector; }

# The newest applied migration, written into every backup so a restore knows which schema it brings back.
schema_version() { owner_psql -d "$DB" -Atc "SELECT max(version_id) FROM goose_db_version"; }

die() { echo "error: $*" >&2; exit 1; }
