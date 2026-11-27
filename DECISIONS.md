# Decisions
Every deviation from `AGENTS.md`, `docs/CONVENTIONS.md` or a `PROMPTS/` brief — and every choice a brief left open — lands here in the same change that needs it. Format: context → decision → consequence.

## 1. The checked-in prompt suite governs
- Context: an earlier, longer prompt pack drafted alongside the private v1 describes a different repo layout (its own bootstrap step, JSON-only config, a different module file set). Agents may receive it pasted next to `PROMPTS/`.
- Decision: `AGENTS.md` and `PROMPTS/` in this repo govern; `tools/lint.sh` enforces their module format. The earlier pack is private reference only: its module queue, dependency order and done checks feed `PROMPTS/AUDIT.md`, and its core details fill gaps `PROMPTS/CORE.md` leaves open, each such fill recorded here.
- Consequence: no file from the earlier pack is copied into this public repo; where the two disagree, this repo wins without a further entry.

## 2. Bootstrap files the lint already required
- Context: `tools/lint.sh` requires `.github/workflows/check.yml`, `.github/PULL_REQUEST_TEMPLATE.md` and a `.gitignore` that ignores `secrets/` and `PORTING.local.md`; the first commits did not track them, so `main` failed its own lint and CI never ran.
- Decision: add them, plus `.gitattributes` (`* text=auto eol=lf`) and `.editorconfig`, so Windows checkouts get LF files that bash in WSL can run.
- Consequence: CI runs `shellcheck tools/*.sh` and `bash tools/lint.sh` on every push and pull request; the core phases extend the same `check` job.

## 3. Review gate and merge mode
- Context: `AGENTS.md` asks for a second agent or a human to run `PROMPTS/REVIEW.md`; `PROMPTS/MASTER.md` says the acting agent never merges its own PR unasked. The owner asked agents to review and merge their own PRs once CI is green, without spawning extra agents, using merge commits by default.
- Decision: the acting agent runs `PROMPTS/REVIEW.md` read-only on its own PR, posts the findings and the verdict as a PR comment, and merges with a merge commit (`gh pr merge --merge`) when CI is green and no P0/P1 is open. A rebase merge needs the owner's acknowledgement first, a squash merge the owner's explicit confirmation; no admin bypass ever.
- Consequence: every merged PR carries its review comment; a human or a second agent can still review any PR and their P0/P1 findings block the merge.

## 4. Core Go layout
- Context: `PROMPTS/CORE.md` fixes the `core/` package layout but not where `go.mod` lives or how packages arrive.
- Decision: one Go module at the repo root (`github.com/Szmelc-INC/Kombinat`); the binary is `core/cmd/kombinat`, each core package (`cli`, `config`, `events`, `store`, `modules`, `api`) lands with the phase that needs it, never earlier. `CGO_ENABLED=0`, so the binary stays static.
- Consequence: `go build ./...` and `go test ./...` cover the whole tree from the root; no empty packages wait ahead of their phase.

## 5. CLI conventions in the core CLI
- Context: the owner's CLI rules ask for colored per-command help, aligned tables, `--no-color` and `NO_COLOR`, `--json`, exit codes 0/1/2 and `completion` for bash, zsh and fish with installed files.
- Decision: the core CLI is one command registry; help and completion scripts are generated from it. The generated scripts are committed under `completions/` (`make completions`), and a Go test fails when they drift from the registry.
- Consequence: every new subcommand updates `completions/` in the same change, or `make check` fails.

## 6. TUI and packaging come after the core
- Context: the owner's project rules ask any CLI project for a Go TUI (bubbletea, lipgloss, bubbles, glamour), an Arch `PKGBUILD` and a `flake.nix`; `PROMPTS/CORE.md` keeps the core to router, loader, config, events, store and CLI, stdlib first.
- Decision: the TUI is a module (an interfaces module that reads and changes state only through the API), built after the core handoff; the `PKGBUILD` and `flake.nix` land in one packaging change right after phase 7, with their CI build steps.
- Consequence: the core takes no Charm dependency; until the packaging change merges, `make build` is the only install path.

