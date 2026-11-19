# Commit guide

Format:

```
<type>,<scope>: <info>
```

1. **type** first: what kind of change.
2. **scope** next: which part of the repo.
3. `:` then a short **info** line: what changed.

## Types

| Type | Use for |
|---|---|
| `feat` | New feature |
| `fix` | Bug fix |
| `refactor` | Code change, same behavior |
| `docs` | Docs only |
| `style` | Format only, no logic |
| `test` | Add or fix tests |
| `chore` | Deps, config, tooling |
| `perf` | Speed up |

## Scopes

| Scope | Folder |
|---|---|
| `frontend` | `app/` |
| `backend` | `server/` |
| `deploy` | `deploy/` |
| `design` | `design/` |
| `docs` | `docs/` |

## Examples

```
feat,frontend: added panel to check users in database
fix,backend: auth user returns now 404 when mistake happens
refactor,backend: split document service into smaller functions
chore,deploy: bump node image to 22
```

## Rules

- One change per commit.
- Info line short (under ~72 chars), no period at end.
- Need more detail? Leave a blank line, then write a body.
