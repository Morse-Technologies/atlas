#!/usr/bin/env bash
# Tests upgrade.sh without Docker: a fake `docker` records every call and fails the subcommands it is told to.
#   bash deploy/scripts/upgrade_test.sh
set -euo pipefail

scripts="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# A copy of the scripts beside an empty compose folder, with a backup.sh that does nothing.
mkdir -p "$work/deploy/scripts" "$work/deploy/compose" "$work/bin"
cp "$scripts/upgrade.sh" "$scripts/lib.sh" "$scripts/common.sh" "$work/deploy/scripts/"
printf '#!/usr/bin/env bash\n' > "$work/deploy/scripts/backup.sh"
cat > "$work/bin/docker" <<'DOCKER'
#!/usr/bin/env bash
echo "docker $*" >> "$CALLS"
for failing in $FAILING; do [ "$2" != "$failing" ] || exit 1; done
DOCKER
chmod +x "$work/deploy/scripts/backup.sh" "$work/bin/docker"

# run_upgrade "SUBCOMMANDS": runs upgrade.sh while those `docker compose` subcommands fail.
run_upgrade() {
  : > "$work/calls"
  FAILING="$1" CALLS="$work/calls" PATH="$work/bin:$PATH" bash "$work/deploy/scripts/upgrade.sh" "$work/backups" > "$work/out" 2>&1
}

fail() { echo "FAIL: $1"; cat "$work/out"; echo "--- calls:"; cat "$work/calls"; exit 1; }
called() { grep -q "docker compose $1" "$work/calls"; }

if run_upgrade "build pull"; then fail "upgrade.sh succeeded although the build and the pull failed"; fi
! called "run --rm migrate" || fail "migrated after the build and the pull failed"
! called "up" || fail "restarted after the build and the pull failed"

run_upgrade "build" || fail "a failed build must fall back to the pull"
called "run --rm migrate" || fail "did not migrate after the pull"

run_upgrade "" || fail "upgrade.sh failed with a healthy Docker"
called "run --rm migrate" && called "up -d" || fail "did not migrate and restart"
echo "upgrade_test: ok"
