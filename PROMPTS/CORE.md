# Prompt: build the MkII core
> Paste to one agent with push access to this repo. Input: none — the repo holds prompts and templates only. Done when every phase check below passes on CI and the handoff phase is complete.

You are the core builder. Goal: the smallest working spine that loads, configures and serves exactly one module. Everything beyond it is a module, not core (`AGENTS.md`, The one law).

## Fixed decisions
- One Go binary `kombinat`, stdlib first, no web framework: a single binary is the packaging DNA and keeps the core legible top to bottom.
- Module sources may be Go, PHP or POSIX shell behind the module contract: ported features keep their proven language instead of forcing rewrites.
- SQLite in WAL mode behind a small storage interface: one file, concurrent readers plus one writer, no server process to babysit.
- HTTP JSON API bound to `127.0.0.1` with the port from config, plus one CLI with subcommands: the API is first-class; every UI is a module.
- Layout: `core/` (cmd, api, modules, config, events, store, cli), `modules/`, existing `templates/module/`, `Makefile`, extended `.github/workflows/check.yml`.

## Anchor example
With the core running: `kombinat module enable hello` succeeds, `curl 127.0.0.1:<port>/api/modules/hello` returns its manifest, and `kombinat log --tail 1` shows the enable event.

## Phases
Run in order; start a phase only when the previous check passes; commit after each phase; report after each phase: what exists, how it works in at most 5 lines, the check with its pasted output, what is next.
1. Skeleton: `core/` package layout, `go.mod`, `Makefile` (`build`, `test`, `lint`, `check`), CI extended with `go build`, `go test`, `go vet`, gofmt-clean and the existing `tools/lint.sh`. Check: `make check` green locally and on CI.
2. Config registry: loads core config plus per-module `config.default.toml`, merges defaults, validates types. Check: an invalid value exits 1 naming file and key; `kombinat config show` prints the merged config as JSON.
3. Event log: append-only `events` storage, one API route, `kombinat log` with `--tail`. Check: an action via API, then `kombinat log --tail 1` shows it.
4. Module loader: discovers `modules/*/module.json`, validates manifests, lifecycle `enable|disable|start|stop`, per-module config. Check: copy `templates/module/` to `modules/hello/`, fill the manifest and a minimal CLI entrypoint; `kombinat module ls` lists it; after `disable` its route answers 404 and the event log says so.
5. API surface: `/api/modules`, `/api/modules/<name>`, `/api/events`, `/api/config`; an auth token from `secrets/` for non-browser clients. Check: each route answers with the right status on curl; no token means 401.
6. Module runtime: run a module entrypoint in each supported language (Go, PHP, shell), capture stdout, exit code and duration into the event log. Check: the `hello` module runs via API and its result appears in `kombinat log`.
7. Handoff: the README "How agents build it" section matches reality, `AGENTS.md` Commands carries the real `make` targets, every deviation from this brief has a `DECISIONS.md` entry. Check: a fresh agent, given only `README.md` and `AGENTS.md`, builds the core and runs the anchor example in one session.

## Constraints
- Core scope = router, loader, config, events, store, CLI; anything else becomes a module, and any scope deviation gets a `DECISIONS.md` entry.
- No TODO stubs and no scaffolding ahead of the phases: one thin vertical slice per phase.
- Tests ship with every phase; failures are reported with output, never hidden.
- No new dependency without a `DECISIONS.md` entry saying why the standard library cannot do it.

## Edge cases
- Port busy → pick a free port, record it in config, warn once.
- Invalid module manifest → skip that module, log the reason, core stays up.
- Unknown subcommand or flag → usage to stderr, exit 2.
- PHP missing on the host → PHP module jobs report `runtime-missing`; the core is unaffected.
