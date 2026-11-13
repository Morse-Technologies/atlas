#!/usr/bin/env bash
# The command of the backup container: runs backup.sh every day at ATLAS_BACKUP_AT (HH:MM, container time; default
# 02:30) and keeps ATLAS_BACKUP_KEEP backups (default 7). A failed backup is logged and tried again the next day.
#   backup-loop.sh           run for ever
#   backup-loop.sh --check   health check: fails when the last good backup is older than 26 hours
# With ATLAS_BACKUP_GPG_RECIPIENT set, backups are encrypted; the public keys to import are the files in /keys.
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$here/common.sh"
source "$here/backup-state.sh"

backup_dir=/backups
at="${ATLAS_BACKUP_AT:-02:30}"
keep="${ATLAS_BACKUP_KEEP:-7}"
seconds_per_day=86400
max_age_seconds=$((26 * 3600)) # a day plus two hours of slack

# seconds_until HH:MM: how long to the next time of day (a full day when it is that time right now).
seconds_until() {
  local now target diff
  now=$(( 10#$(date +%H) * 3600 + 10#$(date +%M) * 60 + 10#$(date +%S) ))
  target=$(( 10#${1%:*} * 3600 + 10#${1#*:} * 60 ))
  diff=$(( (target - now + seconds_per_day) % seconds_per_day ))
  echo $(( diff == 0 ? seconds_per_day : diff ))
}

check_last_backup() {
  local age=$(( $(date +%s) - $(last_ok_epoch "$backup_dir") ))
  [ "$age" -le "$max_age_seconds" ] || die "the last good backup is $((age / 3600)) hours old"
}

import_public_keys() {
  [ -n "${ATLAS_BACKUP_GPG_RECIPIENT:-}" ] || return 0
  export GNUPGHOME="$(mktemp -d)"
  ls /keys/* >/dev/null 2>&1 || die "ATLAS_BACKUP_GPG_RECIPIENT is set but /keys has no public key (ATLAS_BACKUP_KEYS_DIR)"
  gpg --batch --import /keys/*
}

run_forever() {
  [[ "$at" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]] || die "ATLAS_BACKUP_AT must be HH:MM, not $at"
  import_public_keys
  # Until the first backup the clock starts now, so the container is healthy for its first day.
  [ -f "$(state_file "$backup_dir")" ] || write_state "$backup_dir" waiting "$(date +%s)"
  while true; do
    echo "Next backup in $(seconds_until "$at") seconds (at $at)."
    sleep "$(seconds_until "$at")"
    "$here/backup.sh" --in-container --keep "$keep" "$backup_dir" || echo "The backup failed; trying again tomorrow at $at." >&2
  done
}

case "${1:-}" in
  --check) check_last_backup ;;
  "") run_forever ;;
  *) die "usage: backup-loop.sh [--check]" ;;
esac
