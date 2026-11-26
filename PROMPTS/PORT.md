# Prompt: port one v1 feature as one v2 module
> Paste once per queue row; one agent per row, many agents in parallel. Input: `{row_id}` from `docs/PORTING.md` and `{v1_repo}` — a local clone of the private v1 repo (read-only). Done when the PR merges and the row says Done.

You port exactly one feature: `{row_id}` from `docs/PORTING.md`, into exactly one module. One feature = one module = one PR (`AGENTS.md` Workflow).

## Instructions
1. Claim the row: move it to In progress with your name and date, push before writing any code.
2. Read the feature's v1 sources until you can state its contract: inputs, outputs, config, side effects, interfaces, failure modes. `PORTING.local.md` maps the row to branches and paths; if the map is missing, find the feature in the v1 clone by name and rebuild that one row's map.
3. Copy `templates/module/` to `modules/<name>/` and fill everything: the manifest (`module.json`), `MODULE.md`, `SKILL.md`, `ASSERTIONS.md`, `config.default.toml`, `src/`, `locales/en.json` + `locales/pl.json`, and `ui/` only if the module binds to an interface. The embedded prompts are the module's real docs, not placeholders.
4. Reimplement against the core API only: declarative config, an event for every action, no imports from other modules.
5. Verify: module tests pass; the module runs standalone and inside the mesh via the API, same behavior both ways; `bash tools/lint.sh` passes.
6. Open the PR: title `feat(module): <name>`, body with what and why at the README's public level, the pasted verification output, and the queue row id; move the row to In review. Address `REVIEW.md` findings until none P0/P1 are open; merge per `AGENTS.md`; move the row to Done.

## Constraints
- The diff touches only `modules/<name>/` and its queue row; a need for the core or another module → issue labeled `core-change`, stop that part, note it in the PR.
- The v1 is a read-only reference: no copied code, and no v1 quirk survives without a `DECISIONS.md` entry saying why it must.
- Scope beyond the row → a new queue row, not this PR.
- This repo is public: no v1 branch names, paths or code in the PR; secrets stay in `secrets/`.

## Edge cases
- The feature is half-built in v1 → port the working part; a new queue row covers the rest.
- Two rows turn out to be one feature → one module, both rows → `merged`, note why in the queue.
- The row says `drop` or its sources are dead → row → `dropped` with the reason, no PR.
- The v2 module already exists → reconcile with the master instead of building a second one.
