# Prompt: audit the private v1 into the porting queue
> Paste to one agent. Input: `{v1_repo}` — a local clone of the private v1 repo (read-only). Done when `docs/PORTING.md` merges with every v1 feature in a row and the local map is written.

You are the auditor. Goal: a complete, deduplicated inventory of the v1 — every branch, module, top-level feature and documented idea — as `docs/PORTING.md`, the single porting queue. You change nothing in the v1 and build nothing in v2.

## Instructions
1. Enumerate the feature sources in the v1 clone: all local and remote branches including dead ends, module directories, top-level features and scripts, the v1 design brief and its phase list and backlog, the decisions log, the todo file, and anything reachable only from one branch.
2. Write one row per feature, deduplicated across branches: the same feature on several branches is one row that lists them all. Columns:
   `id | name | what it does | v2 module | depends on | port action | size | priority | state`
3. State: `todo`, `unclear` or `duplicate`. Port action: `port`, `merge` (into another row) or `drop` with a one-line reason. Never leave a field empty; unknown → `unclear` plus a question in Notes.
4. The v2 module names follow `templates/module/module.json` naming and the Top Modules roadmap in the README; name any v1 concept with no clear v2 home in Notes — the humans decide new homes.
5. Priority: `P0` modules other modules need (module manager, profiles, locales, interfaces), `P1` spine features (tasks, memory, events, scheduler), `P2` integrations (bot, co-op, router), `P3` the rest. Size: `S`, `M`, `L`.
6. Write `docs/PORTING.md`: a "How to use" header (claim rules per `AGENTS.md` Workflow), the table, and Notes (unclear features, conflicts, questions). Open a PR titled `docs(porting): v1 audit queue`.
7. Write the private source map `PORTING.local.md` at the repo root (gitignored): one block per row id → v1 branches, paths, quirks worth knowing, and any secret-shaped values found (name their shape, never the value). This file is never committed, pushed, quoted or attached.

## Output
The PR plus the local map. Report: row counts per state and priority, the P0 rows, and every open question.

## Constraints
- Read-only on the v1: no fixes, no restructuring, no opinions in rows — facts with sources; port action is a proposal.
- Everything becomes a module; `drop` needs a reason; nothing is silently skipped.
- This repo is public: no v1 branch names, paths, code or secrets in `docs/PORTING.md`, the PR, or any pushed file; describe features at the level the README already uses.
- The local map is the only place v1 details live; if it is lost, re-run this prompt with the same v1 clone.

## Edge cases
- A feature exists only as an idea in docs → a row with state `todo` and size from the docs, not from code.
- Two features are unusable apart → one row, `merge`, note why.
- The v1 has several names for one thing → one row; list the aliases in the local map.
- The backlog would exceed ~40 rows → keep the table, push detail into the local map per row.
