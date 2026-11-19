# Changelog

All notable changes to Atlas are written here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and the project follows [Semantic Versioning](https://semver.org/).

## [1.0.0-rc.1] - 2026-11-19

First release candidate of Atlas Core.

### Added
- Search and answers: ask a question in plain language and get an answer with citations to the documents it came from,
  follow-up questions, and search that understands the language of the document.
- Documents: upload files or let Atlas watch folders, with parsing of office and PDF files, document versions,
  related documents, a viewer, metadata extraction and a quarantine with the reason for files it could not read.
- Sources and access: folders as sources, per-folder access rules, and people names recognised in documents.
- Accounts: local users with roles, single-use codes, account recovery, lockout after repeated failures and
  the right to delete an account.
- Audit: a tamper-evident audit trail of every administrative action.
- Operations: retention rules, maintenance jobs, metrics, health checks, an evaluation command with sample
  questions, and licence installation.
- Deployment: a Docker Compose stack with a reverse proxy, scheduled encrypted backups and restore, an upgrade
  script, pinned image digests and a vulnerability scan.
- Web app: screens for asking, documents, sources, users and settings, a menu that depends on the role, and
  readable texts for every error.
- One `make check` command that runs every test and check, including the end-to-end journeys.

### Changed
- The integration tests share one database container per package, so they run in about a third of the time.
- The web app's type check now works on a clean checkout.
