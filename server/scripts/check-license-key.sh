#!/bin/sh
# Usage: check-license-key.sh <public key file> "<GO_TAGS>"
# A production build (no devlicense tag) must embed Morse's license public key: base64 of 32 bytes (Ed25519).
set -eu

key_file="$1"
tags="${2:-}"
ed25519_public_key_size=32

case " $(echo "$tags" | tr ',' ' ') " in
*" devlicense "*) exit 0 ;;
esac

fail() {
	echo "atlas: this is a production build and server/internal/platform/license/morse_ed25519.pub $1. Put Morse's public key in that file (docs/LICENCIAS.md), or build with GO_TAGS=devlicense for development." >&2
	exit 1
}

if [ ! -f "$key_file" ] || ! grep -q '[^[:space:]]' "$key_file"; then
	fail "is empty, so Pro could never be activated"
fi
size=$(tr -d '[:space:]' <"$key_file" | base64 -d 2>/dev/null | wc -c | tr -d ' ') || size=0
if [ "$size" != "$ed25519_public_key_size" ]; then
	fail "is not a valid base64 Ed25519 public key"
fi
