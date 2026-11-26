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
      # The token goes to curl through a header file, never on a command line.
      printf 'Authorization: Bearer %s\n' "$(cat "$root/secrets/api_token")" >"$root/.auth"
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

# api <method> <path> [json]: call the API with the token; sets $out to the body and $status to the HTTP code.
api() {
  local args=(-sS -o "$root/.body" -w '%{http_code}' -H "@$root/.auth" -X "$1")
  [[ $# -ge 3 ]] && args+=(-H 'Content-Type: application/json' --data "$3")
  status=$(curl "${args[@]}" "$base$2")
  out=$(cat "$root/.body")
}

# api_anon <method> <path> [json]: the same call without a token.
api_anon() {
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
rm "$root/config/core.toml" "$root/config/hello.local.toml"

step "event log: an action via the API, then log --tail 1 shows it"
start_server
api POST /api/events '{"action":"core.smoke","summary":"hello from smoke"}'
[[ $status == 201 && $(jq -r .ok <<<"$out") == true ]] || fail "POST /api/events: $status $out"
expect 0 "$bin" log --tail 1
[[ $out == *"core.smoke"*"hello from smoke"* ]] || fail "log --tail 1 printed: $out"
expect 0 "$bin" --json log --tail 1
[[ $(jq -r '.[0].actor' <<<"$out") == api ]] || fail "log --json: $out"

step "API: foreign Host is refused, unknown route answers the JSON envelope"
status=$(curl -sS -o /dev/null -w '%{http_code}' -H "@$root/.auth" -H 'Host: evil.example' "$base/api/events")
[[ $status == 403 ]] || fail "foreign Host: $status"
api GET /api/nope
[[ $status == 404 && $(jq -r .ok <<<"$out") == false ]] || fail "unknown route: $status $out"

step "serve stops cleanly on SIGTERM and logs it"
stop_server
expect 0 "$bin" log --tail 1
[[ $out == *"core.api.stop"* ]] || fail "stop not logged: $out"

step "modules: the repo's hello module is discovered, valid and disabled by default"
rm -rf "$root/modules/hello"
cp -R modules/hello "$root/modules/hello"
expect 0 "$bin" module check
expect 0 "$bin" module ls
[[ $out == *hello*0.1.0*disabled* ]] || fail "module ls: $out"

step "an invalid manifest is skipped with its reason; the core stays up"
mkdir -p "$root/modules/broken"
printf '{"name": "broken"}\n' >"$root/modules/broken/module.json"
expect 1 "$bin" module check
[[ $err == *"broken"*"missing key"* ]] || fail "module check: $err"
start_server
expect 0 "$bin" log --tail 20
[[ $out == *"core.module.invalid"*"broken"* ]] || fail "invalid module not logged: $out"

step "enable: the module route answers its manifest; disable: 404 and the log says so"
api GET /api/modules/hello
[[ $status == 404 ]] || fail "disabled module route: $status $out"
expect 0 "$bin" module enable hello
api GET /api/modules/hello
[[ $status == 200 && $(jq -r .data.manifest.name <<<"$out") == hello && $(jq -r .data.status <<<"$out") == running ]] || fail "enabled module route: $status $out"
expect 0 "$bin" module stop hello
api GET /api/modules/hello
[[ $status == 404 ]] || fail "stopped module route: $status"
expect 0 "$bin" module start hello
expect 0 "$bin" module disable hello
api GET /api/modules/hello
[[ $status == 404 ]] || fail "disabled module route: $status $out"
expect 0 "$bin" log --tail 1
[[ $out == *"core.module.disable"*"hello"* ]] || fail "disable not logged: $out"
expect 1 "$bin" module enable broken

step "API surface: every route answers the right status; no token means 401"
[[ $(stat -c %a "$root/secrets/api_token") == 600 ]] || fail "secrets/api_token must be mode 0600"
for route in "GET /api/config" "GET /api/events" "GET /api/modules" "GET /api/modules/hello" "POST /api/modules/hello/enable"; do
  read -r method path <<<"$route"
  if [[ $method == POST ]]; then api_anon "$method" "$path" '{}'; else api_anon "$method" "$path"; fi
  [[ $status == 401 && $(jq -r .ok <<<"$out") == false ]] || fail "$route without a token: $status $out"
done
api GET /api/config
[[ $status == 200 && $(jq -r .data.core.store.path <<<"$out") == var/kombinat.db ]] || fail "GET /api/config: $status $out"
api GET /api/modules
[[ $status == 200 && $(jq -r '[.data[].name] | join(",")' <<<"$out") == broken,hello ]] || fail "GET /api/modules: $status $out"
[[ $(jq -r '.data[] | select(.name == "broken") | .status' <<<"$out") == invalid ]] || fail "broken must list as invalid: $out"
api POST /api/modules/hello/enable '{}'
[[ $status == 200 && $(jq -r .data.status <<<"$out") == running ]] || fail "POST enable: $status $out"
api GET /api/modules/hello
[[ $status == 200 ]] || fail "GET enabled module: $status"
api POST /api/modules/broken/enable '{}'
[[ $status == 422 ]] || fail "POST enable invalid: $status $out"
api POST /api/modules/ghost/enable '{}'
[[ $status == 404 ]] || fail "POST enable unknown: $status $out"
api POST /api/modules/hello/disable '{}'
[[ $status == 200 ]] || fail "POST disable: $status $out"
expect 0 "$bin" --json log --tail 1
[[ $(jq -r '.[0].action + " " + .[0].actor' <<<"$out") == "core.module.disable api" ]] || fail "API lifecycle event: $out"
stop_server

step "hello runs standalone"
[[ $(sh modules/hello/src/cli.sh) == "Hello, world!" ]] || fail "hello standalone"

printf '✓ smoke: all steps passed\n'
