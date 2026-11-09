#!/usr/bin/env bash
# Backs up the database and the blob volume into DIR/<timestamp>/ (ATLAS.md §10.4).
#   backup.sh DIR
# The documents themselves stay in the customer's folders; what a backup protects is what Atlas created:
# corrections, conversations, alerts, the catalog and the audit log.
# It does NOT contain ATLAS_SECRET_KEY, the license or the audit signing key. Keep those somewhere else:
# without the secret key, nobody with a second factor can sign in after a restore.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

dest_root="${1:?usage: backup.sh DIR}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
dest="$dest_root/$stamp"
mkdir -p "$dest"
dest="$(cd "$dest" && pwd)"

echo "Dumping the database..."
compose exec -T postgres pg_dump -U "$OWNER" -Fc "$DB" > "$dest/atlas.dump"

echo "Archiving the blobs..."
docker run --rm --entrypoint tar -v "$BLOBS_VOLUME":/data:ro -v "$dest":/backup "$(postgres_image)" \
  -C /data -czf /backup/blobs.tgz .

echo "Writing the manifest..."
{
  echo "created_at=$stamp"
  echo "schema_version=$(schema_version)"
  (cd "$dest" && sha256 atlas.dump blobs.tgz)
} > "$dest/MANIFEST"

echo "Backup written to $dest"
echo "Reminder: ATLAS_SECRET_KEY and the license file are not in it. Store them apart."
