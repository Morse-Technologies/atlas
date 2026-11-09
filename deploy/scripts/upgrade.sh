#!/usr/bin/env bash
# Upgrades Atlas: backup, then migrate, then restart (ATLAS.md §4.3). Run it from a checkout with the new version,
# or after `docker compose pull`.
#   upgrade.sh [BACKUP_DIR]       BACKUP_DIR defaults to ../backups
# If the migration fails, nothing was restarted and the backup is the way back (restore.sh --yes).
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)" # lib.sh changes directory, so find the scripts first
source "$here/lib.sh"

backup_dir="${1:-$COMPOSE_DIR/../backups}"
echo "1/4 Backing up before touching anything..."
"$here/backup.sh" "$backup_dir"

echo "2/4 Building or pulling the new images..."
compose build --pull 2>/dev/null || compose pull

echo "3/4 Applying migrations (the old version keeps running meanwhile: they only add)..."
compose run --rm migrate

echo "4/4 Restarting on the new version..."
compose up -d
compose ps
echo "Upgrade done. Check: docker compose exec api /atlas audit verify"
