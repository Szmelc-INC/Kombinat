#!/usr/bin/env bash
# Hello module tests: standalone, then inside the mesh (enabled and disabled).
# Run by `make modules-test` with KOMBINAT_BIN set to the built core.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
bin=${KOMBINAT_BIN:?KOMBINAT_BIN must point at the built kombinat binary}
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
export KOMBINAT_ROOT=$root NO_COLOR=1
fail() { printf '✗ hello: %s\n' "$*" >&2; exit 1; }

# Standalone: same file, no core.
[[ $(sh "$here/src/cli.sh") == "Hello, world!" ]] || fail "standalone default"
[[ $(sh "$here/src/cli.sh" "Ala") == "Hello, Ala!" ]] || fail "standalone name"

# Mesh: a throwaway root with only this module.
mkdir -p "$root/modules"
cp -R "$here" "$root/modules/hello"
"$bin" module check >/dev/null 2>&1 || fail "module check"
if "$bin" module run hello >/dev/null 2>&1; then fail "a disabled module must not run"; fi
"$bin" module enable hello >/dev/null 2>&1
[[ $("$bin" module run hello Kombinat) == "Hello, Kombinat!" ]] || fail "mesh run"
log=$("$bin" --json log --tail 2)
[[ $(jq -r '[.[].action] | join(",")' <<<"$log") == "hello.greeted,core.module.run" ]] || fail "events: $log"
[[ $(jq -r '.[1].payload.events' <<<"$log") == 1 ]] || fail "run event must count the module event: $log"
mkdir -p "$root/config"
printf '[module]\nname = "Szmelc"\n' >"$root/config/hello.local.toml"
[[ $("$bin" module run hello) == "Hello, Szmelc!" ]] || fail "config name"
"$bin" module disable hello >/dev/null 2>&1
if "$bin" module run hello >/dev/null 2>&1; then fail "a disabled module must not run"; fi
printf '✓ hello: standalone and mesh tests passed\n'
