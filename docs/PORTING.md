# Porting queue
> The single queue of v1 features to port into v2 modules (`PROMPTS/AUDIT.md`). One row = one feature = one module = one PR (`PROMPTS/PORT.md`). The private v1 details for each row — sources, quirks — live only in the gitignored `PORTING.local.md`.

## How to use
- Take a row whose state is `todo` and whose `depends on` rows are all `done`; check open PRs, issues and branches first, so no two agents hold one row.
- Claim it before writing code (`AGENTS.md` Workflow): set its state to `in progress (your name, date)` in one commit and push that commit.
- Run `PROMPTS/PORT.md` for the row: its PR sets `in review (#PR)`, its merge sets `done (#PR)`. A row that turns out empty or covered closes as `dropped` or `merged` with the reason in Notes.
- `unclear` rows wait for the human answer in Notes; `duplicate` rows are already covered elsewhere and need no PR.
- New scope gets a new row at the end with the next free id; ids never change.
- `depends on` names row ids; `—` means the core alone. `port (new)` marks a roadmap module with no v1 source (`PROMPTS/PORT.md` still applies; the design goes into `DECISIONS.md` first).
- Priority (a proposal; the master reorders): `P0` modules other modules need, `P1` spine features, `P2` integrations, `P3` the rest. Size: `S`, `M`, `L`.

## Queue
| id | name | what it does | v2 module | depends on | port action | size | priority | state |
|---|---|---|---|---|---|---|---|---|
| R01 | Core spine | Config, append-only event log with redaction, SQLite storage, token auth with a Host guard, module discovery and lifecycle, subprocess runs without a shell, a CLI over the same code | core | — | drop: rebuilt as the v2 core (`PROMPTS/CORE.md`) | L | P0 | duplicate |
| R02 | Locales | English source keys, a Polish pack per module, one lookup function, a check that every module's language packs share one key set; machine translation only drafts, a human commits | locales | — | port | M | P0 | todo |
| R03 | Module manager | Create a module from the template, list, edit, enable and disable from the interfaces, export and import with archive vetting, delete into a trash; an agent may enable only a module that is still a bare template | modules-manager | — | port | M | P0 | todo |
| R04 | Profiles | Named bundles of enabled modules, config overrides and agent sets, switched and switched back in one call, exported and imported; never touches secrets; also holds the human's display name | profiles | R03 | port (new) | M | P0 | todo |
| R05 | Web interface | The WebUI: five places (inbox, work, agents, machine, log), a dashboard home, a command palette, a status bar, one theme, an EN/PL switch, strict security headers, a browser session with CSRF; pages come from the modules | interfaces-web | R02 | port | L | P0 | todo |
| R06 | Terminal interface | The `kombinat` terminal app: greeting, manual, a panel with the same places, palette, keybinds, Markdown rendering, shell shortcuts and hooks; it reads and changes state only through the API | interfaces-tui | R02 | port | L | P0 | todo |
| R07 | Tasks | Tasks, ideas and plans as one type with states (idea, planned, queued, running, done, failed, dropped), threaded comments, full CRUD from the API and the CLI | tasks | — | port | M | P1 | todo |
| R08 | Approvals | The approval queue: read, write and exec permission tiers, approve or reject with a reason, one event per stamp, stamp history per job; exec work waits for a stamp | approvals | R07 | port | M | P1 | todo |
| R09 | Worker | A daemon that claims queued jobs and runs them within parallel limits per agent and per adapter, writes heartbeats, never touches another worker's jobs, and treats agent output as data | worker | R07, R08 | port | L | P1 | todo |
| R10 | Fleet | The agent registry: adapters (prompt on stdin, answer on stdout, empty work dir, minimal env), discovery, write and network probes, permission levels and full-access variants, per-agent tokens with bounded delegation, statistics, the allowance meter, agents shipped by modules, a who-works-now view | fleet | R09 | port | L | P1 | todo |
| R11 | Live terminal | The event log viewer grouped by day with filters, plus a live verbose feed of every event and every job's full context, unfiltered except secret redaction | live-terminal | — | port | M | P1 | todo |
| R12 | Notifications | Desktop notifications at levels (attention, done, error, elevated command), at most one sound per two minutes, a mute switch, spoken notifications, a sound pack fetcher; every notification is an event | notifications | — | port | M | P1 | todo |
| R13 | Memory 5 | Hybrid search (full-text plus vectors) over a configurable file list, redaction before anything is indexed, journals and user memories, a retrieval route that returns framed data, never instructions | memory5 | — | port | L | P1 | todo |
| R14 | Proposals | A proposer that reads capped project digests and writes proposals; the human approves, edits or rejects them with a reason, and the proposer learns from the reasons | proposals | R09, R10 | port | L | P1 | todo |
| R15 | Schedule | Approved proposals spread over the day under per-hour, gap, active-hours, per-day and in-flight limits, with pinned times; it starts in plan mode and sends nothing until the human switches it | schedule | R14 | port | M | P1 | todo |
| R16 | Dispatcher | Picks the agent for each job with a score shown part by part; skips missing programs, keys, cooldowns, subscription windows and caps; retries on another agent; includes the model router | dispatcher | R15 | port | L | P1 | todo |
| R17 | Tokens router | Free-token backends as read-level agents without tools (a local multi-provider endpoint, a keyless public model), marked as sharing prompts, each with its own service install; wrapper APIs land here as adapters | tokens-router | R10 | port | M | P2 | todo |
| R18 | Discord bot | A two-way owner channel: rich embeds and components, owner commands in, notifications out, channels for logs, controls and tickets | discord | R08, R12 | port (new) | L | P2 | todo |
| R19 | Co-op | Agent-to-agent and operator-to-operator messaging between Kombinat instances, shared goals and dashboards, per-user permissions | coop | R18 | port (new) | L | P2 | todo |
| R20 | KUR market | List, search, install and update modules from GitHub repositories with version pins, dependency checks before install, and git as the signature | kur | R03 | port (new) | L | P2 | todo |
| R21 | Ask | One prompt to many agents with the answers side by side; the CLI exits 0 when all answered, 1 on a failure, 3 while some still wait | ask | R10 | port | M | P3 | todo |
| R22 | Evaluations | Sensors first (closing report, commits, the diff), then a tool-less judge from another pool; pass, partial with fixes, or failed; a regulator whose every change is one event with its evidence | evaluations | R16 | port | L | P3 | todo |
| R23 | Workflows | A library of workflows (agent steps, loops, checks, gates), a block editor, live runs, built-in definitions and a CLI; a workflow can be a dispatch target | workflows | R10 | port | L | P3 | todo |
| R24 | Staged schedules | A project in human-approved stages with a coordinator and one agent per stage, stage templates and tools, run in terminals; exists only on an unmerged v1 branch | workflows | R15, R23 | merge | L | P3 | unclear |
| R25 | Policies | Actions declare a risk 0–3, config maps risk to a tier with per-actor exceptions; generalizes the read/write/exec gate | policies | R08 | port (new) | M | P3 | todo |
| R26 | Capabilities | An agent acts only through capabilities registered for it: adapters declare them, probes verify them, an unregistered action is refused | capabilities | R10 | port (new) | M | P3 | todo |
| R27 | Assertions | Pre- and post-conditions on jobs and workflow steps; a false assertion fails the step and marks the run, never the whole system | assertions | R23 | port (new) | M | P3 | todo |
| R28 | Skills | A registry of `SKILL.md` skills: modules ship them, the API lists and serves them to agents byte for byte | skills | R10 | port (new) | M | P3 | todo |
| R29 | Workshop | Coding-agent skills installed on this machine, plus drafts written from past sessions that become skills only when the human saves them; exists only on an unmerged v1 branch | workshop | R28, R30 | port | M | P3 | unclear |
| R30 | Sessions | Coding-agent sessions on this machine, live and past: a list, a conversation reader, a terminal in the browser, and state changes as events and notifications; exists only on an unmerged v1 branch | sessions | R05, R12 | port | L | P3 | todo |
| R31 | MetaCode | The operating system as a layer (Arch first): machine state without root, framed context for agents, actions as proposals from a closed list that a human confirms in a terminal, package log sync | metacode | R08, R10, R14 | port | L | P3 | todo |
| R32 | Voice | A voice agent: microphone, speech recognition, a local model, speech synthesis sentence by sentence; read-only tools; every turn is an event; voice never stamps an approval | voice | R09, R10 | port | L | P3 | todo |
| R33 | Services | User service units for the core (which supervises module daemons): install, status, logs, uninstall, no root; the WSL install note | services | R05, R09 | port | S | P3 | todo |
| R34 | Windows desktop | Start with Windows through WSL, a desktop widget and a tray icon fed by the local API, WSL that lives as long as the app, and a "what stops if WSL shuts down" check; exists only on an unmerged v1 branch | desktop-windows | R33 | port | M | P3 | todo |
| R35 | Local overrides | Personal overrides of tracked config files and the human's display name; exists only on unmerged v1 branches | core | — | drop: the core's `config/NAME.local.toml` layer covers overrides (`DECISIONS.md` 8); the display name moves to R04 | S | P3 | duplicate |
| R36 | Earlier prompt pack | The first draft of the v2 agent prompt pack; exists only on an unmerged v1 branch | — | — | drop: superseded by `PROMPTS/` (`DECISIONS.md` 1) | S | P3 | duplicate |

