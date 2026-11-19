# Issue guide

## Title

Title says what to add, what mistake to fix, or what to refactor.

```
Add panel to check users in database
Auth user returns 500 instead of 404 on mistake
Refactor document service into smaller functions
```

## Body

1. **Branch** first: `<type>/<scope>` (same types and scopes as [COMMITS.md](COMMITS.md)).
2. **Description**: what and why.
3. **Tasks**: checkboxes with things to do.

## Example

```markdown
**Branch:** fix/backend

## Description
Auth endpoint returns 500 when user does not exist. Should return 404.

## Tasks
- [ ] Return 404 when user not found
- [ ] Add test for missing user
- [ ] Update API docs
```

Use the template: **New issue → Task**.
