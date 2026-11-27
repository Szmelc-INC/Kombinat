#!/usr/bin/env bash
# End-to-end smoke test of the built core on a throwaway Kombinat root.
# Usage: tests/smoke.sh [path/to/kombinat]; `make check` runs it after the build.
set -euo pipefail
cd "$(dirname "$0")/.."
bin=$(realpath "${1:-bin/kombinat}")
root=$(mktemp -d)
server_pid=""
cleanup() {
  if [[ -n $server_pid ]]; then kill "$server_pid" 2>/dev/null || true; fi
  rm -rf "$root"
}
trap cleanup EXIT
export KOMBINAT_ROOT=$root NO_COLOR=1

step() { printf '==> %s\n' "$*"; }
fail() {
  printf '✗ %s\n' "$*" >&2
  [[ -f $root/serve.log ]] && sed 's/^/   serve: /' "$root/serve.log" >&2
  exit 1
}

# start_server: serve on a free port; sets $base to the API URL.
start_server() {
  printf '[api]\nport = 0\n' >"$root/config/core.local.toml"
  "$bin" serve >"$root/serve.log" 2>&1 &
  server_pid=$!
  local i
  for ((i = 0; i < 100; i++)); do
    if [[ -s $root/var/run/api.json ]]; then
      base="http://127.0.0.1:$(jq -r .port "$root/var/run/api.json")"
      return 0
    fi
    kill -0 "$server_pid" 2>/dev/null || fail "server exited early"
    sleep 0.1
  done
  fail "server did not record var/run/api.json"
}

stop_server() {
  kill -TERM "$server_pid"
  wait "$server_pid" || fail "server exited non-zero on SIGTERM"
  server_pid=""
  [[ ! -e $root/var/run/api.json ]] || fail "var/run/api.json left behind"
}

# api <method> <path> [json]: call the API; sets $out to the body and $status to the HTTP code.
api() {
  local args=(-sS -o "$root/.body" -w '%{http_code}' -X "$1")
  [[ $# -ge 3 ]] && args+=(-H 'Content-Type: application/json' --data "$3")
  status=$(curl "${args[@]}" "$base$2")
  out=$(cat "$root/.body")
}

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

step "event log: an action via the API, then log --tail 1 shows it"
start_server
api POST /api/events '{"action":"core.smoke","summary":"hello from smoke"}'
[[ $status == 201 && $(jq -r .ok <<<"$out") == true ]] || fail "POST /api/events: $status $out"
expect 0 "$bin" log --tail 1
[[ $out == *"core.smoke"*"hello from smoke"* ]] || fail "log --tail 1 printed: $out"
expect 0 "$bin" --json log --tail 1
[[ $(jq -r '.[0].actor' <<<"$out") == api ]] || fail "log --json: $out"

step "API: foreign Host is refused, unknown route answers the JSON envelope"
status=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Host: evil.example' "$base/api/events")
[[ $status == 403 ]] || fail "foreign Host: $status"
api GET /api/nope
[[ $status == 404 && $(jq -r .ok <<<"$out") == false ]] || fail "unknown route: $status $out"

step "serve stops cleanly on SIGTERM and logs it"
stop_server
expect 0 "$bin" log --tail 1
[[ $out == *"core.api.stop"* ]] || fail "stop not logged: $out"

printf '✓ smoke: all steps passed\n'
