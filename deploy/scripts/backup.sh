#!/usr/bin/env bash
# Backs up the database and the blob volume into DIR/<timestamp>/ (ATLAS.md §10.4).
#   backup.sh [--keep N] [--encrypt RECIPIENT] [--in-container] DIR
#     --keep N          afterwards keep only the N newest backups in DIR (default: keep all)
#     --encrypt R       encrypt the dump and the blobs with gpg for the key R (default: ATLAS_BACKUP_GPG_RECIPIENT,
#                       if set). The public key must be in the keyring of whoever runs this; restore.sh needs the
#                       private key.
#     --in-container    run inside the backup container (docker compose --profile backup): no Docker or Compose,
#                       it reads Postgres over the network (PGHOST, PGUSER, PGPASSWORD) and the blobs from /data.
# Every run writes DIR/last-backup.json, which the health check of the backup container reads.
# The documents themselves stay in the customer's folders; what a backup protects is what Atlas created:
# corrections, conversations, alerts, the catalog and the audit log.
# It does NOT contain ATLAS_SECRET_KEY or the license file. Keep those somewhere else: without the secret key,
# nobody with a second factor can sign in after a restore. The audit signing key IS in it (in the database), sealed
# with ATLAS_SECRET_KEY, so a restored installation signs and verifies reports with the same key.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
usage="usage: backup.sh [--keep N] [--encrypt RECIPIENT] [--in-container] DIR"
keep=0 recipient="${ATLAS_BACKUP_GPG_RECIPIENT:-}" in_container=0
while [ $# -gt 0 ]; do
  case "$1" in
    --keep) keep="${2:?$usage}"; shift 2 ;;
    --encrypt) recipient="${2:?$usage}"; shift 2 ;;
    --in-container) in_container=1; shift ;;
    -*) echo "$usage" >&2; exit 2 ;;
    *) break ;;
  esac
done
dest_root="${1:?$usage}"
[[ "$keep" =~ ^[0-9]+$ ]] || { echo "--keep needs a number: $usage" >&2; exit 2; }
mkdir -p "$dest_root"
dest_root="$(cd "$dest_root" && pwd)" # lib.sh changes directory, so make the path absolute first

if [ "$in_container" = 1 ]; then source "$here/common.sh"; else source "$here/lib.sh"; fi
source "$here/backup-state.sh"

dump_database() {
  if [ "$in_container" = 1 ]; then pg_dump -Fc > "$1"; else compose exec -T postgres pg_dump -U "$OWNER" -Fc "$DB" > "$1"; fi
}

archive_blobs() {
  if [ "$in_container" = 1 ]; then
    tar -C /data --exclude=./license -czf "$1" .
  else
    docker run --rm --entrypoint tar -v "$BLOBS_VOLUME":/data:ro -v "$(dirname "$1")":/backup "$(postgres_image)" \
      -C /data --exclude=./license -czf "/backup/$(basename "$1")" .
  fi
}

database_schema_version() {
  if [ "$in_container" = 1 ]; then psql -Atc "SELECT max(version_id) FROM goose_db_version"; else schema_version; fi
}

encrypt_files() {
  local file
  for file in atlas.dump blobs.tgz; do
    gpg --batch --yes --trust-model always --recipient "$recipient" --output "$dest/$file.gpg" --encrypt "$dest/$file"
    rm "$dest/$file"
  done
}

write_manifest() {
  {
    echo "created_at=$stamp"
    echo "schema_version=$(database_schema_version)"
    [ -z "$recipient" ] || echo "encrypted=gpg"
    (cd "$dest" && sha256 atlas.dump* blobs.tgz*)
  } > "$dest/MANIFEST"
}

# Old backups go only after the new one is complete, so a failing run never shrinks what there is.
prune_old_backups() {
  [ "$keep" -gt 0 ] || return 0
  ls -1d "$dest_root"/????????T??????Z | sort -r | tail -n +"$((keep + 1))" | while read -r old; do rm -rf "$old"; done
}

# A failed run leaves no half-written folder and is written to the status file; the last good time is kept.
record_outcome() {
  local status=$?
  if [ "$status" -eq 0 ]; then
    write_state "$dest_root" ok "$(date +%s)"
  else
    rm -rf "$dest"
    write_state "$dest_root" failed "$(last_ok_epoch "$dest_root")"
    echo "error: the backup failed (exit $status)" >&2
  fi
}

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
dest="$dest_root/$stamp"
trap record_outcome EXIT
mkdir -p "$dest"

echo "Dumping the database..."
dump_database "$dest/atlas.dump"
echo "Archiving the blobs..."
archive_blobs "$dest/blobs.tgz"
[ -z "$recipient" ] || { echo "Encrypting for $recipient..."; encrypt_files; }
echo "Writing the manifest..."
write_manifest
prune_old_backups

echo "Backup written to $dest"
echo "Reminder: ATLAS_SECRET_KEY and the license file (the blob volume's license folder) are not in it. Store them apart."
