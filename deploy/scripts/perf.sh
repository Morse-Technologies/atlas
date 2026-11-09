#!/usr/bin/env bash
# Measures Atlas under load on the development stack with fake model servers (so it measures Atlas, not models).
#   perf.sh [small|pilot|skew|multi-acl] [seconds per scenario]
# It deletes the "atlasdev" stack's data, fills it with a synthetic corpus (server/cmd/perfseed) and runs the
# scenarios of server/cmd/perfrun. The report goes to deploy/perf-results/ and to the screen.
set -euo pipefail

profile="${1:-small}"
seconds="${2:-60}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root/deploy/compose"
[ -e .env.dev ] || "$root/deploy/scripts/dev-env.sh"
compose() { (cd "$root/deploy/compose" && docker compose --env-file .env.dev -f compose.yaml -f perf.override.yaml "$@"); }
psql_owner() { compose exec -T postgres psql -U atlas_owner -d atlas -Atc "$1"; }
out="$root/deploy/perf-results"; mkdir -p "$out"
report="$out/$profile-$(date +%Y%m%d-%H%M%S).txt"
exec > >(tee "$report") 2>&1

echo "== Atlas performance: profile $profile, $seconds s per scenario, $(date -u +%FT%TZ)"
compose down -v --remove-orphans >/dev/null 2>&1 || true
compose up -d --build >/dev/null 2>&1
for _ in $(seq 1 60); do curl -fsk https://localhost/api/v1/setup >/dev/null 2>&1 && break; sleep 2; done

cd "$root/server"
state="$out/state.json"; questions="$out/questions.json"
go run ./cmd/perfrun setup -url https://localhost -state "$state"
tenant="$(psql_owner 'select id from access.tenants limit 1')"
reader="$(sed -n 's/.*"user_id":"\([^"]*\)".*/\1/p' "$state")"
password="$(sed -n 's/^ATLAS_OWNER_PASSWORD=//p' "$root/deploy/compose/.env.dev")"

echo "== corpus"
go run ./cmd/perfseed -db "postgres://atlas_owner:$password@127.0.0.1:54329/atlas" -tenant "$tenant" -reader "$reader" -profile "$profile" -questions "$questions"
echo "== query plans"; "$root/deploy/scripts/perf-explain.sh" || echo "(the plans could not be read)"

run() { go run ./cmd/perfrun run -url https://localhost -state "$state" -questions "$questions" -duration "${seconds}s" "$@"; }
wait_for_api() { for _ in $(seq 1 60); do curl -fsk https://localhost/api/v1/setup >/dev/null 2>&1 && return; sleep 1; done; }
# The share of passages above which a word is left out of the word search (0: search with every word).
with_common_share() { ATLAS_SEARCH_COMMON_SHARE="$1" compose up -d --force-recreate api >/dev/null 2>&1; wait_for_api; }
echo "== A/B: word search with every word (the way it was)"
with_common_share 0
run -scenario ask -users 20
echo "== A/B: word search without the words in most passages (the default)"
with_common_share 0.02
run -scenario ask -users 20
echo "== cold start: the first question after a restart"
compose restart api >/dev/null 2>&1; wait_for_api
run -scenario ask -users 1 -duration 5s
echo "== one person"; run -scenario ask -users 1
echo "== 20 people asking"; run -scenario ask -users 20
echo "== 20 people browsing documents"; run -scenario documents -users 20
echo "== 20 people, mixed"; run -scenario mixed -users 20
echo "== 50 people asking"; run -scenario ask -users 50
echo "== database"; psql_owner "select pg_size_pretty(pg_database_size('atlas'))"
docker stats --no-stream --format '{{.Name}} {{.MemUsage}}' | sort
echo "report: $report"
