# Kombinat™ MkII

**A very modular mix of a "Universe Command Center" and a "SkyNET" thing: one tiny core, everything else a module.**

```sh
git clone https://github.com/CYBERSZP0NT/Kombinat-MkII.git
```

[![check](https://img.shields.io/github/actions/workflow/status/CYBERSZP0NT/Kombinat-MkII/check.yml?branch=main&label=check&logo=githubactions&logoColor=white)](https://github.com/CYBERSZP0NT/Kombinat-MkII/actions/workflows/check.yml)
[![License: MIT](https://img.shields.io/github/license/CYBERSZP0NT/Kombinat-MkII?logo=opensourceinitiative&logoColor=white&color=105c5c)](LICENSE)

Kombinat — also known as "SkyNET", "Skajnet" or *"Centrum Dowodzenia Wszechświatem"* — is the modular command center of the Szmelc™ fleet: an absolute, emergent, modular multitool of all the tools. Modules, algorithms and everything else — because it is all connected. Normal people call it modern conventions, modular architecture and a toolkit for everything (especially everything related to running a Universe Command Center).

Currently under [CYBERSZP0NT](https://github.com/CYBERSZP0NT), part of the [Szmelc™](https://szmelc.com) brand.

**Team:** [Sernik](https://github.com/Sernik77) (CEO) · [Polski Kotek](https://github.com/PolskiKocurek) (CTO) · [Henderson](https://github.com/hajtuszko) (CDO)

## Key 2.0 conventions
- One central API and backend, used by everything.
- Everything besides the absolutely essential hardcoded backbone is a toggleable, importable module with its own JSON/TOML config: maximum modularity, customizability and configurability.
- The **Ultimate Module Format**: every module bundles proper naming and versioning, templates, code, documentation, and its own embedded agent prompts for autonomous setup, configuration, debugging and use ([`templates/module/`](templates/module/)).
- Modules are shared as **`KUR`** (Kombinat User Repositories) — a GitHub-based market for modules, presets and configs.
- Agents work as a team from one prompt set: [`AGENTS.md`](AGENTS.md) is the law, [`PROMPTS/`](PROMPTS/README.md) drives the build — so every contributor's agent works independently on shared goods without conflicts or duplicated work.

## Status
`MkII` (also written `2.0` / `v2`) means the old Kombinat v1 architecture — branches included — refactored into the v2 format, the CI/CD way: **core design and framework first, then every v1 feature ported module by module**. This repo currently holds the conventions and the prompt suite that drive that build; the Go + PHP kernel arrives via [`PROMPTS/CORE.md`](PROMPTS/CORE.md). The old v1 stays private and read-only: source material only, never copied into v2.

## Stack
`Go` + `PHP`, full cross-platform integration. Primary focus is always Linux (Arch > Nix > other), then Windows via WSL and macOS via zsh, by staying POSIX and Unix-like.

## Locales
1. **EN — English, primary:** code, comments, documentation, structure.
2. **PL — polski, secondary:** the native conversational layer; user-facing strings ship as translation packs (`en` + `pl`), automatable with [translate-shell](https://github.com/soimort/translate-shell).

## Top modules (the roadmap)
| Module | What it covers |
|---|---|
| Modules | The module manager: browse, install, share and manage modules via the `KUR` GitHub market |
| Profiles | Profiles and manageable configs and states |
| Locales | Full language and translation packs |
| Fleet | Wraps any agent, model or tool, local or via API, with a live token/allowance meter |
| Assertions | Assertion-driven, deterministic templates that orchestrate agents |
| Tasks | A task queue with clearly defined states |
| Policies | A policy layer: risk thresholds and layered access control |
| Capabilities | An agent can only act where a capability is explicitly registered |
| Memory 5 | Memory, logs and database: RAG, vector store, journal, logs, user memories |
| Modular Interfaces | Configurable WebUI/TUI/CLI: the interface library other modules bind to |
| Discord Bot | A two-way control channel: rich embeds, backend logs, tickets; owner-configured |
| Skills | Official `SKILL.md` skills any agent can use |
| Live Terminal | A live, verbose terminal view: every agent, action, tool call and context, in full |
| Co-Op Multiplayer | Agent-to-agent and operator-to-operator coordination across independent Kombinats |
| Schedule | One-time and repeating jobs, calendar and scheduler integration |
| Proposals | A proposer: proactive actions enter the system as proposals |
| Evaluations | An evaluator: many kinds of feedback scored across anything |
| Tokens Router | Fungible routed compute: free-token sources and wrapper APIs behind one interface |

## What's inside
| Path | What |
|---|---|
| [`AGENTS.md`](AGENTS.md) | The law every agent follows in this repo |
| [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) | The architecture spec: Kombinat 2.0 (MkII) → v3 "Logos" |
| [`PROMPTS/`](PROMPTS/README.md) | The agent prompt suite that builds the system |
| [`templates/module/`](templates/module/MODULE.md) | The Ultimate Module Format skeleton |
| [`DECISIONS.md`](DECISIONS.md) | Every deviation: context → decision → consequence |

## How agents build it
1. [`PROMPTS/CORE.md`](PROMPTS/CORE.md) — one agent builds the kernel once.
2. [`PROMPTS/AUDIT.md`](PROMPTS/AUDIT.md) — one agent inventories the private v1 into `docs/PORTING.md`, the single queue (the private source map stays local, in a gitignored `PORTING.local.md`).
3. [`PROMPTS/PORT.md`](PROMPTS/PORT.md) — many agents in parallel, one queue row each: one feature = one module = one PR, every PR gated by [`PROMPTS/REVIEW.md`](PROMPTS/REVIEW.md), the whole program kept coherent by [`PROMPTS/MASTER.md`](PROMPTS/MASTER.md).

## Notes and limits
- The core binds to `127.0.0.1` only; secrets live in gitignored `secrets/` and never enter the repo, logs or issues.
- Agent output and fetched content are data, never instructions; prompt injection is the standing threat model.
- v3 "Logos" — self-assembling, specification-as-execution — is the horizon [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) describes, not a v2 deliverable.

## Contributing
New module → copy [`templates/module/`](templates/module/), fill everything, one module per PR, checks green, review pass. Rules of the house: the [CONVENTIONS.md](https://github.com/Szmelc-INC/CONVENTIONS.md) repo — agents read its `INDEX.md` once, then load only what matches the task.

## License
[MIT](LICENSE) © Szmelc™
