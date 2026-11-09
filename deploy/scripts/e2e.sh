#!/usr/bin/env bash
# Starts a clean development stack and runs the end-to-end tests against it.
#   e2e.sh [fake|ollama] [--pro]   API journey (Go) and, if app/node_modules exists, the browser journey
#                                  (Playwright). Options as dev-env.sh: real small models, Pro licence screens.
# It deletes the volumes of the "atlasdev" project (never another installation's) so the stack starts empty.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root/deploy/compose"
env_file="$("$root/deploy/scripts/dev-env.sh" --path "$@")"
[ -e "$env_file" ] || "$root/deploy/scripts/dev-env.sh" "$@"
export ATLAS_ENV_FILE="$env_file"
compose() { docker compose --env-file "$env_file" "$@"; }

echo "Starting a clean stack..."
compose down -v --remove-orphans >/dev/null 2>&1 || true
compose up -d --build

echo "Waiting for the API..."
for _ in $(seq 1 60); do
  if curl -fsk https://localhost/api/v1/setup >/dev/null 2>&1; then break; fi
  sleep 2
done
curl -fsk https://localhost/api/v1/setup >/dev/null || { compose ps; compose logs --tail 30 api caddy; exit 1; }

case " $* " in *" ollama "*) real=1 ;; *) real=0 ;; esac
case " $* " in *" --pro "*) edition=pro ;; *) edition=core ;; esac

if [ "$real" = 1 ]; then
  # The models are downloaded once and loaded on first use: wait until every model answers.
  echo "Waiting for Ollama and its models (the first run downloads about 2 GB)..."
  for _ in $(seq 1 450); do curl -fs http://127.0.0.1:8080/readyz >/dev/null 2>&1 && break; sleep 2; done
  curl -fs http://127.0.0.1:8080/readyz >/dev/null || { compose logs --tail 20 ollama-pull llm-cpu; exit 1; }
  echo "Real-model journey..."
  (cd "$root/server" && ATLAS_E2E_REAL_MODELS=1 ATLAS_E2E_URL=https://localhost go test -tags e2e ./e2e -run SmallRealModels -count=1 -v -timeout 40m)
  echo "Real-model journey passed."
  exit 0
fi

echo "API journey ($edition)..."
(cd "$root/server" && ATLAS_E2E_EDITION=$edition ATLAS_E2E_URL=https://localhost go test -tags e2e ./e2e -count=1)

if [ "${SKIP_BROWSER:-}" != "1" ] && [ -d "$root/app/node_modules/@playwright" ]; then
  echo "Browser journey..."
  compose down -v >/dev/null 2>&1 && compose up -d
  for _ in $(seq 1 60); do curl -fsk https://localhost/api/v1/setup >/dev/null 2>&1 && break; sleep 2; done
  (cd "$root/app" && ATLAS_E2E_EDITION=$edition pnpm e2e)
fi
echo "End-to-end tests passed."
