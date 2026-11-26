#!/bin/sh
# Notifications CLI entrypoint: tells the human what happened. One notification is
# a desktop popup, at most one sound (or spoken line) per interval, and one
# notifications.sent event, whichever channels are available.
#
# Inside the mesh the core sets KOMBINAT_CFG_* from config, KOMBINAT_DATA (this
# module's private directory) and KOMBINAT_EVENTS (this run's event file).
# Standalone: sh modules/notifications/src/cli.sh [--json] COMMAND [ARGS]
# Message text is untrusted data: it reaches commands only as one quoted
# argument after `--`, or on stdin, and events only as escaped JSON strings.
set -eu

LEVELS='info attention done error elevated'
TITLE_MAX=200
BODY_MAX=2000

usage() {
  cat <<'EOF'
Usage: sh src/cli.sh [--json] COMMAND [ARGS]
  send LEVEL TITLE [BODY]   notify at LEVEL: info, attention, done, error or elevated;
                            BODY "-" reads the body from stdin
  mute                      silence sounds and speech (desktop popups still show)
  unmute                    allow sounds and speech again
  status                    show the mute switch, the rate limit, the tools and the sounds
  fetch-sounds [--force]    download or copy the sound pack into the data directory
  help                      print this help
--json prints one JSON document instead of lines.
EOF
}

die_usage() {
  printf 'notifications: %s\n' "$1" >&2
  usage >&2
  exit 2
}

# cfg KEY DEFAULT: the flattened config value KOMBINAT_CFG_KEY, or DEFAULT when
# the core did not set it (a standalone run). An empty value stays empty.
# KEY is a constant or built from a validated level name, never from input.
cfg() {
  eval "printf '%s' \"\${KOMBINAT_CFG_$1-\$2}\""
}

is_true() { [ "$1" = true ]; }

# level_default LEVEL FIELD: the built-in default, equal to config.default.toml.
level_default() {
  case $1:$2 in
    info:desktop) echo false ;;
    *:desktop) echo true ;;
    info:urgency) echo low ;;
    error:urgency | elevated:urgency) echo critical ;;
    *:urgency) echo normal ;;
    info:sound | info:source) echo '' ;;
    *:sound) echo "$1.oga" ;;
    attention:source) echo file:///usr/share/sounds/freedesktop/stereo/message-new-instant.oga ;;
    done:source) echo file:///usr/share/sounds/freedesktop/stereo/complete.oga ;;
    error:source) echo file:///usr/share/sounds/freedesktop/stereo/dialog-warning.oga ;;
    elevated:source) echo file:///usr/share/sounds/freedesktop/stereo/dialog-information.oga ;;
  esac
}

level_cfg() { # level_cfg LEVEL FIELD
  cfg "MODULE_LEVELS_$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')_$(printf '%s' "$2" | tr '[:lower:]' '[:upper:]')" "$(level_default "$1" "$2")"
}

valid_level() {
  case $1 in '' | *[!a-z]*) return 1 ;; esac
  case " $LEVELS " in *" $1 "*) return 0 ;; esac
  return 1
}

# A sound file name: plain, no path, a known audio extension.
valid_sound_name() {
  case $1 in
    .* | *[!A-Za-z0-9._-]*) return 1 ;;
    *.oga | *.ogg | *.opus | *.mp3 | *.wav | *.flac) return 0 ;;
  esac
  return 1
}

# in_list WORD JSON_ARRAY: whether a level name is in a config array, which the
# core passes as JSON (["a","b"]); level names never contain quotes.
in_list() {
  case $2 in *"\"$1\""*) return 0 ;; esac
  return 1
}

# first_word CMD and run_cfg CMD ARGS...: a configured command is split on
# blanks without globbing (it is operator config, never message text), and the
# message travels as separate arguments; no shell ever parses it.
first_word() {
  set -f
  # shellcheck disable=SC2086 # splitting the configured command is the point
  set -- $1
  set +f
  printf '%s' "${1-}"
}

run_cfg() {
  cmdline=$1
  shift
  set -f
  # shellcheck disable=SC2086 # splitting the configured command is the point
  set -- $cmdline "$@"
  set +f
  "$@"
}

have() { [ -n "$1" ] && command -v "$1" >/dev/null 2>&1; }

