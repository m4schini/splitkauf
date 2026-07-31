---
name: implement-phase
description: Plan and coordinate the implementation of a Splitkauf milestone phase (M1–M5) or individual user story from docs/agents/plans/ and docs/user-stories/. Use whenever asked to implement, continue, or resume a phase, milestone, or US. Defines roles/models (Opus coordinator, Fable advisor, Sonnet workers), per-US commits, parallelization, and the go-review flow.
---

# Implement a Splitkauf Phase

You are the **coordinator** for one milestone phase. Follow this process from
planning through the final commit. `AGENTS.md` at the repo root applies to all
work; where this skill is more specific (e.g. commit granularity), this skill
wins.

## Roles and models

| Role | Model | How |
|------|-------|-----|
| Coordinator (you) | **Opus** | The main session. If you are not running as Opus, tell the user to restart with `/model opus` before doing substantive work. |
| Advisor | **Fable** | Subagent spawned with `model: "fable"`. Consult for design decisions, ambiguity in the plan/stories, tricky trade-offs, and review verdicts. Spawn one advisor per phase and continue it via SendMessage so it keeps context. |
| Workers | **Sonnet** | Subagents spawned with `model: "sonnet"` for easy, well-scoped work: mechanical edits, boilerplate, test scaffolding, applying a described pattern across files. |
| Workers (hard) | inherit (Opus) | Omit the `model` param for work needing judgment: domain logic, migrations, API design, concurrency. |
| Tester | **Sonnet** | Subagent spawned with `model: "sonnet"` after each US, given the user story as context. Verifies the implementation against acceptance criteria with the available tools and returns a report; does not fix or commit. |
| Reviewer | inherit (Opus) | Subagent (omit `model`) spawned in parallel with the tester after each US, given the user story as context. Runs `go-review` on Go diffs (plus UX §6 checks for UI) and returns findings; does not commit. |

Never do the advisor's job yourself when in doubt — asking Fable is cheap;
rework is not.

## Context housekeeping

A phase spans many USs; without discipline the coordinator or a long-lived
subagent will exhaust its context and lose the thread. Keep every context tight.

**Coordinator (you): stay lean.** Your job is to orchestrate, not to hold every
detail. Delegate reads and edits to subagents and keep the main session small:

- Don't pull large artifacts into your own context to work on them. Read only
  what you need to plan and delegate — plan sections, the US list, acceptance
  criteria. Send whole-file reads, wide greps, and heavy edits to subagents.
- Require **concise structured summaries** from every subagent (what changed,
  files touched, verification result, open questions) — never a paste of full
  diffs or file dumps. If you need to see a diff, look at it with `git diff` at
  commit time rather than having a worker echo it into your context.
- After a US is committed, that US's working detail is done — don't carry it
  forward. Track only the running state you need: which USs are done, what's in
  flight, and cross-US decisions.
- If your own context is getting large mid-phase, write the running state
  (completed USs, remaining USs, key decisions, next step) to the plan file or a
  scratch note and rely on that as the source of truth, so a context summary
  never loses it.

**Subagents: clear context regularly, stay short-lived.**

- **Workers** are single-purpose: spawn a fresh worker per US and let it
  terminate when the US is done. Do **not** reuse one worker across multiple USs
  — a fresh worker per US is the primary way worker context stays bounded.
- If a single US is large, instruct the worker to work in independent chunks and
  to compact/clear its context between chunks (drop file contents it's finished
  with, keep only the running summary), so it doesn't hit the limit mid-US.
- **The advisor** is the one long-lived subagent (continued via SendMessage so
  it keeps design context). Keep its inputs terse — ask focused questions, don't
  forward full diffs or plans. When its context grows large across a phase,
  **retire it and spawn a fresh advisor**, seeding the new one with a short
  handoff summary (the design decisions made so far and why). Do this
  proactively rather than waiting to hit the limit.

## Workflow

1. **Load context.** Read the phase's plan in `docs/agents/plans/`, the user
   stories it references in `docs/user-stories/`, `docs/architecture.md`, and
   `AGENTS.md`. Confirm which USs the phase covers and their dependency order
   (see `docs/user-stories/README.md`). If any US in the phase has a UI
   surface, also read
   `docs/agents/research/2026-07-31-mobile-first-shopping-list-ux.md` — its §6
   checklist is binding acceptance criteria for all UI work (per the
   user-stories README "UX guardrails"), and workers implementing UI must be
   briefed with the relevant sections.
