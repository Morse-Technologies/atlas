# Helpers with no Docker or Compose in them, shared by lib.sh and the backup container. Source it; do not run it.
set -euo pipefail

# SHA-256 of files with the same output and `-c` on Linux (GNU sha256sum) and macOS (its own sha256sum is BSD and
# cannot check a list, so shasum is used there).
if sha256sum --version 2>/dev/null | grep -q GNU; then sha256() { sha256sum "$@"; }; else sha256() { shasum -a 256 "$@"; }; fi

die() { echo "error: $*" >&2; exit 1; }
