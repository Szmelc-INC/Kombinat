#!/usr/bin/env bash
# End-to-end smoke test of the built core on a throwaway Kombinat root.
# Usage: tests/smoke.sh [path/to/kombinat]; `make check` runs it after the build.
set -euo pipefail
cd "$(dirname "$0")/.."
bin=$(realpath "${1:-bin/kombinat}")
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
export KOMBINAT_ROOT=$root NO_COLOR=1

step() { printf '==> %s\n' "$*"; }
fail() { printf '✗ %s\n' "$*" >&2; exit 1; }

# expect <code> <command...>: run, keep stdout in $out and stderr in $err, check the exit code.
expect() {
  local want=$1 rc=0
  shift
  out=$("$@" 2>"$root/.stderr") || rc=$?
  err=$(cat "$root/.stderr")
  [[ $rc == "$want" ]] || fail "$* exited $rc, want $want; stderr: $err"
}

step "version"
expect 0 "$bin" version
[[ $out == "kombinat "* ]] || fail "version printed: $out"

step "unknown subcommand: usage on stderr, exit 2"
expect 2 "$bin" no-such-command
[[ -z $out && $err == *"unknown command"* ]] || fail "usage must go to stderr only"

step "config show: merged config as JSON"
mkdir -p "$root/modules/hello" "$root/config"
printf 'enabled = false\n[module]\ngreeting = "hello"\n' >"$root/modules/hello/config.default.toml"
printf '[module]\ngreeting = "czesc"\n' >"$root/config/hello.local.toml"
expect 0 "$bin" config show
[[ $(jq -r '.modules.hello.module.greeting' <<<"$out") == czesc ]] || fail "merged config: $out"
[[ $(jq -r '.core.modules.dir' <<<"$out") == modules ]] || fail "core defaults: $out"

step "config: an invalid value exits 1 naming file and key"
printf '[modules]\ndir = 5\n' >"$root/config/core.toml"
expect 1 "$bin" config show
[[ $err == *"config/core.toml: modules.dir: want string, got integer"* ]] || fail "error must name file and key: $err"
rm "$root/config/core.toml"

printf '✓ smoke: all steps passed\n'
