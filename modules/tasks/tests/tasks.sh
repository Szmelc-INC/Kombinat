#!/usr/bin/env bash
# Tasks module tests: standalone, then inside the mesh through the CLI and the
# API run route, enabled and disabled. Every state change must show up in
# `kombinat log` as its tasks.* event followed by the core.module.run that
# counts it. Run by `make modules-test` with KOMBINAT_BIN set to the built
# core; the throwaway server binds 127.0.0.1 only.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
bin=${KOMBINAT_BIN:?KOMBINAT_BIN must point at the built kombinat binary}
root=$(mktemp -d)
server_pid=""
cleanup() {
  if [[ -n $server_pid ]]; then kill "$server_pid" 2>/dev/null || true; fi
  rm -rf "$root"
}
trap cleanup EXIT
export KOMBINAT_ROOT=$root NO_COLOR=1
unset KOMBINAT_DATA KOMBINAT_EVENTS KOMBINAT_CONFIG

fail() {
  printf '✗ tasks: %s\n' "$*" >&2
  if [[ -f $root/serve.log ]]; then sed 's/^/   serve: /' "$root/serve.log" >&2; fi
  exit 1
}

# expect CODE CMD...: run, keep stdout in $out and stderr in $err, check the exit code.
expect() {
  local want=$1 rc=0
  shift
  out=$("$@" 2>"$root/.stderr") || rc=$?
  err=$(cat "$root/.stderr")
  [[ $rc == "$want" ]] || fail "$* exited $rc, want $want; stdout: $out; stderr: $err"
}

