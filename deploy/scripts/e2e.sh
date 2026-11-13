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
# The override mounts the sample questions of `atlas eval` in the api container (never part of a real installation).
compose_files=(-f "$root/deploy/compose/compose.yaml" -f "$root/deploy/compose/e2e.override.yaml")
compose() { docker compose --env-file "$env_file" "${compose_files[@]}" "$@"; }
export ATLAS_E2E_API_EXEC="docker compose --env-file $root/deploy/compose/$env_file ${compose_files[*]} exec -T api"

# Fails unless every container of the stack reports `healthy` (docker compose ps); the slow ones get two minutes.
wait_until_healthy() {
  local not_healthy=""
  for _ in $(seq 1 60); do
    not_healthy="$(compose ps --format '{{.Service}} {{.Health}}' | grep -v ' healthy$' || true)"
    [ -z "$not_healthy" ] && return 0
    sleep 2
  done
  echo "Containers that are not healthy:"; echo "$not_healthy"; compose ps
  return 1
}

# The metrics port is not published: read it from inside the Compose network (the Caddy image has wget).
check_metrics() {
  local text
  for role in api worker; do
    text="$(compose exec -T caddy wget -q -O - "http://$role:9090/metrics")" || { echo "No metrics from $role on :9090"; return 1; }
    grep -q '^# TYPE atlas_jobs gauge' <<<"$text" || { echo "$role metrics lack atlas_jobs"; return 1; }
  done
  text="$(compose exec -T caddy wget -q -O - http://api:9090/metrics)"
  grep -q '^atlas_http_requests_total' <<<"$text" || { echo "api metrics lack atlas_http_requests_total"; return 1; }
}

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
# The Pro journey installs the development license by the API / Settings, like a customer would.
[ "$edition" != pro ] || export ATLAS_E2E_LICENSE_FILE="$root/deploy/compose/license/dev.lic"

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

wait_until_healthy
echo "Metrics..."
check_metrics
echo "API journey ($edition)..."
(cd "$root/server" && ATLAS_E2E_EDITION=$edition ATLAS_E2E_URL=https://localhost go test -tags e2e ./e2e -count=1)

if [ "${SKIP_BROWSER:-}" != "1" ] && [ -d "$root/app/node_modules/@playwright" ]; then
  echo "Browser journey..."
  compose down -v >/dev/null 2>&1 && compose up -d
  for _ in $(seq 1 60); do curl -fsk https://localhost/api/v1/setup >/dev/null 2>&1 && break; sleep 2; done
  wait_until_healthy
  (cd "$root/app" && ATLAS_E2E_EDITION=$edition pnpm e2e)
fi
echo "End-to-end tests passed."
