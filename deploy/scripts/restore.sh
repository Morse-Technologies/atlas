#!/usr/bin/env bash
# Restores a backup made by backup.sh.
#   restore.sh --verify DIR   restore into a temporary database, check it, and drop it (safe, any time)
#   restore.sh --yes DIR      replace the live database and blobs with the backup (stops Atlas first)
# A backup that has never been restored does not count (ATLAS.md §10.4): run --verify on a schedule.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

mode="${1:-}"
src="${2:?usage: restore.sh --verify|--yes DIR}"
[ -f "$src/atlas.dump" ] && [ -f "$src/MANIFEST" ] || die "$src is not a backup made by backup.sh"
src="$(cd "$src" && pwd)"

check_files() {
  echo "Checking the files against the manifest..."
  (cd "$src" && grep -E '^[0-9a-f]{64}  ' MANIFEST | sha256 -c) \
    || die "a file does not match its checksum: the backup is damaged"
}

restore_database() {
  local target="$1"
  owner_psql -d postgres -c "DROP DATABASE IF EXISTS $target" -c "CREATE DATABASE $target OWNER $OWNER"
  compose exec -T postgres pg_restore -U "$OWNER" -d "$target" --no-owner --role="$OWNER" < "$src/atlas.dump"
}

verify() {
  check_files
  local target=atlas_verify
  echo "Restoring into the temporary database $target..."
  restore_database "$target"
  trap 'owner_psql -d postgres -c "DROP DATABASE IF EXISTS atlas_verify" >/dev/null' EXIT
  echo "Counting rows (live vs restored)..."
  for table in documents.documents audit.events search.answers access.users; do
    live="$(owner_psql -d "$DB" -Atc "SELECT count(*) FROM $table")"
    restored="$(owner_psql -d "$target" -Atc "SELECT count(*) FROM $table")"
    echo "  $table: live=$live restored=$restored"
  done
  echo "Verifying the audit chain of the restored copy..."
  compose run --rm --no-deps -e "ATLAS_DATABASE_URL=postgres://atlas_app:${ATLAS_APP_PASSWORD:?set ATLAS_APP_PASSWORD}@postgres:5432/$target?sslmode=disable" \
    api audit verify
  echo "The backup restores and its audit chain is intact."
}

replace_live() {
  check_files
  echo "Stopping Atlas..."
  compose stop caddy api worker watch
  echo "Restoring the database..."
  restore_database "$DB"
  echo "Restoring the blobs..."
  docker run --rm --entrypoint sh -v "$BLOBS_VOLUME":/data -v "$src":/backup:ro "$(postgres_image)" \
    -c 'find /data -mindepth 1 -delete && tar -C /data -xzf /backup/blobs.tgz'
  compose up -d
  echo "Restored. Run: atlas audit verify"
}

case "$mode" in
  --verify) verify ;;
  --yes) replace_live ;;
  *) die "usage: restore.sh --verify|--yes DIR" ;;
esac
