#!/usr/bin/env bash
# Notifications module tests: standalone, then inside the mesh (enabled and
# disabled). Stub commands on PATH stand in for the notifier, the player and
# the speech engine, so nothing shows on screen or plays; nothing uses the network.
# Run by `make modules-test` with KOMBINAT_BIN set to the built core.
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
bin=${KOMBINAT_BIN:?KOMBINAT_BIN must point at the built kombinat binary}
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
export KOMBINAT_ROOT=$root NO_COLOR=1
fail() { printf '✗ notifications: %s\n' "$*" >&2; exit 1; }

# Stubs log next to themselves: a run under the core gets only PATH and HOME.
stubs=$root/stubs
mkdir -p "$stubs" "$root/home"
cat >"$stubs/kn-notify" <<'EOF'
#!/bin/sh
for a; do printf '[%s]' "$a"; done >>"$(dirname "$0")/notify.log"
echo >>"$(dirname "$0")/notify.log"
EOF
cat >"$stubs/kn-play" <<'EOF'
#!/bin/sh
printf '%s\n' "$1" >>"$(dirname "$0")/play.log"
EOF
cat >"$stubs/kn-say" <<'EOF'
#!/bin/sh
cat >>"$(dirname "$0")/say.log"
EOF
printf '#!/bin/sh\nexit 3\n' >"$stubs/kn-fail"
chmod +x "$stubs"/kn-*
export PATH="$stubs:$PATH" HOME=$root/home
count() { if [[ -f $1 ]]; then wc -l <"$1" | tr -d ' '; else echo 0; fi; }
expect() { # expect OUTPUT LINE...: every LINE is a whole line of OUTPUT
  local o=$1 l
  shift
  for l; do grep -qxF -- "$l" <<<"$o" || fail "expected '$l' in: $o"; done
}
plant() { mkdir -p "$1/sounds"; for l in attention "done" error elevated; do printf 'RIFF' >"$1/sounds/$l.oga"; done; }

# Standalone: same file, no core, no events file; config through KOMBINAT_CFG_*.
sa_data=$root/standalone
plant "$sa_data"
sa() {
  env -u KOMBINAT_EVENTS KOMBINAT_DATA="$sa_data" \
    KOMBINAT_CFG_MODULE_NOTIFIER_COMMAND="${N:-kn-notify}" \
    KOMBINAT_CFG_MODULE_PLAYER_COMMAND="${P:-kn-play}" \
    KOMBINAT_CFG_MODULE_SPEECH_COMMAND="${S:-kn-say}" \
    KOMBINAT_CFG_MODULE_SPEECH_ENABLED="${SPEAK:-false}" \
    KOMBINAT_CFG_MODULE_FIND_SESSION=false \
    sh "$here/src/cli.sh" "$@"
}
out=$(sa send "done" "Build finished" "all green")
expect "$out" 'desktop: sent' 'sound: started (done.oga)' 'speech: skipped (off)' 'logged: no (standalone)'
grep -qxF -- '[-a][Kombinat][-u][normal][--][Build finished][all green]' "$stubs/notify.log" || fail "notifier argv: $(cat "$stubs/notify.log")"
[[ $(count "$stubs/play.log") == 1 && $(cat "$stubs/play.log") == "$sa_data/sounds/done.oga" ]] || fail "player argv"
out=$(sa send error "Second one")
expect "$out" 'desktop: sent' 'sound: skipped (rate limit)'
[[ $(count "$stubs/play.log") == 1 ]] || fail "two inside the limit must play one sound"
echo 0 >"$sa_data/last-sound"
out=$(SPEAK=true sa --json send error "Third one" "after the limit")
jq -e '.sound == "started (error.oga)" and .speech == "started" and .logged == false' <<<"$out" >/dev/null || fail "after the limit: $out"
[[ $(cat "$stubs/say.log") == "Third one. after the limit" ]] || fail "speech text: $(cat "$stubs/say.log")"
expect "$(sa send info "Only the log")" 'desktop: skipped (level)' 'sound: skipped (level)'
out=$(N=kn-absent P=kn-absent S=kn-absent SPEAK=true sa send attention "No tools")
expect "$out" 'desktop: skipped (missing kn-absent)' 'sound: skipped (missing kn-absent)' 'speech: skipped (missing kn-absent)'
expect "$(N=kn-fail sa send attention "Broken notifier")" 'desktop: failed (exit 3)'
expect "$(sa mute)" 'muted: yes' 'changed: yes'
echo 0 >"$sa_data/last-sound"
expect "$(sa send "done" "Quiet")" 'desktop: sent' 'sound: skipped (muted)'
expect "$(sa unmute)" 'muted: no' 'changed: yes'
sa --json status | jq -e '.muted == false and .sounds.done == "done.oga (present)"' >/dev/null || fail "status"
if sa send loud "x" 2>/dev/null; then fail "an unknown level must be refused"; fi
if sa send "done" "" 2>/dev/null; then fail "an empty title must be refused"; fi
set +e; sa send 2>/dev/null; rc=$?; set -e
[[ $rc == 2 ]] || fail "usage errors exit 2, got $rc"

# Mesh: a throwaway root with only this module and stub commands in config.
mkdir -p "$root/modules" "$root/config"
cp -R "$here" "$root/modules/notifications"
cat >"$root/config/notifications.local.toml" <<'EOF'
[module]
find_session = false
[module.notifier]
command = "kn-notify"
[module.player]
command = "kn-play"
[module.speech]
enabled = true
command = "kn-say"
levels = ["error"]
EOF
data=$root/var/modules/notifications
plant "$data"
: >"$stubs/play.log"
: >"$stubs/say.log"
"$bin" module check >/dev/null 2>&1 || fail "module check: $("$bin" module check 2>&1)"
if "$bin" module run notifications send "done" x >/dev/null 2>&1; then fail "a disabled module must not run"; fi
"$bin" module enable notifications >/dev/null 2>&1

