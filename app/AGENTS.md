<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

## Project code rules

- Functions at most 20 lines, at most 3 parameters (more: pass an object), names that say what they do.
- No magic numbers, no clever logic. Smallest change that solves the task.
- `src/lib/api/types.ts` (`AtlasApi`) is the backend contract: change it together with `server/api/openapi.yaml`.
- Architecture and backend plan: `docs/ATLAS.md`, `docs/C4.md`, `docs/TESTING.md`.
