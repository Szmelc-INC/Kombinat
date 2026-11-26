# Notifications — assertions
> Deterministic invariants that gate every state-changing action (`AGENTS.md`, The one law). An action that fails an assertion is rejected, logged as an event, and the core stays up.

- Precondition: `send` is rejected with exit 2 and no event unless LEVEL is one of `info attention done error elevated` and TITLE is not empty; check: `sh src/cli.sh send loud x; echo $?` prints the usage and `2`.
- Precondition: `fetch-sounds` writes a file only when its name is plain (letters, digits, `.`, `_`, `-`, no leading dot) with an audio extension, its source starts with `https://` or `file://`, and the download is not empty and at most `module.fetch.max_bytes`; otherwise that level reports `failed (REASON)` and nothing is written.
- Invariant: every `send` that passes the preconditions writes exactly one `notifications.sent` event inside the mesh, whatever channel is missing or fails, and exits 0; check: `kombinat module run notifications send done x` then `kombinat log --tail 2` shows `notifications.sent` then `core.module.run` with `events` 1.
- Invariant: at most one audible alert (sound or speech) starts per `module.sound_interval_seconds`; check: two `send done x` runs in a row play the sound once, and the second result says `sound: skipped (rate limit)`.
- Invariant: while `KOMBINAT_DATA/muted` exists, no sound or speech starts; check: after `mute`, `send error x` says `sound: skipped (muted)`.
- Invariant: message text never reaches a shell and never ends its JSON string; check: a title holding `"`, `\`, `$(...)` and a newline lands in the log as the same text (the newline as a space) with no rejected events and no command run (`tests/notifications.sh`).
- Invariant: `mute` and `unmute` write an event only when the switch changes; a repeated call prints `changed: no (already so)` and logs nothing of its own.
- Invariant: all state stays under `KOMBINAT_DATA` (`muted`, `last-sound`, `sounds/`); the module directory is never written.
- Fallback: on assertion failure — the command exits non-zero with the reason on stderr (or `failed (REASON)` per level for `fetch-sounds`), no partial file stays behind (downloads land in a `.part` file that is moved or removed), and the core logs the run as `core.module.run` with its status.