# json_str STRING: a JSON string literal; quotes, backslashes and every control
# character are escaped, so no input can end the string or the event line.
json_str() {
  JSON_S=$1 LC_ALL=C awk 'BEGIN {
    s = ENVIRON["JSON_S"]; out = ""
    for (i = 1; i < 32; i++) ctl[sprintf("%c", i)] = sprintf("\\u%04x", i)
    ctl[sprintf("%c", 127)] = "\\u007f"
    n = length(s)
    for (i = 1; i <= n; i++) {
      c = substr(s, i, 1)
      if (c == "\\") out = out "\\\\"
      else if (c == "\"") out = out "\\\""
      else if (c in ctl) out = out ctl[c]
      else out = out c
    }
    printf "\"%s\"", out
  }'
}

# trunc STRING BYTES: at most BYTES bytes, never ending inside a UTF-8 sequence.
trunc() {
  TRUNC_S=$1 LC_ALL=C awk -v n="$2" 'BEGIN {
    s = ENVIRON["TRUNC_S"]
    if (length(s) <= n) { printf "%s", s; exit }
    for (i = 128; i < 256; i++) ord[sprintf("%c", i)] = i
    s = substr(s, 1, n)
    for (k = 0; k < 4 && k < n; k++) {
      c = substr(s, n - k, 1)
      b = (c in ord) ? ord[c] : 0
      if (b < 128) break
      if (b >= 192) {
        len = (b >= 240) ? 4 : (b >= 224) ? 3 : 2
        if (k + 1 < len) s = substr(s, 1, n - k - 1)
        break
      }
    }
    printf "%s", s
  }'
}

# emit ACTION SUBJECT SUMMARY PAYLOAD_JSON: one strict event line; standalone
# (no KOMBINAT_EVENTS) nothing is logged.
emit() {
  if [ -z "${KOMBINAT_EVENTS:-}" ]; then
    return 0
  fi
  printf '{"action":"%s","subject":%s,"summary":%s,"payload":%s}\n' \
    "$1" "$(json_str "$2")" "$(json_str "$3")" "$4" >>"$KOMBINAT_EVENTS"
}

logged() {
  if [ -n "${KOMBINAT_EVENTS:-}" ]; then echo true; else echo false; fi
}

logged_text() {
  if [ -n "${KOMBINAT_EVENTS:-}" ]; then echo yes; else echo 'no (standalone)'; fi
}

uint_or() { # uint_or VALUE DEFAULT
  case $1 in '' | *[!0-9]*) printf '%s' "$2" ;; *) printf '%s' "$1" ;; esac
}

# The desktop session: a run under the core gets no DBus or runtime directory
# (decision 17), so with find_session the module looks up the user's own.
find_session() {
  if ! is_true "$(cfg MODULE_FIND_SESSION true)"; then
    return 0
  fi
  if [ -z "${XDG_RUNTIME_DIR:-}" ]; then
    d=/run/user/$(id -u)
    if [ -d "$d" ]; then
      XDG_RUNTIME_DIR=$d
      export XDG_RUNTIME_DIR
    fi
  fi
  if [ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ] && [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -S "$XDG_RUNTIME_DIR/bus" ]; then
    DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"
    export DBUS_SESSION_BUS_ADDRESS
  fi
}

setup_data() {
  if [ -n "${KOMBINAT_DATA:-}" ]; then
    DATA=$KOMBINAT_DATA
  else
    DATA=${XDG_STATE_HOME:-${HOME:?HOME or KOMBINAT_DATA must be set}/.local/state}/kombinat/notifications
  fi
  SOUNDS=$DATA/sounds
  mkdir -p "$SOUNDS"
  chmod 700 "$DATA"
}

interval() { uint_or "$(cfg MODULE_SOUND_INTERVAL_SECONDS 120)" 120; }

last_sound() {
  last=0
  if [ -f "$DATA/last-sound" ]; then
    last=$(cat "$DATA/last-sound")
  fi
  uint_or "$last" 0
}

