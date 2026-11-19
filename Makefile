# One entry point for every check CI runs. It only delegates: the commands live in server/Makefile,
# app/package.json and deploy/scripts.
.PHONY: check check-fast server-fast server-slow app e2e

## check-fast: everything that needs no Docker (what to run before every commit)
check-fast: server-fast app

## check: check-fast plus integration tests and both end-to-end journeys (Docker required)
check: check-fast server-slow e2e

server-fast:
	$(MAKE) -C server lint test vuln

server-slow:
	$(MAKE) -C server test-integration

app:
	cd app && pnpm typecheck && pnpm lint && pnpm test && pnpm build && pnpm check:demo-leak

e2e:
	deploy/scripts/e2e.sh
	deploy/scripts/e2e.sh --pro