## Notes
- **Core gaps the first ports will hit.** The core has no storage API for module data, no event subscriptions, no daemon supervision, no pre-run approval hook, no browser session or CSRF, and no module routes beyond `run`. Expect `core-change` issues from R07 (module data), R08 (approval hook), R09 (daemons), R11 and R12 (event subscriptions) and R05 (session, CSRF, pages); the master rules on each (`PROMPTS/MASTER.md` 5).
- **R24, unclear:** the staged schedules overlap R23 (workflows) and R15 (schedule) in v1 already. Question for the humans: merge them into workflows (the proposal), or keep a separate module?
- **R29, unclear:** the workshop manages one coding agent's skills; R28 serves `SKILL.md` skills to every agent. Question: one module, or two that share the format?
- **Branch-only features.** R24, R29, R30, R34, R35 and R36 exist only on unmerged v1 branches. The branch that carries most of them lacks main's workflows, proposer, dispatcher and evaluator, and its WebUI redesign conflicts with main's styles; R05 picks one look for v2.
- **Not found.** No v1 branch holds job-search helpers; if they are wanted, they need a new row.
- **Covered by the core already.** Host-header guard, token bootstrap (mode 0600), append-only log with DB triggers, key-based redaction, WAL storage with forward-only migrations, shell-free subprocess runs; v1 wrote module enable state into tracked manifests, v2 keeps it in the store (`DECISIONS.md` 12).
- **Counts:** 36 rows — `todo` 31, `unclear` 2, `duplicate` 3; by priority `P0` 6, `P1` 10, `P2` 4, `P3` 16.