# claim_slot: take the one audible slot per interval, atomically (a mkdir lock
# guards read-compare-write against a concurrent run). 0 = taken.
claim_slot() {
  lock=$DATA/.sound.lock
  tries=0
  while ! mkdir "$lock" 2>/dev/null; do
    tries=$((tries + 1))
    if [ "$tries" -ge 3 ]; then
      # A lock older than three seconds is left from a killed run.
      rm -rf "$lock"
      mkdir "$lock" 2>/dev/null || return 1
      break
    fi
    sleep 1
  done
  now=$(date +%s)
  last=$(last_sound)
  if [ "$last" -gt "$now" ]; then
    last=0
  fi
  rc=1
  if [ $((now - last)) -ge "$(interval)" ]; then
    printf '%s\n' "$now" >"$DATA/last-sound.tmp"
    mv "$DATA/last-sound.tmp" "$DATA/last-sound"
    rc=0
  fi
  rmdir "$lock"
  return "$rc"
}

cmd_send() {
  if [ $# -lt 2 ] || [ $# -gt 3 ]; then
    die_usage 'send needs LEVEL TITLE [BODY]'
  fi
  level=$1
  valid_level "$level" || die_usage "unknown level: $level (want: $LEVELS)"
  # Titles are one line without control characters; bodies keep newlines and tabs.
  title=$(printf '%s' "$2" | tr '\n\r\t' '   ' | tr -d '\000-\010\013-\037\177')
  title=$(trunc "$title" "$TITLE_MAX")
  [ -n "$title" ] || die_usage 'send needs a non-empty TITLE'
  body=${3-}
  if [ "$body" = - ]; then
    body=$(cat)
  fi
  body=$(printf '%s' "$body" | tr -d '\000-\010\013\014\016-\037\177')
  body=$(trunc "$body" "$BODY_MAX")
  setup_data
  find_session
  muted=false
  if [ -e "$DATA/muted" ]; then
    muted=true
  fi

  # Desktop popup.
  notifier=$(cfg MODULE_NOTIFIER_COMMAND notify-send)
  urgency=$(level_cfg "$level" urgency)
  case $urgency in low | normal | critical) ;; *) urgency=normal ;; esac
  if ! is_true "$(level_cfg "$level" desktop)"; then
    desktop='skipped (level)'
  elif [ -z "$notifier" ]; then
    desktop='skipped (disabled)'
  elif ! have "$(first_word "$notifier")"; then
    desktop="skipped (missing $(first_word "$notifier"))"
  else
    rc=0
    run_cfg "$notifier" -a "$(cfg MODULE_APP_NAME Kombinat)" -u "$urgency" -- "$title" "$body" \
      </dev/null >/dev/null 2>&1 || rc=$?
    if [ "$rc" -eq 0 ]; then desktop=sent; else desktop="failed (exit $rc)"; fi
  fi

  # Sound: decide now, play after the event is written.
  player=$(cfg MODULE_PLAYER_COMMAND pw-play)
  sound_name=$(level_cfg "$level" sound)
  if [ -z "$sound_name" ]; then
    sound='skipped (level)'
  elif is_true "$muted"; then
    sound='skipped (muted)'
  elif ! valid_sound_name "$sound_name"; then
    sound='skipped (bad file name)'
  elif [ -z "$player" ]; then
    sound='skipped (disabled)'
  elif ! have "$(first_word "$player")"; then
    sound="skipped (missing $(first_word "$player"))"
  elif [ ! -f "$SOUNDS/$sound_name" ]; then
    sound="skipped (no file $sound_name; run fetch-sounds)"
  else
    sound=pending
  fi

  # Speech: same rules, its own command and level list.
  speaker=$(cfg MODULE_SPEECH_COMMAND 'espeak-ng --stdin')
  if ! is_true "$(cfg MODULE_SPEECH_ENABLED false)"; then
    speech='skipped (off)'
  elif ! in_list "$level" "$(cfg MODULE_SPEECH_LEVELS '["attention","error","elevated"]')"; then
    speech='skipped (level)'
  elif is_true "$muted"; then
    speech='skipped (muted)'
  elif [ -z "$speaker" ]; then
    speech='skipped (disabled)'
  elif ! have "$(first_word "$speaker")"; then
    speech="skipped (missing $(first_word "$speaker"))"
  else
    speech=pending
  fi

  # One audible slot per interval, shared by sound and speech.
  if [ "$sound" = pending ] || [ "$speech" = pending ]; then
    if claim_slot; then
      if [ "$sound" = pending ]; then sound="started ($sound_name)"; fi
      if [ "$speech" = pending ]; then speech=started; fi
    else
      if [ "$sound" = pending ]; then sound='skipped (rate limit)'; fi
      if [ "$speech" = pending ]; then speech='skipped (rate limit)'; fi
    fi
  fi

  # The event and the result come before any audio, so a slow player or the
  # run timeout can never cost the log entry.
  emit notifications.sent "$level" "$title" \
    "{\"level\":\"$level\",\"body\":$(json_str "$body"),\"desktop\":$(json_str "$desktop"),\"sound\":$(json_str "$sound"),\"speech\":$(json_str "$speech")}"
  if [ "$JSON" = true ]; then
    printf '{"level":"%s","title":%s,"desktop":%s,"sound":%s,"speech":%s,"logged":%s}\n' \
      "$level" "$(json_str "$title")" "$(json_str "$desktop")" "$(json_str "$sound")" "$(json_str "$speech")" "$(logged)"
  else
    printf 'level: %s\ntitle: %s\ndesktop: %s\nsound: %s\nspeech: %s\nlogged: %s\n' \
      "$level" "$title" "$desktop" "$sound" "$speech" "$(logged_text)"
  fi

  # Sound, then speech, one after the other; failures stay silent.
  case $sound in
    started*) run_cfg "$player" "$SOUNDS/$sound_name" </dev/null >/dev/null 2>&1 || true ;;
  esac
  if [ "$speech" = started ]; then
    spoken=$(trunc "$title. $body" "$(uint_or "$(cfg MODULE_SPEECH_MAX_BYTES 300)" 300)")
    printf '%s\n' "$spoken" | run_cfg "$speaker" >/dev/null 2>&1 || true
  fi
}

