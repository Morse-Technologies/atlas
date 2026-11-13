# The status file of the backups, last-backup.json in the backup folder. Written by backup.sh, read by the health
# check of the backup container. Source it; do not run it.
#   {"status":"ok|failed|waiting","last_attempt":"<UTC time>","last_ok_epoch":<seconds, 0 = never>}

state_file() { echo "$1/last-backup.json"; }

# last_ok_epoch DIR: when the last good backup finished (0 when there is none).
last_ok_epoch() {
  local epoch=""
  [ ! -f "$(state_file "$1")" ] || epoch="$(sed -n 's/.*"last_ok_epoch": *\([0-9]*\).*/\1/p' "$(state_file "$1")")"
  echo "${epoch:-0}"
}

# write_state DIR STATUS LAST_OK_EPOCH
write_state() {
  printf '{"status":"%s","last_attempt":"%s","last_ok_epoch":%s}\n' "$2" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$3" \
    > "$(state_file "$1").tmp"
  mv "$(state_file "$1").tmp" "$(state_file "$1")"
}
