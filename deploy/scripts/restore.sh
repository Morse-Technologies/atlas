#!/usr/bin/env bash
# Restores a backup made by backup.sh.
#   restore.sh --verify DIR   restore into a temporary database, check it, and drop it (safe, any time)
#   restore.sh --yes DIR      replace the live database and blobs with the backup (stops Atlas first)
# A backup made with --encrypt (atlas.dump.gpg) is decrypted into a temporary folder first: the private key must be
# in the gpg keyring of whoever runs this, and the plain files are deleted when it ends.
# A backup that has never been restored does not count (ATLAS.md §10.4): run --verify on a schedule.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

mode="${1:-}"
src="${2:?usage: restore.sh --verify|--yes DIR}"
[ -f "$src/MANIFEST" ] && compgen -G "$src/atlas.dump*" >/dev/null || die "$src is not a backup made by backup.sh"
src="$(cd "$src" && pwd)"
plain="$src" # the folder with atlas.dump and blobs.tgz in the clear

check_files() {
  echo "Checking the files against the manifest..."
  (cd "$src" && grep -E '^[0-9a-f]{64}  ' MANIFEST | sha256 -c) \
    || die "a file does not match its checksum: the backup is damaged"
}

decrypt_if_needed() {
  local file
  [ -f "$src/atlas.dump.gpg" ] || return 0
  command -v gpg >/dev/null || die "this backup is encrypted and gpg is not installed"
  plain="$(mktemp -d)"
  echo "Decrypting the backup..."
  for file in atlas.dump blobs.tgz; do
    gpg --batch --yes --output "$plain/$file" --decrypt "$src/$file.gpg"
  done
}

cleanup() {
  [ "$plain" = "$src" ] || rm -rf "$plain"
  owner_psql -d postgres -c "DROP DATABASE IF EXISTS atlas_verify" >/dev/null 2>&1 || true
}

restore_database() {
  local target="$1"
  owner_psql -d postgres -c "DROP DATABASE IF EXISTS $target" -c "CREATE DATABASE $target OWNER $OWNER"
  compose exec -T postgres pg_restore -U "$OWNER" -d "$target" --no-owner --role="$OWNER" < "$plain/atlas.dump"
}

verify() {
  check_files
  decrypt_if_needed
  local target=atlas_verify
  echo "Restoring into the temporary database $target..."
  restore_database "$target"
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
  decrypt_if_needed
  echo "Stopping Atlas..."
  compose stop caddy api worker watch
  echo "Restoring the database..."
  restore_database "$DB"
  echo "Restoring the blobs..."
  docker run --rm --entrypoint sh -v "$BLOBS_VOLUME":/data -v "$plain":/backup:ro "$(postgres_image)" \
    -c 'find /data -mindepth 1 -delete && tar -C /data -xzf /backup/blobs.tgz'
  compose up -d
  echo "Restored. Run: atlas audit verify"
}

trap cleanup EXIT
case "$mode" in
  --verify) verify ;;
  --yes) replace_live ;;
  *) die "usage: restore.sh --verify|--yes DIR" ;;
esac
