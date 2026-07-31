# US-L.1 — Create a list

**Milestone:** M1
**Depends on:** US-A.1

**As a** member, **I want** to create a shopping list with a name, **so that** my
group can start collecting items for a shop.

## Acceptance criteria

- `POST` endpoint exists in `splitkauf.openapi.yaml` before implementation.
- The new list is persisted in Postgres and visible to all members.
- Validation errors return RFC 9457 Problem Details.
