#!/usr/bin/env bash
# Checks that every image we do not build ourselves is pinned by digest (name@sha256:...), and that the production
# image of Atlas has no fake model servers in it.
#   check-digests.sh           both checks (the second one builds the image, so it needs Docker)
#   check-digests.sh --lines   only the first, a text check with no Docker
# A tag can be moved by whoever owns the registry; a digest cannot. To update one:
#   docker buildx imagetools inspect IMAGE:TAG   (the "Digest:" line), then change both the tag and the digest.
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dockerfiles=("$root/server/Dockerfile" "$root/app/Dockerfile")
local_images='image: (\*|(atlas|atlas-web|atlas-fake):dev)' # built here, or a YAML alias of an image already checked

compose_images_without_digest() {
  grep -E '^\s*image:' "$root/deploy/compose/compose.yaml" | grep -vE "$local_images" | grep -v '@sha256:' || true
}

# FROM lines whose image is neither pinned nor an earlier stage of the same file.
from_images_without_digest() {
  awk 'tolower($1) == "from" {
         if (!($2 in stage) && $2 !~ /@sha256:/) print FILENAME ": " $0
         for (i = 3; i < NF; i++) if (tolower($i) == "as") stage[$(i + 1)] = 1
       }' "$@"
}

check_digests() {
  local unpinned
  unpinned="$(compose_images_without_digest; from_images_without_digest "${dockerfiles[@]}")"
  [ -z "$unpinned" ] || die "images without @sha256: digest:"$'\n'"$unpinned"
  echo "Every image is pinned by digest."
}

check_production_image_has_no_fake_models() {
  docker build -q --target prod -t atlas:check "$root/server" >/dev/null
  if docker run --rm --entrypoint /fakemodels atlas:check >/dev/null 2>&1; then
    die "the production image contains /fakemodels"
  fi
  echo "The production image has no fake model servers."
}

check_digests
[ "${1:-}" = --lines ] || check_production_image_has_no_fake_models
