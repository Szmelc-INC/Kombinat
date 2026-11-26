#!/usr/bin/env bash
# Structure checks: the prompt suite, the module template, and public-doc hygiene.
# Run before every push; CI runs it plus shellcheck on every push and PR (`AGENTS.md` Commands).
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0

# 1. Required files exist.
required=(
  README.md AGENTS.md CLAUDE.md GEMINI.md DECISIONS.md LICENSE
  docs/CONVENTIONS.md
  PROMPTS/README.md PROMPTS/CORE.md PROMPTS/AUDIT.md PROMPTS/PORT.md PROMPTS/REVIEW.md PROMPTS/MASTER.md
  templates/module/module.json templates/module/MODULE.md templates/module/SKILL.md
  templates/module/ASSERTIONS.md templates/module/config.default.toml
  templates/module/locales/en.json templates/module/locales/pl.json
  templates/module/src templates/module/ui
  .github/workflows/check.yml .github/PULL_REQUEST_TEMPLATE.md
)
for f in "${required[@]}"; do
  if [[ ! -e $f ]]; then echo "lint: missing: $f"; fail=1; fi
done

# 2. Tool imports stay one-line imports of AGENTS.md.
for f in CLAUDE.md GEMINI.md; do
  if [[ $(cat "$f") != '@AGENTS.md' ]]; then echo "lint: $f must contain exactly: @AGENTS.md"; fail=1; fi
done

# 3. Prompts: every file but the map states its done-condition and carries constraints.
for f in PROMPTS/*.md; do
  case $f in PROMPTS/README.md) continue ;; esac
  grep -q '^> .*Done' "$f" || { echo "lint: $f: intro line must state when it is done"; fail=1; }
  grep -q '^## Constraints' "$f" || { echo "lint: $f: missing section: ## Constraints"; fail=1; }
done

# 4. Module template: valid JSON with the required manifest keys.
json_ok() { # json_ok <file> [key...]
  python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if all(k in d for k in sys.argv[2:]) else 1)' "$@"
}
json_ok templates/module/module.json name display_name version description depends provides entrypoints locales \
  || { echo "lint: templates/module/module.json: invalid JSON or missing keys"; fail=1; }
json_ok templates/module/locales/en.json || { echo "lint: templates/module/locales/en.json: invalid JSON"; fail=1; }
json_ok templates/module/locales/pl.json || { echo "lint: templates/module/locales/pl.json: invalid JSON"; fail=1; }

# 5. Public docs: no unresolved placeholders, no backslash line breaks.
public_docs=(README.md AGENTS.md DECISIONS.md docs/CONVENTIONS.md)
placeholder_re='<[a-zA-Z][a-zA-Z0-9._-]*>|\{[a-zA-Z][a-zA-Z0-9._-]*\}'
for f in "${public_docs[@]}"; do
  if grep -nEq "$placeholder_re" "$f"; then
    echo "lint: $f: unresolved placeholder (allowed only in PROMPTS/ and templates/):"
    grep -nE "$placeholder_re" "$f"
    fail=1
  fi
  if grep -q '\\$' "$f"; then
    echo "lint: $f: backslash line break; rewrite the line without it"
    fail=1
  fi
done

# 6. Hygiene: secrets and the private porting map stay untracked.
grep -q '^secrets/$' .gitignore || { echo "lint: .gitignore must ignore secrets/"; fail=1; }
grep -q '^PORTING.local.md$' .gitignore || { echo "lint: .gitignore must ignore PORTING.local.md"; fail=1; }

exit "$fail"
