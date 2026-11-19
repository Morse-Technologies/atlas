TOOL    := go tool -modfile=$(CURDIR)/tools/go.mod
COMPOSE := docker compose -f ../deploy/compose/compose.yaml

.PHONY: gen lint vuln test test-integration test-golden e2e check-prod-build up down

## gen: regenerate sqlc queries and the OpenAPI server
gen:
	cd db && $(TOOL) sqlc generate
	cd api && $(TOOL) oapi-codegen -config oapi-codegen.yaml openapi.yaml

## lint: code rules of ATLAS_DESIGN.md §22 (20 lines, 3 params, no magic numbers) and more
lint:
	$(TOOL) golangci-lint run ./...

## vuln: known vulnerabilities in the code Atlas calls and in its dependencies (needs the network for the database)
vuln:
	$(TOOL) govulncheck ./...

## test: unit and architecture tests, no Docker needed (the license package also runs with the devlicense tag)
test:
	go test -race ./...
	go test -race -tags devlicense ./internal/platform/license

## test-integration: real Postgres via testcontainers (Docker required)
test-integration:
	go test -race -tags integration ./...

## test-golden: parse testdata/golden with real docling and gotenberg; UPDATE=1 rewrites expectations
test-golden:
	go test -tags golden -timeout 30m ./internal/documents/ -run TestGolden $(if $(UPDATE),-args -update)

## e2e: a clean development stack with fake models, then the API and browser journeys (Docker required)
e2e:
	../deploy/scripts/e2e.sh

## check-prod-build: fails while Morse's public key is missing or invalid (not part of make check until the key is embedded)
check-prod-build:
	sh scripts/check-license-key.sh internal/platform/license/morse_ed25519.pub ""

## up / down: the compose stack (uses deploy/compose/.env; deploy/scripts/dev-env.sh makes a development one)
up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down
