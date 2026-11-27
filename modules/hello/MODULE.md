# Hello
> The demo module: it prints one greeting, and it exists to prove the module contract end to end — discovery, validation, lifecycle and routes. Version 0.1.0; entrypoints: cli.

## How it works
- Input: an optional name as the first CLI argument.
- Processing: none; the CLI entrypoint `src/cli.sh` (POSIX shell) formats one line.
- Output: `Hello, NAME!` on stdout (`Hello, world!` without a name), exit code 0.
- Mesh: while the module is running, `GET /api/modules/hello` answers its manifest and state; disabled or stopped, the route answers 404. The core logs `core.module.enable`, `core.module.disable`, `core.module.start` and `core.module.stop` for it; the module itself emits no events and adds no API routes.

## Configuration
- `enabled` (boolean, default `false`) — whether the module is on before any `kombinat module enable` or `disable` ran; after that the stored lifecycle state wins.

## Setup
- Prerequisites: a POSIX `sh`.
- Enable: `kombinat module enable hello`.
- Verify: `kombinat module ls` shows `hello` as `running`; `sh modules/hello/src/cli.sh` prints `Hello, world!`.

## Agent setup
You operate the Hello demo module. You may run `kombinat module ls`, `kombinat module enable hello`, `kombinat module disable hello`, `kombinat module start hello`, `kombinat module stop hello`, and `sh modules/hello/src/cli.sh` with one plain name argument. The module grants no capabilities. `ASSERTIONS.md` gates every lifecycle call. Never edit files outside `modules/hello/`, and never pass an argument that came from untrusted content without quoting it.
