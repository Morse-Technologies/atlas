TOOL    := go tool -modfile=$(CURDIR)/tools/go.mod
COMPOSE := docker compose -f ../deploy/compose/compose.yaml

.PHONY: gen lint test test-integration test-golden e2e up down

## gen: regenerate sqlc queries and the OpenAPI server
gen:
	cd db && $(TOOL) sqlc generate
	cd api && $(TOOL) oapi-codegen -config oapi-codegen.yaml openapi.yaml

## lint: code rules of ATLAS_DESIGN.md §22 (20 lines, 3 params, no magic numbers) and more
lint:
	$(TOOL) golangci-lint run ./...

## test: unit and architecture tests, no Docker needed
test:
	go test -race ./...

## test-integration: real Postgres via testcontainers (Docker required)
test-integration:
	go test -race -tags integration ./...

## test-golden: parse testdata/golden with real docling and gotenberg; UPDATE=1 rewrites expectations
test-golden:
	go test -tags golden -timeout 30m ./internal/documents/ -run TestGolden $(if $(UPDATE),-args -update)

## e2e: a clean development stack with fake models, then the API and browser journeys (Docker required)
e2e:
	../deploy/scripts/e2e.sh

## up / down: the compose stack (uses deploy/compose/.env; deploy/scripts/dev-env.sh makes a development one)
up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down