cmd_mute() { # cmd_mute on|off
  [ $# -eq 1 ] || die_usage "$CMD takes no arguments"
  setup_data
  changed=false
  if [ "$1" = on ] && [ ! -e "$DATA/muted" ]; then
    : >"$DATA/muted"
    changed=true
    emit notifications.muted notifications 'sounds and speech muted' '{"muted":true}'
  elif [ "$1" = off ] && [ -e "$DATA/muted" ]; then
    rm -f "$DATA/muted"
    changed=true
    emit notifications.unmuted notifications 'sounds and speech unmuted' '{"muted":false}'
  fi
  muted=false
  if [ "$1" = on ]; then muted=true; fi
  if [ "$JSON" = true ]; then
    printf '{"muted":%s,"changed":%s,"logged":%s}\n' "$muted" "$changed" "$(if is_true "$changed"; then logged; else echo false; fi)"
  else
    printf 'muted: %s\nchanged: %s\n' "$(if is_true "$muted"; then echo yes; else echo no; fi)" "$(if is_true "$changed"; then echo yes; else echo 'no (already so)'; fi)"
  fi
}

tool_state() { # tool_state CMD: "NAME (found)", "NAME (missing)" or "disabled"
  if [ -z "$1" ]; then
    echo disabled
  elif have "$(first_word "$1")"; then
    echo "$(first_word "$1") (found)"
  else
    echo "$(first_word "$1") (missing)"
  fi
}

cmd_status() {
  [ $# -eq 0 ] || die_usage 'status takes no arguments'
  setup_data
  muted=false
  if [ -e "$DATA/muted" ]; then muted=true; fi
  now=$(date +%s)
  last=$(last_sound)
  iv=$(interval)
  wait=0
  if [ "$last" -gt 0 ] && [ "$last" -le "$now" ] && [ $((now - last)) -lt "$iv" ]; then
    wait=$((iv - (now - last)))
  fi
  notifier=$(tool_state "$(cfg MODULE_NOTIFIER_COMMAND notify-send)")
  player=$(tool_state "$(cfg MODULE_PLAYER_COMMAND pw-play)")
  speech=$(tool_state "$(cfg MODULE_SPEECH_COMMAND 'espeak-ng --stdin')")
  if ! is_true "$(cfg MODULE_SPEECH_ENABLED false)"; then speech="off, $speech"; fi
  sounds=''
  sounds_json=''
  for level in $LEVELS; do
    name=$(level_cfg "$level" sound)
    if [ -z "$name" ]; then
      state=none
    elif ! valid_sound_name "$name"; then
      state="$name (bad file name)"
    elif [ -f "$SOUNDS/$name" ]; then
      state="$name (present)"
    else
      state="$name (missing)"
    fi
    sounds="${sounds}sound $level: $state
"
    sounds_json="$sounds_json${sounds_json:+,}\"$level\":$(json_str "$state")"
  done
  if [ "$JSON" = true ]; then
    printf '{"muted":%s,"interval_seconds":%s,"last_sound":%s,"next_sound_in_seconds":%s,"notifier":%s,"player":%s,"speech":%s,"sounds":{%s}}\n' \
      "$muted" "$iv" "$last" "$wait" "$(json_str "$notifier")" "$(json_str "$player")" "$(json_str "$speech")" "$sounds_json"
  else
    printf 'muted: %s\ninterval: %s s\nnext sound in: %s s\nnotifier: %s\nplayer: %s\nspeech: %s\n%s' \
      "$(if is_true "$muted"; then echo yes; else echo no; fi)" "$iv" "$wait" "$notifier" "$player" "$speech" "$sounds"
  fi
}

# fetch-sounds: the only command that may touch the network. It downloads (or
# copies, for file:// sources) each level's sound into the data directory.
cmd_fetch() {
  force=false
  case $#:${1-} in
    0:) ;;
    1:--force) force=true ;;
    *) die_usage 'fetch-sounds takes only --force' ;;
  esac
  setup_data
  timeout=$(uint_or "$(cfg MODULE_FETCH_TIMEOUT_SECONDS 6)" 6)
  max=$(uint_or "$(cfg MODULE_FETCH_MAX_BYTES 2000000)" 2000000)
  fetched=0 present=0 skipped=0 failed=0
  lines='' results=''
  for level in $LEVELS; do
    name=$(level_cfg "$level" sound)
    src=$(level_cfg "$level" source)
    if [ -z "$name" ]; then
      continue
    fi
    if ! valid_sound_name "$name"; then
      state='failed (bad file name)'
    elif [ -f "$SOUNDS/$name" ] && ! is_true "$force"; then
      state=present
    elif [ -z "$src" ]; then
      state='skipped (no source)'
    else
      case $src in
        https://* | file://*)
          if ! have curl; then
            state='failed (missing curl)'
          else
            tmp=$SOUNDS/.$name.part
            rc=0
            curl -fsSL --proto '=https,file' --proto-redir '=https' --max-time "$timeout" \
              --max-filesize "$max" -o "$tmp" -- "$src" </dev/null >/dev/null 2>&1 || rc=$?
            size=0
            if [ -f "$tmp" ]; then size=$(wc -c <"$tmp" | tr -d ' '); fi
            if [ "$rc" -ne 0 ]; then
              state="failed (curl exit $rc)"
            elif [ "$size" -eq 0 ] || [ "$size" -gt "$max" ]; then
              state="failed (size $size)"
            else
              mv "$tmp" "$SOUNDS/$name"
              state=fetched
            fi
            rm -f "$tmp"
          fi
          ;;
        *) state='failed (source must be https:// or file://)' ;;
      esac
    fi
    case $state in
      fetched) fetched=$((fetched + 1)) ;;
      present) present=$((present + 1)) ;;
      skipped*) skipped=$((skipped + 1)) ;;
      *) failed=$((failed + 1)) ;;
    esac
    lines="$lines$level: $name $state
