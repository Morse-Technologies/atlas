#!/usr/bin/env bash
# Throws away the data of the "atlasdev" development stack and starts it again empty, without rebuilding images.
# Uses ATLAS_ENV_FILE (default .env.dev); its COMPOSE_PROFILES picks the models.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../compose"
env_file="${ATLAS_ENV_FILE:-.env.dev}"
docker compose --env-file "$env_file" down -v --remove-orphans >/dev/null 2>&1
docker compose --env-file "$env_file" up -d >/dev/null 2>&1
for _ in $(seq 1 60); do curl -fsk https://localhost/api/v1/setup >/dev/null 2>&1 && exit 0; sleep 2; done
echo "the stack did not come up" >&2; exit 1