## 7. TOML parser dependency
- Context: module defaults are `config.default.toml`; Go's standard library has no TOML parser.
- Decision: depend on `github.com/BurntSushi/toml` (MIT, no transitive dependencies), used only inside `core/config` behind one parse function.
- Consequence: exit plan — swap the parse function for another parser or a hand-written TOML subset without touching callers; `go.sum` pins the version.

## 8. Config layers, root and validation
- Context: `PROMPTS/CORE.md` asks for core config plus per-module defaults, merged and type-validated, without fixing file locations.
- Decision: the Kombinat root is `KOMBINAT_ROOT`, else the working directory. Layers, later wins: built-in `core/config/core.default.toml` or `modules/NAME/config.default.toml`, then `config/NAME.toml` (tracked, team), then `config/NAME.local.toml` (gitignored, personal). The defaults are the schema: an override key must exist there with the same TOML type; tables merge, arrays and scalars replace. A core problem stops the command (exit 1, file and key named); a module problem leaves only that module out.
- Consequence: no environment or flag layer per key yet; one arrives only when two real users need it. `kombinat config show` prints exactly what the core and modules see.

## 9. SQLite driver dependency
- Context: `PROMPTS/CORE.md` fixes SQLite in WAL mode; Go's standard library has `database/sql` but no SQLite driver, and the core builds with `CGO_ENABLED=0`.
- Decision: `modernc.org/sqlite`, a pure-Go driver (the Go conventions name it), used only inside `core/store`. Migrations only move forward (`PRAGMA user_version`); a database newer than the core is refused, not touched. Triggers make `events` append-only at the database level.
- Consequence: the binary stays static and cross-compiles for linux, darwin, windows and freebsd; exit plan — any `database/sql` SQLite driver drops in behind `store.Open`.

## 10. API envelope, browser guard and port fallback
- Context: `PROMPTS/CORE.md` asks for an HTTP JSON API on `127.0.0.1` and says a busy port is recorded in config; it leaves the response shape and browser threats open, and auth arrives only in phase 5.
- Decision: every response is `{"ok":true,"data":...}` or `{"ok":false,"error":"..."}`. Before any route runs, the API refuses a foreign `Host` header (DNS rebinding), a foreign `Origin` header, and any state-changing request without `Content-Type: application/json` (no simple cross-site form posts); request bodies are strict JSON capped at 1 MiB. A busy port falls back to a free one, logged as `core.api.port_fallback` and recorded with the PID in the machine-written `var/run/api.json`, not in a human-edited TOML file, so comments in personal config are never rewritten.
- Consequence: clients find the live port in `var/run/api.json`; `api.port = 0` always picks a free port, which tests use. The default port is 2138: 2137 is commonly taken on the team's machines.

## 11. One code path for CLI and API
- Context: the CLI and the API must not grow two implementations of one action.
- Decision: both call the same core packages (`config`, `store`, `events`, later the loader) on the same SQLite file; the CLI opens the store directly and works without a running server, WAL keeps CLI and server consistent. CLI actions log the actor `cli`, API actions `api` until phase 5 names token actors.
- Consequence: `kombinat log` and `GET /api/events` always agree; a CLI-only command is a bug unless it is local plumbing (`serve`, `completion`, `help`, `version`).

## 12. Module lifecycle semantics
- Context: `PROMPTS/CORE.md` names the lifecycle `enable|disable|start|stop` without defining the four states; the template's `config.default.toml` carries `enabled = false` and says runtime values come from the core store.
- Decision: `enable` turns a module on and starts it; `disable` stops it and turns it off; `stop` and `start` toggle only whether it runs, and `kombinat serve` starts every enabled module again at boot. The state lives in the `module_state` table, written in one transaction with its event; until the first lifecycle call, the module's `enabled` config key decides (`module ls` shows the source). Enabling needs every dependency enabled; disabling refuses while an enabled module depends on it. A call that changes nothing writes nothing.
- Consequence: a module's routes are live only while it is valid, enabled and running; otherwise they answer 404 like an absent route.