# logged [ACTION...]: the newest events are ACTION... and then the
# core.module.run that counts exactly them, with nothing rejected; $log keeps them.
logged() {
  local want=core.module.run
  if (($#)); then want="$(IFS=,; printf '%s' "$*"),core.module.run"; fi
  log=$("$bin" --json log --tail $(($# + 1)))
  [[ $(jq -r '[.[].action] | join(",")' <<<"$log") == "$want" ]] || fail "log: want $want, got $log"
  [[ $(jq -r '.[-1].payload.events' <<<"$log") == "$#" ]] || fail "run event must count $# module events: $log"
  [[ $(jq -r '.[-1].payload.events_rejected // "none"' <<<"$log") == none ]] || fail "rejected events: $log"
}

# run ARGS...: the cli entrypoint inside the mesh.
run() { "$bin" module run tasks -- "$@"; }

for f in "$here"/src/*.php; do
  php -l "$f" >/dev/null || fail "php -l $f"
done

# ---- standalone: the same files, no core, no events ------------------------
data=$root/standalone
mkdir -p "$data"
sa() { KOMBINAT_DATA=$data php -f "$here/src/cli.php" -- "$@"; }
sa_api() { KOMBINAT_DATA=$data php -f "$here/src/api.php"; }

expect 0 sa add "Write docs"
[[ $out == "Added task #1 (idea): Write docs" ]] || fail "standalone add: $out"
expect 0 sa update 1 --status planned --priority 2
[[ $out == "Updated task #1: status=planned, priority=2" ]] || fail "standalone update: $out"
expect 0 sa comment add 1 "Start with the CLI"
expect 0 sa comment add 1 "Agreed" --reply-to 1 --author ala
expect 0 sa --json show 1
[[ $(jq -r '.data.comments[0].replies[0] | .author + ": " + .text' <<<"$out") == "ala: Agreed" ]] || fail "standalone thread: $out"
expect 0 sa_api <<<'{"op":"list","status":"planned"}'
[[ $(jq -r '.ok, (.data.tasks | length)' <<<"$out" | paste -sd,) == "true,1" ]] || fail "standalone api list: $out"
expect 3 sa_api <<<'{"op":"show","id":42}'
[[ $(jq -r .error.code <<<"$out") == not_found ]] || fail "standalone api not found: $out"
expect 0 sa delete 1
[[ $out == "Deleted task #1: Write docs (with 2 comments)" ]] || fail "standalone delete: $out"
expect 5 php -f "$here/src/cli.php" -- list
[[ $err == *"KOMBINAT_DATA is not set"* ]] || fail "no KOMBINAT_DATA must refuse: $err"
expect 2 sa add "Bad" --status nope
expect 2 sa frobnicate

# ---- mesh: a throwaway root with only this module --------------------------
mkdir -p "$root/modules" "$root/config"
cp -R "$here" "$root/modules/tasks"
expect 0 "$bin" module check

# The in-code defaults match config.default.toml as the core reads it.
expect 0 "$bin" config show
core_cfg=$(jq -S '.modules.tasks' <<<"$out")
expect 0 sa config
[[ $(jq -S . <<<"$out") == "$core_cfg" ]] || fail "defaults differ: core $core_cfg, module $out"

if run list >/dev/null 2>&1; then fail "a disabled module must not run"; fi
expect 0 "$bin" module enable tasks

step=0
# CLI: create, read, update, delete, every step in the log.
expect 0 run add "Ship the tasks module" --status planned --owner ala
[[ $out == "Added task #1 (planned): Ship the tasks module" ]] || fail "add: $out"
logged tasks.add
[[ $(jq -r '.[0] | .subject + " " + .payload.task.owner + " " + .actor' <<<"$log") == "task/1 ala cli" ]] || fail "add event: $log"

expect 0 run add "Write the tests" --parent 1 --priority 5 --body '{"cwd":"/tmp","steps":["one","two"]}'
logged tasks.add

expect 0 run --json list
[[ $(jq -r '.data.tasks | map("\(.id):\(.status)") | join(",")' <<<"$out") == "2:idea,1:planned" ]] || fail "list order: $out"
logged

expect 0 run update 1 --status running
[[ $out == "Updated task #1: status=running" ]] || fail "update: $out"
logged tasks.update
[[ $(jq -r '.[0].payload.changes.status | .from + ">" + .to' <<<"$log") == "planned>running" ]] || fail "update event: $log"

expect 0 run update 1 --status running
[[ $out == "Task #1 unchanged." ]] || fail "no-op update: $out"
logged

expect 2 run update 1 --status bogus
logged
expect 4 run update 1 --parent 2
[[ $err == *cycle* ]] || fail "a cycle must be refused: $err"
logged

# Threaded comments; a long text comes on stdin.
expect 0 run comment add 1 "Looks good" --author ala
[[ $out == "Added comment #1 to task #1" ]] || fail "comment add: $out"
logged tasks.comment.add
expect 0 run comment add 1 "Thanks" --reply-to 1
logged tasks.comment.add
[[ $(jq -r '.[0].payload.comment | "\(.reply_to) \(.author)"' <<<"$log") == "1 operator" ]] || fail "reply event: $log"
out=$(printf 'line one\nline two' | "$bin" module run --stdin tasks -- comment add 1 - --reply-to 2) || fail "comment from stdin"
logged tasks.comment.add

expect 0 run --json show 1
[[ $(jq -r '.data.comments[0].replies[0].replies[0].text' <<<"$out") == $'line one\nline two' ]] || fail "thread: $out"
[[ $(jq -r '.data.children | map(.id) | join(",")' <<<"$out") == 2 ]] || fail "children: $out"
logged
expect 0 run show 2
[[ $out == *'body: {"cwd":"/tmp","steps":["one","two"]}'* ]] || fail "show text: $out"

expect 0 run comment edit 1 "Looks great"
logged tasks.comment.edit
[[ $(jq -r '.[0].payload | .from + ">" + .to' <<<"$log") == "Looks good>Looks great" ]] || fail "edit event: $log"
expect 4 run comment delete 2
logged
expect 0 run comment delete 3
logged tasks.comment.delete

expect 4 run delete 1
[[ $err == *"child tasks"* ]] || fail "a parent with children must be kept: $err"
logged
expect 0 run delete 2
logged tasks.delete
[[ $(jq -r '.[0].payload.deleted.body.cwd' <<<"$log") == /tmp ]] || fail "delete keeps a copy: $log"

# Config from the core store.
printf '[tasks]\ndefault_status = "planned"\n' >"$root/config/tasks.local.toml"
expect 0 run add "Configured"
[[ $out == "Added task #3 (planned): Configured" ]] || fail "config default_status: $out"
logged tasks.add
printf '[comments]\nmax_depth = 2\n' >"$root/config/tasks.local.toml"
expect 4 run comment add 1 "Too deep" --reply-to 2
[[ $err == *"at most 2 levels"* ]] || fail "thread depth must be capped: $err"
logged
printf '[tasks]\ndefault_status = "bogus"\n' >"$root/config/tasks.local.toml"
expect 5 run list
[[ $err == *"tasks.default_status"* ]] || fail "bad config must name its key: $err"
rm "$root/config/tasks.local.toml"

# A payload too big for one event line falls back to a lean one, never a rejected line.
for ((step = 0; step < 10; step++)); do
  head -c 7000 /dev/zero | tr '\0' x | "$bin" module run --stdin tasks -- comment add 3 - >/dev/null || fail "big comment $step"
done
expect 0 run delete 3
logged tasks.delete
[[ $(jq -r '.[0].payload.comments_omitted' <<<"$log") == 10 ]] || fail "lean delete event: $log"

# ---- mesh: the API run route ------------------------------------------------
printf '[api]\nport = 0\n' >"$root/config/core.local.toml"
"$bin" serve >"$root/serve.log" 2>&1 &
server_pid=$!
for ((step = 0; step < 100; step++)); do
  [[ -s $root/var/run/api.json ]] && break
  kill -0 "$server_pid" 2>/dev/null || fail "server exited early"
  sleep 0.1
done
[[ -s $root/var/run/api.json ]] || fail "server did not record var/run/api.json"
base="http://127.0.0.1:$(jq -r .port "$root/var/run/api.json")"
# The token goes to curl through a header file, never on a command line.
printf 'Authorization: Bearer %s\n' "$(cat "$root/secrets/api_token")" >"$root/.auth"

# api METHOD PATH [JSON]: sets $status and $out.
api() {
  local args=(-sS -o "$root/.body" -w '%{http_code}' -H "@$root/.auth" -X "$1")
  if [[ $# -ge 3 ]]; then args+=(-H 'Content-Type: application/json' --data "$3"); fi
  status=$(curl "${args[@]}" "$base$2")
  out=$(cat "$root/.body")
}
# api_run REQUEST [EXIT]: the api entrypoint through POST /api/modules/tasks/run; sets $doc to its answer.
api_run() {
  local want=${2:-0}
  api POST /api/modules/tasks/run "$(jq -cn --arg s "$1" '{entrypoint: "api", stdin: $s}')"
  [[ $status == 200 ]] || fail "POST run $1: $status $out"
  [[ $(jq -r .data.exit_code <<<"$out") == "$want" ]] || fail "POST run $1: want exit $want: $out"
  doc=$(jq -r .data.stdout <<<"$out")
}

api_run '{"op":"add","title":"From the API","status":"queued","body":{"agent":"any"}}'
[[ $(jq -r '.ok, .data.task.id, .data.task.status' <<<"$doc" | paste -sd,) == "true,4,queued" ]] || fail "api add: $doc"
logged tasks.add
[[ $(jq -r '.[0].actor' <<<"$log") == api ]] || fail "api events carry the api actor: $log"
api_run '{"op":"list"}'
[[ $(jq -r '.data.tasks | map(.id) | join(",")' <<<"$doc") == "4,1" ]] || fail "api list: $doc"
logged
api_run '{"op":"update","id":4,"status":"done","owner":"bot"}'
[[ $(jq -r '.data.changes.status.to + " " + .data.task.owner' <<<"$doc") == "done bot" ]] || fail "api update: $doc"
logged tasks.update
api_run '{"op":"comment.add","task":4,"text":"Finished"}'
logged tasks.comment.add
api_run '{"op":"show","id":4}'
[[ $(jq -r '.data.task.body.agent + " " + .data.comments[0].text' <<<"$doc") == "any Finished" ]] || fail "api show: $doc"
logged
api_run '{"op":"delete","id":4}'
[[ $(jq -r '.data.deleted.id, .data.comments' <<<"$doc" | paste -sd,) == "4,1" ]] || fail "api delete: $doc"
logged tasks.delete
api_run '{"op":"show","id":4}' 3
[[ $(jq -r .error.code <<<"$doc") == not_found ]] || fail "api not found: $doc"
api_run '{"op":"add"}' 2
[[ $(jq -r .error.message <<<"$doc") == "title is required" ]] || fail "api invalid: $doc"
logged

# The api entrypoint through the CLI, same document.
out=$(printf '{"op":"list"}' | "$bin" module run --entrypoint api --stdin tasks) || fail "cli api entrypoint"
[[ $(jq -r '.data.tasks | length' <<<"$out") == 1 ]] || fail "cli api list: $out"

# Disabled: the route answers 404, the CLI refuses, the data stays.
api POST /api/modules/tasks/disable '{}'
[[ $status == 200 ]] || fail "disable: $status $out"
api POST /api/modules/tasks/run '{"entrypoint":"api","stdin":"{\"op\":\"list\"}"}'
[[ $status == 404 ]] || fail "a disabled module answered: $status $out"
if run list >/dev/null 2>&1; then fail "a disabled module must not run"; fi
api POST /api/modules/tasks/enable '{}'
[[ $status == 200 ]] || fail "enable: $status $out"
api_run '{"op":"show","id":1}'
[[ $(jq -r '.data.task.title' <<<"$doc") == "Ship the tasks module" ]] || fail "data must survive disable: $doc"

kill -TERM "$server_pid"
wait "$server_pid" || fail "server exited non-zero on SIGTERM"
server_pid=""
printf '✓ tasks: standalone, CLI and API tests passed\n'
