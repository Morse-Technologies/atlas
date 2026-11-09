<div align="center">

# Atlas

### Document Assistant for Any Organization

**Index. Ask. Cite. Audit.**  
Your documents stay where they live. Every answer carries its source.

[Quick Start](#quick-start) | [Architecture](#architecture) | [Development](#development)

**Read in:** [English](README.md) | [Español](README.es.md) | [中文](README.zh.md)

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://go.dev)
[![Next.js](https://img.shields.io/badge/Next.js-16-000000?style=for-the-badge&logo=nextdotjs&logoColor=white)](https://nextjs.org)
[![React](https://img.shields.io/badge/React-19-61DAFB?style=for-the-badge&logo=react&logoColor=black)](https://react.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?style=for-the-badge&logo=typescript&logoColor=white)](https://www.typescriptlang.org)
[![TailwindCSS](https://img.shields.io/badge/Tailwind-4-06B6D4?style=for-the-badge&logo=tailwindcss&logoColor=white)](https://tailwindcss.com)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-18-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)](https://www.postgresql.org)

**Apache-2.0 core** | Go | Next.js | Postgres | On-premise

</div>

---

> **Atlas** indexes an organization's documentation in place (network folders, shared drives, procedures, manuals, norms, emails) and answers questions in natural language with the exact citation: document, revision, page and section. When the evidence is missing, it says so. Built by [Morse Technologies](https://morsetechnologies.es) for teams that need answers they can trust and trace.

---

## What is Atlas?

Atlas is an **on-premise document assistant**. It reads your sources without moving files, keeps its index fresh on its own, and lets people ask questions the way they would ask a colleague.

Every answer links to the passage it came from. Permissions come from the source and apply before search, so each person only sees answers from documents they can already open. Every question and answer is recorded for audit, in line with EU AI Act traceability.

Models run locally. No document leaves your network.

```bash
# Frontend (mock data, no backend needed)
cd app && pnpm install && pnpm dev
# App at http://localhost:3000

# Backend stack (Postgres, parsers, atlas roles, web, Caddy): see docs/OPERACION.md
cd deploy/compose && cp .env.example .env && docker compose up -d --build
# API at http://127.0.0.1:8080, interface at https://localhost
```

---

## What You Can Do

| Feature | Core | Pro | Description |
|--------|------|-----|-------------|
| **Ask** | YES | YES | Questions in natural language, streamed answers with numbered citations. |
| **Documents** | YES | YES | Browse the index, open the viewer at the cited page, see metadata and revisions. |
| **Sources** | YES | YES | Connect local and network folders; Atlas watches them and reindexes changes. |
| **Users and settings** | YES | YES | Local accounts, roles, two-factor sign-in, GDPR-friendly deletion. |
| **Audit trail** | YES | YES | Append-only, hash-chained record of every question, answer and change; integrity check. |
| **Dashboard** | NO | YES | Daily overview of activity, coverage and open issues. |
| **Quality alerts** | NO | YES | Outdated revisions, contradictions, broken references between documents. |
| **Knowledge map** | NO | YES | Graph of documents and the entities they mention. |
| **Signed audit reports and SSO** | NO | YES | Signed audit exports, OIDC sign-in (Entra ID, Keycloak). |

*Pro features are unlocked by an offline signed license. The UI locks them and the backend enforces it.*

---

## Problem It Solves

- **Answers buried in files.** People open ten PDFs to find one value. Atlas finds the passage and shows it.
- **Answers you cannot check.** Generic chat tools sound confident with no source. Atlas never answers without one.
- **Documents that drift.** Old revisions, conflicting procedures, missing references. Atlas surfaces them before an audit does.
- **Data that cannot leave.** Everything runs on your server, air-gapped if needed.

---

## Architecture

Monorepo by layer: backend, frontend, deployment.

```
server/           Go backend: one binary (atlas) with roles api, worker, watch, migrate
  api/            openapi.yaml, the contract shared with the frontend
  db/             SQL migrations and queries (sqlc)
  internal/       Modules: access, catalog, documents, events, jobs, sources, httpapi
app/              Next.js frontend: Ask, Documents, Sources, Settings, Access
  src/lib/api/    Typed AtlasApi interface and the mock adapter used until the API is wired
  src/ui/         Atoms, cells, organisms, templates
deploy/compose/   Docker Compose stack: Postgres 18 + pgvector, docling-serve, Gotenberg, atlas roles
```

Postgres holds data, vectors, full-text search, the job queue (River) and the audit log. Parsing (Docling), conversion (Gotenberg), embeddings and the LLM run as separate services called over HTTP.

---

## Quick Start

### 1. Requirements

- **Go 1.27+**
- **Node 20+** and **pnpm**
- **Docker** with Compose

```bash
git clone <repo-url>
cd atlas
```

### 2. Frontend

```bash
cd app
pnpm install
pnpm dev
```

App at **http://localhost:3000**. The frontend runs on a typed mock of the API, with a fictional demo organization, so it works without the backend.

### 3. Backend stack

```bash
cd deploy/compose
cp .env.example .env          # set the passwords
docker compose up -d --build
```

Then create the organization and add a source:

```bash
docker compose exec worker /atlas setup --org "My organization"
docker compose exec worker /atlas source add --name Docs --path /sources/docs
docker compose exec worker /atlas quarantine list
```

Put documents in `deploy/compose/sources/` (or set `ATLAS_SOURCES_DIR`). API at **http://localhost:8080**.

---

## Impact & Audience

- **Quality, compliance and IT teams** who need traceable answers and an audit trail.
- **People who ask quick questions daily** and need the right revision, with the page to prove it.
- **Any organization with scattered documentation**: industry, legal, health, public sector, services, education.

---

## Development

```bash
# Backend
cd server
make gen                # regenerate sqlc queries and the OpenAPI server
make lint               # golangci-lint
make test               # unit and architecture tests
make test-integration   # real Postgres via testcontainers (Docker)

# Frontend
cd app
pnpm lint
pnpm build
sh scripts/check-punct.sh   # house punctuation rules
```

Full run on a clean stack with fake models: `deploy/scripts/dev-env.sh && deploy/scripts/e2e.sh`.

Docs: [`docs/ESTADO.md`](docs/ESTADO.md) (project status and backlog), [`docs/C4.md`](docs/C4.md), [`docs/OPERACION.md`](docs/OPERACION.md), [`docs/TESTING.md`](docs/TESTING.md).

Code rules: functions of 20 lines or fewer, 3 parameters or fewer, clear names, no magic numbers.

---

## License

The core is Apache-2.0. The Atlas name and logo are trademarks of Morse Technologies and are not covered by the code license. Atlas Pro is commercial.

---

<div align="center">

**Atlas** by Morse Technologies | Go | Next.js | Postgres | On-premise

</div>
