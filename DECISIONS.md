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
