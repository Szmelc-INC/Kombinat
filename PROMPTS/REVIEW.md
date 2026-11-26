# Prompt: review a MkII PR
> Paste to a second agent, or run yourself read-only. Input: PR `{n}`. Done when the findings and the verdict are posted on the PR.

Review PR `{n}` read-only. Judge against, in order:
1. `AGENTS.md` law: the diff touches only the one module directory and its queue row; no core edits without a `core-change` decision; no cross-module imports — only the central API and events.
2. Module format: every `templates/module/` file present and filled — manifest fields complete, `MODULE.md`/`SKILL.md`/`ASSERTIONS.md` are real docs, `config.default.toml` covers every key the code reads, `locales/en.json` and `locales/pl.json` cover the same keys.
3. Verification: the author's claimed checks actually run — rerun the module tests and the standalone-versus-mesh check; the output is pasted, not asserted.
4. Security: no secrets, correct shell/SQL/HTML/URL escaping, `127.0.0.1` binds only, untrusted content treated as data.
5. Repo hygiene: a Conventional Commit title, the queue row state is correct, a `DECISIONS.md` entry for every deviation, and no private v1 material anywhere in the public diff.

## Output
Findings, most severe first, each as `[P0..P3] imperative title — path:line` with one sentence on the scenario and the fix; then the verdict on its own line: `merge`, `fix first` or `reject`, with one line why. No findings → post `No findings. merge`.

## Constraints
- Read-only: findings and verdict only; you fix nothing and merge nothing.
- P0 = breaks the one law, loses data or secrets, or presents unverified work as verified; P1 = must fix before merge; P2 and P3 can merge.
- Judge against the rules and the module contract, not taste; a style nit that no rule owns is not a finding.