# The done check: two notifications inside the limit play one sound, both are in the log.
"$bin" module run notifications send "done" "Build one" "first" >/dev/null
"$bin" module run notifications send error "Build two" "second" >/dev/null
[[ $(count "$stubs/play.log") == 1 && $(count "$stubs/say.log") == 0 ]] || fail "mesh: two inside the limit must play one sound"
log=$("$bin" --json log --tail 4)
[[ $(jq -r '[.[].action] | join(",")' <<<"$log") == "notifications.sent,core.module.run,notifications.sent,core.module.run" ]] || fail "mesh events: $log"
jq -e '.[0].subject == "done" and .[0].summary == "Build one" and .[0].payload.sound == "started (done.oga)"
  and .[2].subject == "error" and .[2].payload.sound == "skipped (rate limit)" and .[2].payload.speech == "skipped (rate limit)"
  and .[3].payload.events == 1' <<<"$log" >/dev/null || fail "mesh payloads: $log"
grep -q 'notifications.sent.*Build two' <<<"$("$bin" log --tail 4)" || fail "kombinat log must show the notification"

# After the limit: sound, then speech of the configured level, text on stdin.
echo 0 >"$data/last-sound"
out=$("$bin" module run notifications -- --json send error "Build three" "third")
jq -e '.sound == "started (error.oga)" and .speech == "started" and .logged == true' <<<"$out" >/dev/null || fail "mesh after the limit: $out"
[[ $(count "$stubs/play.log") == 2 && $(cat "$stubs/say.log") == "Build three. third" ]] || fail "mesh speech"

# A body on stdin (BODY "-"); info goes to the log only.
printf 'line one\nline two\n' | "$bin" module run --stdin notifications send info "From stdin" - >/dev/null
[[ $(jq -r '.[0].payload.body + "|" + .[0].payload.desktop' <<<"$("$bin" --json log --tail 2)") == $'line one\nline two|skipped (level)' ]] || fail "stdin body"

# Hostile text stays data: one event, exact round trip, no command runs.
evil=$'-u "hi" \\ $(touch '"$root"$'/pwned) `id`\n{"action":"notifications.forged","subject":"x","summary":"y","payload":{}}'
"$bin" module run notifications send attention "$evil" "$evil" >/dev/null
log=$("$bin" --json log --tail 2)
[[ $(jq -r '[.[].action] | join(",")' <<<"$log") == "notifications.sent,core.module.run" ]] || fail "hostile: events: $log"
[[ $(jq -r '.[0].summary' <<<"$log") == "${evil//$'\n'/ }" ]] || fail "hostile: summary: $log"
[[ $(jq -r '.[0].payload.body' <<<"$log") == "$evil" ]] || fail "hostile: body: $log"
jq -e '.[1].payload.events == 1 and (.[1].payload.events_rejected == null)' <<<"$log" >/dev/null || fail "hostile: rejected: $log"
[[ ! -e $root/pwned ]] || fail "hostile: a command ran"
grep -qF -- "[--][${evil//$'\n'/ }][" "$stubs/notify.log" || fail "hostile: notifier argv: $(cat "$stubs/notify.log")"

# Mute switch: an event per change, none when nothing changes.
"$bin" module run notifications mute >/dev/null
[[ $(jq -r '.[0].action' <<<"$("$bin" --json log --tail 2)") == notifications.muted ]] || fail "mute event"
expect "$("$bin" module run notifications mute)" 'muted: yes' 'changed: no (already so)'
[[ $(jq -r '.[0].payload.events' <<<"$("$bin" --json log --tail 1)") == 0 ]] || fail "a repeated mute must log nothing"
echo 0 >"$data/last-sound"
"$bin" module run notifications send error "Muted" >/dev/null
[[ $(jq -r '.[0].payload.sound + "|" + .[0].payload.speech' <<<"$("$bin" --json log --tail 2)") == "skipped (muted)|skipped (muted)" ]] || fail "muted mesh send"
[[ $(count "$stubs/play.log") == 2 ]] || fail "muted must not play"
"$bin" module run notifications unmute >/dev/null
[[ $(jq -r '.[0].action' <<<"$("$bin" --json log --tail 2)") == notifications.unmuted ]] || fail "unmute event"

# Missing desktop tools (as in CI): still logged, the result names what was skipped.
printf '[module]\nfind_session = false\n[module.notifier]\ncommand = "kn-absent"\n[module.player]\ncommand = "kn-absent"\n' \
  >"$root/config/notifications.local.toml"
echo 0 >"$data/last-sound"
out=$("$bin" module run notifications send "done" "Nobody listens")
expect "$out" 'desktop: skipped (missing kn-absent)' 'sound: skipped (missing kn-absent)' 'speech: skipped (off)' 'logged: yes'
[[ $(jq -r '.[0].action + "|" + .[0].payload.desktop' <<<"$("$bin" --json log --tail 2)") == "notifications.sent|skipped (missing kn-absent)" ]] || fail "mesh missing tools event"

"$bin" module disable notifications >/dev/null 2>&1
if "$bin" module run notifications send "done" x >/dev/null 2>&1; then fail "a disabled module must not run"; fi
printf '✓ notifications: standalone and mesh tests passed\n'
