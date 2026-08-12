# US-L.11 — See who did what

**Milestone:** M8
**Depends on:** US-L.2, US-L.7, US-A.5

**As a** member of a shared household, **I want** to see who created a list and
who added or bought each item, **so that** I know whether something is already
taken care of and who to ask about it.

## Acceptance criteria

- Each list row in the overview names its creator: "3 open · 1 done · by Alex",
  or "· by you" for the viewer's own lists.
- An open item shows a muted "Added by Alex" / "Added by you" line under its
  quantity and note; a checked item shows "Bought by …" instead.
- "You" is decided by comparing user ids with the signed-in user, not by
  matching names — the viewer reads as "you" even if their display name is
  unknown.
- Nothing is shown where the attribution is unknown: lists and items that
  predate this feature, or an account with no resolvable display name. A raw
  user id is never shown.
- Unchecking an item clears its buyer; checking it again credits whoever checked
  it this time, who may be someone else. Re-checking an already-checked item
  does not reassign it.
- Copying a list credits the copier as the creator of the copy and the adder of
  every item on it, whoever assembled the original.
- Attributions appear immediately on the viewer's own actions, including
  offline; changes queued offline are credited to the acting member once they
  replay.
- Renaming a member updates their name everywhere it appears, including on
  actions taken long before the rename.
