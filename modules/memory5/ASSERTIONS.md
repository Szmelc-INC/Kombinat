# Memory 5 — assertions
> Deterministic invariants that gate every state-changing action (`AGENTS.md`, The one law). An action that fails an assertion is rejected, logged as an event, and the core stays up.

- Precondition: `enable` and `start` are rejected unless `kombinat module check` reports `memory5` valid.
- Precondition: every run is rejected (exit 2) unless `KOMBINAT_DATA` is an absolute path; state is never written next to the working directory.
- Precondition: `memory add` and `journal add` are rejected (exit 2) unless the text is non-empty UTF-8 within `memory.max_bytes` or `journal.max_bytes`; a query is rejected unless it is non-empty UTF-8 of at most 4096 bytes.
- Invariant: no file below a directory named `secrets`, no hidden path and no secret-shaped file name is ever read or indexed, whatever `index.files` says; check: `kombinat module run memory5 -- index` lists such paths under `excluded:` and `kombinat module run memory5 -- query WORD` never returns them.
- Invariant: text is redacted before it is stored, chunked, indexed or embedded; check: `grep -a` for a known secret value in every file under `var/modules/memory5/` finds nothing.
- Invariant: every query answer is framed; check: in `kombinat module run memory5 -- query WORD`, every line between `memory5: begin retrieved data` and `memory5: end retrieved data` starts with `[N] source: ` or `> `.
- Invariant: event payloads carry counts, ids and days only, never indexed text, query text or paths; check: `kombinat --json log --tail 20` shows no file content for `memory5.*` actions.
- Invariant: one index run at a time; check: a run while another holds `index.lock` prints `busy` and changes nothing.
- Fallback: on assertion failure — the run exits non-zero (or answers `{"ok":false,...}`), the SQLite transaction is rolled back, no event is written for the rejected action, and the core logs the failed `core.module.run`.