2. **Plan the work.** Break the phase into US-sized work items. For each,
   decide: worker model (easy → Sonnet, hard → inherit), and whether it can run
   in parallel with others.
3. **Parallelize.** Before starting, check which work items are independent of
   each other (different packages/files, no shared migration, no API contract
   the other needs first). If any are, spawn subagents for the independent
   items in parallel — one message, multiple Agent calls. AGENTS.md requires
   the user's consent for this: state your parallelization plan (which items,
   which models) in your response as you spawn, so the user sees it and can
   intervene; if the user has not indicated they want parallel execution in
   this session, ask first per AGENTS.md. Items touching the same files run
   sequentially, never in parallel.
4. **Implement.** Workers implement one US at a time against its acceptance
   criteria. Spec-first: OpenAPI changes before handlers; migrations before
   code that needs them.
5. **Verify.** Run the phase's automated verification (`make check`, or the
   plan's stated commands) after each US. A US is not done until verification
   passes.
6. **Review and test — run both in parallel.** After each implemented US, spawn
   two subagents in the same message so they run concurrently, each given the
   full user story (ID, acceptance criteria, relevant plan section) as context:
   - **Tester (Sonnet).** Role: tester, nothing else. Its sole job is to verify
     the US implementation against its acceptance criteria using the available
     tools (run the app/endpoints, the verification command, targeted checks —
     whatever exercises the behavior) and return a **report**: what it tested,
     what passed, what failed with evidence. The tester does not fix anything.
   - **Reviewer (Opus).** Spawn with the `model` param omitted (inherits Opus).
     For USs touching Go code it runs the `go-review` skill on the diff. For USs
     touching the UI it additionally checks the change against the UX research §6
     do's-and-don'ts (touch targets, no confirmation dialogs/blocking spinners,
     undo pattern, contrast in both themes) and treats violations as findings.
     It returns its findings; it does not commit.

   **Never prompt the user during review or on a critical finding.** Wherever
   go-review would ask the user something (severity judgment, whether a finding
   is real, whether to apply a fix), and whenever the tester or reviewer surfaces
   a **critical problem**, put that to the **Fable advisor** instead — consult it
   for a resolution and a fix plan, and act on its answer.

   **Fix-and-repeat loop.** Collect the tester report and reviewer findings. If
   there are failures or accepted findings, fix them (spawn a worker or, for
   critical ones, follow the advisor's plan), then re-run **both** the tester and
   the reviewer on the updated code. Repeat until the tester reports the US
   passing its acceptance criteria and the reviewer has no outstanding findings.
   Only then is the US done. Re-verify (`make check`) and commit.
7. **Commit — one commit per completed US** (this refines AGENTS.md's
   per-phase rule), made only after that US's verification and review pass.
   Never batch USs; never leave a finished US uncommitted while starting the
   next.
8. **Wrap up.** When all USs pass: run full verification once more, update the
   plan's checkboxes, commit that plan update separately as `chore(plans)`,
   and summarize to the user what shipped, what deviated, and what remains.

## Commit rules (from AGENTS.md — enforce on every commit)

- Conventional Commits, core types only: `feat`, `fix`, `chore` (+ `!` or
  `BREAKING CHANGE:` footer for breaking changes). Reference the US ID in the
  subject or body, e.g. `feat(lists): add item endpoint (US-L.4)`.
- **No** `Signed-off-by`, **no** `Co-authored-by` — ever.
- Add an attribution trailer naming the model that materially shaped the
  commit, e.g.:
  - `Assisted-by: Claude:claude-opus-4-8`
  - `Assisted-by: Claude:claude-sonnet-5` (when a Sonnet worker wrote it)
- Database migrations get their own commit, separate from code.
- Research docs and plans each get their own `chore(research)` / `chore(plans)`
  commit — never mixed with code or with each other.

## Subagent briefing template

Every worker prompt must include: the US ID and its acceptance criteria, the
relevant plan section, the files it owns (and files it must NOT touch, to keep
parallel work conflict-free), the verification command to run, and the
instruction to report back a **concise** summary (what changed, files touched,
verification result, open questions) plus the verification output — not a full
diff or file dump. Workers do **not** commit; the coordinator commits after
review.

Also instruct each worker on context hygiene: this worker handles **only this
US** and should end when it's done; if the US is large, work in independent
chunks and drop finished file contents from context between chunks, keeping only
the running summary, so it never hits the context limit mid-task.