## 13. Entrypoint files and manifest validation
- Context: the template's `entrypoints` are booleans (`api`, `cli`, `ui`, `worker`) and say nothing about files or languages.
- Decision: a declared entrypoint is exactly one of `src/KIND.sh` (POSIX shell), `src/KIND.php` (PHP) or `src/KIND/main.go` (Go); a file for an undeclared kind is an error. The loader also requires strict manifest JSON with every key and no unknown one, a name equal to the directory, SemVer, `provides.api` and `provides.events` under the module's namespace, `provides.config` keys present in the defaults, the required files, a `SKILL.md` name equal to the module, and locales whose keys all start with `module.NAME.` and match across languages.
- Consequence: `kombinat module check` (and a Go test over the repo's `modules/`) is the module lint; an invalid module is listed with its reasons, logged once per server start as `core.module.invalid`, and never enabled.

## 14. Discovery at server start
- Context: the server could rescan `modules/` on every request or once.
- Decision: `kombinat serve` discovers modules once at start; lifecycle state is read from the store on every request, so CLI `enable`, `disable`, `start` and `stop` take effect on a running server at once.
- Consequence: a newly added or edited module directory needs a server restart; reloading without a restart becomes the modules manager's job.

## 15. API tokens from secrets/
- Context: `PROMPTS/CORE.md` asks for an auth token from `secrets/` for non-browser clients, with 401 when it is missing.
- Decision: `kombinat serve` creates `secrets/api_token` (32 random bytes, hex, mode 0600) when it is missing and prints only its path. Every API route needs `Authorization: Bearer TOKEN`; `secrets/api_token` acts as actor `api`, and each `secrets/token-ACTOR` file acts as `ACTOR`, so events name who did what. Tokens are at least 32 characters, unique, compared in constant time, and read once at server start; the event log records actor names, never tokens.
- Consequence: rotating a token means editing the file and restarting the server. Browser sessions with CSRF tokens are not in the core yet: the first web interface module asks for them through a `core-change` issue, and until then the browser guard (decision 10) plus the bearer token keep browsers out.

## 16. The API surface
- Context: phase 5 names `/api/modules`, `/api/modules/NAME`, `/api/events` and `/api/config` without methods or status codes.
- Decision: `GET /api/config` (merged config, secret-shaped keys redacted), `GET` and `POST /api/events` (a client may not write the reserved `core.*` namespace: 403, so every `core.*` event comes from the core itself), `GET /api/modules` (every module, invalid ones included, with state), `GET /api/modules/NAME` (a running module; 404 otherwise) and `POST /api/modules/NAME/ACTION` for `enable`, `disable`, `start` and `stop` (404 unknown module or action, 409 conflict, 422 invalid module). `api.Routes` lists them, and the tests walk that list.
- Consequence: the CLI lifecycle commands and the API call the same `modules.Manager` methods; a new route lands in `api.Routes` and its tests in the same change.

## 17. The module runtime contract
- Context: phase 6 asks the core to run a module entrypoint in Go, PHP or shell and log stdout, exit code and duration, without fixing how a module receives input or config.
- Decision: `POST /api/modules/NAME/run` and `kombinat module run NAME` run one `cli` or `api` entrypoint of a running module. No shell sits in between: argv is the interpreter (`sh`, `php -f`) or the built Go binary plus the request's args. The child runs in the module directory with a minimal environment: `PATH`, `HOME`, `LANG`, `LC_ALL`, `TMPDIR`, `TZ`, plus `KOMBINAT_MODULE`, `KOMBINAT_CONFIG` (the merged module config as JSON) and one `KOMBINAT_CFG_KEY` variable per config key (`module.name` becomes `KOMBINAT_CFG_MODULE_NAME`). The core's other variables, tokens included, never reach it. Stdin carries the request's `stdin`, at most 1 MiB. Output is capped (stdout 1 MiB, stderr the last 64 KiB). `runtime.timeout_seconds` (default 30) kills the whole process group. A missing interpreter reports `runtime-missing`, and the core is unaffected. Go entrypoints build with `CGO_ENABLED=0` from the module's own `go.mod` into `var/cache/modules`. Every run, whatever its outcome, logs one `core.module.run` event with the status, exit code, duration and the first 4 KiB of stdout and 2 KiB of stderr.
- Consequence: modules stay standalone (the same file runs by hand), and shell modules read config without a JSON parser. Run output enters the append-only log, where only secret-shaped payload keys are redacted, so a module must never print a secret. Long-running `worker` entrypoints are not supervised by the core yet: the first module that needs a daemon asks for it with a `core-change` issue.

## 18. No approval gate in the core yet
- Context: the earlier prompt pack (decision 1) wanted command-running routes to fail closed until an approvals module exists; `PROMPTS/CORE.md` asks for no such gate.
- Decision: running an entrypoint of a module the operator enabled needs only a valid token. Approvals, policies and capabilities stay modules from the porting queue; the first of them proposes the smallest core hook (a pre-run check) in a `core-change` issue.
- Consequence: until that hook lands, every token holder may run every running module; tokens therefore stay local (`secrets/`, mode 0600) and the API stays on `127.0.0.1`.

## 19. Module data and module events
- Context: a one-shot run (decision 17) had no place for module data, so state would land in the tracked module directory, and no way to log its own actions, though `PROMPTS/PORT.md` asks for an event for every action.
- Decision: every run gets `KOMBINAT_DATA`, the module's private directory `var/modules/NAME` (mode 0700, kept across runs), and `KOMBINAT_EVENTS`, a fresh file for this run. After the run the core appends each line as one event if it is a strict JSON object with `action`, `subject`, `summary` and `payload`, its action is inside the `NAME.*` namespace, and it passes the event log's own checks: redaction, at most 100 events per run. The run's actor is the actor of these events. A rejected line is listed with its reason in the run result and in the `core.module.run` payload, which also counts the accepted events; module events precede that run event in the log.
- Consequence: a module keeps its state in files or its own SQLite database under `KOMBINAT_DATA`, never in its source tree. It still cannot read the event log or call the API; that need goes to a `core-change` issue. Run standalone, a module treats an unset `KOMBINAT_EVENTS` as "do not log".

## 20. Module tests in make check
- Context: `PROMPTS/PORT.md` requires module tests to pass, but `make check` ran none, so each porter would have edited the `Makefile` or CI.
- Decision: a module's tests are `modules/NAME/tests/*.sh`, run by `make modules-test` (part of `make check`) with `KOMBINAT_BIN` pointing at the built core; a test makes its own throwaway root. A module with its own `go.mod` also gets `go vet` and `go test` in its directory. Shell sources and tests of every module are shellchecked; CI provides PHP with SQLite.
- Consequence: porting a module never touches the `Makefile` or CI; `modules/hello/tests/hello.sh` is the example.

## 21. Tasks module choices
- Context: porting the tasks row (queue R07) left open how states move, how comments thread, what delete does, and which calls log events.
- Decision: one `tasks` type for tasks, ideas and plans with seven states, and any state may follow any other, as in the source feature. Comments live in their own table, threaded inside one task through `reply_to`, nesting capped by `comments.max_depth` (32). Delete is refused while child tasks or replies exist, and its event carries the removed task and its comments. Reads (`list`, `show`) emit no `tasks.*` event; `core.module.run` already logs them. State is one SQLite file in `KOMBINAT_DATA`; the code mirrors `config.default.toml` for standalone runs, and the module test checks that both agree. `owner` and comment `author` are labels the caller sets; the core does not pass the caller's identity to modules yet.
- Consequence: a future worker or approvals module guards the transitions it cares about itself; the log alone can rebuild a deleted task; verified ownership needs the core to pass the actor (a `core-change` proposal).
