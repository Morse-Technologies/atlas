#!/usr/bin/env bash
# Writes an env file in deploy/compose for local development: random secrets, a small docling, and its own
# project name so its volumes never mix with another installation on the same machine.
#   dev-env.sh [fake|ollama] [--pro]
#     fake    (default) the deterministic fake model servers -> .env.dev
#     ollama  real small models with Ollama (qwen2.5:1.5b + bge-m3, about 3 GB of RAM) -> .env.ollama
#     --pro   also signs a one-year Pro development license (alerts, map, audit reports) with a throwaway key
#     --path  only print the env file name for these options (.env.dev, .env.dev-pro, .env.ollama, .env.ollama-pro)
#   then: export ATLAS_ENV_FILE=.env.dev   (every script and `docker compose --env-file` use it)
# Never use these files in production: there the passwords, the secret key and the license are yours.
set -euo pipefail

mode=fake pro=false print_path=false
for arg in "$@"; do
  case "$arg" in
    fake|ollama) mode=$arg ;;
    --pro) pro=true ;;
    --path) print_path=true ;;
    *) echo "usage: dev-env.sh [fake|ollama] [--pro] [--path]" >&2; exit 2 ;;
  esac
done

compose_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../compose" && pwd)"
server_dir="$compose_dir/../../server"
name=$mode; [ "$mode" != fake ] || name=dev
$pro && name="$name-pro"
$print_path && { echo ".env.$name"; exit 0; }
target="$compose_dir/.env.$name"
[ ! -e "$target" ] || { echo "error: $target already exists; delete it first if you want a new one" >&2; exit 1; }

random() { openssl rand -base64 24 | tr -d '/+=\n'; }

models_fake() {
  cat <<ENV
COMPOSE_PROFILES=fake
ATLAS_EMBED_URL=http://fake-models:8080
ATLAS_RERANK_URL=http://fake-models:8080
ATLAS_LLM_URL=http://fake-models:8080/v1
ATLAS_LLM_MODEL=fake-llm
ENV
}

# Ollama has no reranker: a small TEI one (rerank-cpu) lets the language model answer.
models_ollama() {
  cat <<ENV
COMPOSE_PROFILES=cpu
ATLAS_EMBED_API=ollama
ATLAS_EMBED_URL=http://llm-cpu:11434
ATLAS_EMBED_MODEL=bge-m3
ATLAS_RERANK_URL=http://rerank-cpu:80
ATLAS_RERANK_TIMEOUT=90s
ATLAS_LLM_URL=http://llm-cpu:11434/v1
ATLAS_LLM_MODEL=${ATLAS_OLLAMA_LLM:-qwen2.5:1.5b}
ENV
}

# A throwaway key pair and a license signed with it; the public key goes to the server, the private one stays here.
license_env() {
  local dir="$compose_dir/license"
  (cd "$server_dir" && go run ./cmd/licensegen keygen --dir "$dir/.keys" >/dev/null \
    && go run ./cmd/licensegen sign --key "$dir/.keys/private.key" --org "Planta Norte" --days 365 \
         --features alerts,map,audit_reports --out "$dir/dev.lic")
  echo "ATLAS_EDITION=pro"
  echo "ATLAS_LICENSE_FILE=/license/dev.lic"
  echo "ATLAS_LICENSE_PUBLIC_KEY=$(cat "$dir/.keys/public.key")"
}

{
  cat <<ENV
COMPOSE_PROJECT_NAME=atlasdev
ATLAS_OWNER_PASSWORD=$(random)
ATLAS_APP_PASSWORD=$(random)
ATLAS_SECRET_KEY=$(openssl rand -base64 32)
ATLAS_HOST=localhost
ATLAS_SOURCES_DIR=./sources
# Docker Desktop gives containers about 8 GB in total; docling alone may not take all of it.
ATLAS_DOCLING_MEM=${ATLAS_DOCLING_MEM:-4g}
ENV
  "models_$mode"
  if $pro; then license_env; else echo "ATLAS_EDITION=core"; fi
} > "$target"
chmod 600 "$target"
echo "Wrote $target"