"
    results="$results${results:+,}\"$level\":$(json_str "$state")"
  done
  summary="sound pack: $fetched fetched, $present present, $skipped skipped, $failed failed"
  emit notifications.sounds.fetched notifications "$summary" \
    "{\"fetched\":$fetched,\"present\":$present,\"skipped\":$skipped,\"failed\":$failed,\"results\":{$results}}"
  if [ "$JSON" = true ]; then
    printf '{"fetched":%s,"present":%s,"skipped":%s,"failed":%s,"results":{%s},"logged":%s}\n' \
      "$fetched" "$present" "$skipped" "$failed" "$results" "$(logged)"
  else
    printf '%s%s\n' "$lines" "$summary"
  fi
  [ "$failed" -eq 0 ]
}

JSON=false
if [ "${1-}" = --json ]; then
  JSON=true
  shift
fi
CMD=${1-help}
if [ $# -gt 0 ]; then shift; fi
case $CMD in
  send) cmd_send "$@" ;;
  mute) cmd_mute on "$@" ;;
  unmute) cmd_mute off "$@" ;;
  status) cmd_status "$@" ;;
  fetch-sounds) cmd_fetch "$@" ;;
  help | -h | --help) usage ;;
  *) die_usage "unknown command: $CMD" ;;
esac
