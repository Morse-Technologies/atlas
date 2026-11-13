#!/usr/bin/env bash
# Builds the real (live) app and fails if anything of the demo ended up in the browser or server bundles.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
NEXT_PUBLIC_ATLAS_API=live pnpm build

if leaks="$(grep -rIlE 'admin@admin\.es|Planta Norte|CNC-07|admin123|Lucía Marín|Production SharePoint|ERP machine master' .next/static .next/server)"; then
  echo "The real build contains demo data:" >&2
  echo "$leaks" >&2
  exit 1
fi
echo "No demo data in the real build."
