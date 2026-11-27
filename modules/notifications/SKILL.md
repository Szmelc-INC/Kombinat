---
name: notifications
description: Notify the human through Kombinat with a desktop popup, a rate-limited sound and optional speech, and log every notification as an event. Use when a long task finished, when the human must decide something, when work hits a failure or a blocker, or right before an elevated (sudo, pkexec) command asks for a password; also to mute or unmute sounds, check which notification tools are present, or fetch the sound pack. Triggers: notify, notification, ping me, let me know, alert, sound, mute, beep.
license: MIT
---

# Notifications — agent capability
> One capability per skill: what an agent may ask this module to do, and the exact calls that do it.

1. Notify: `kombinat module run notifications send LEVEL "TITLE" "BODY"` (or `POST /api/modules/notifications/run` with `{"args": ["--json", "send", "LEVEL", "TITLE", "BODY"]}`). LEVEL is `info` (log only), `attention` (the human must decide something), `done` (long work completed), `error` (a failure or a blocker) or `elevated` (an elevated command is next; put the exact command in the body). The result names each channel: `desktop`, `sound`, `speech` as `sent`/`started` or `skipped (REASON)`/`failed (REASON)`, and `logged`. A long body goes on stdin: `BODY` `-` with `kombinat module run --stdin notifications send done "TITLE" -`. Standalone: `sh modules/notifications/src/cli.sh send LEVEL "TITLE" "BODY"`.
2. Expect silence inside the limit: at most one sound or spoken line per `module.sound_interval_seconds` (default 120); a second notification inside it still shows and is logged with `sound: skipped (rate limit)`. Do not resend to force a sound.
3. Inspect: `kombinat module run notifications status` shows the mute switch, the next free sound slot, which tools are found or missing, and which sound files are present.
4. Mute and fetch, only when the human asks: `kombinat module run notifications mute` / `unmute` (logs `notifications.muted` / `notifications.unmuted` when the switch changes); `kombinat module run notifications fetch-sounds` downloads or copies the sound pack and is the only call that may use the network.
5. Boundaries: text is data, passed as separate arguments, never through a shell; never put secrets, tokens or private data in a notification, since every one lands in the append-only log; the module reads no other module and grants no capabilities; `ASSERTIONS.md` gates the state changes.
