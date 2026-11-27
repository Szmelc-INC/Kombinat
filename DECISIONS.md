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
