# Hello
> The demo module: it prints one greeting, and it exists to prove the module contract end to end — discovery, validation, lifecycle and routes. Version 0.1.0; entrypoints: cli.

## How it works
- Input: an optional name as the first CLI argument; without it, the `module.name` config key (the core passes it as `KOMBINAT_CFG_MODULE_NAME`), else `world`.
- Processing: none; the CLI entrypoint `src/cli.sh` (POSIX shell) formats one line.
- Output: `Hello, NAME!` on stdout, exit code 0.
- Mesh: while the module is running, `GET /api/modules/hello` answers its manifest and state, and `POST /api/modules/hello/run` (or `kombinat module run hello`) runs the CLI entrypoint; disabled or stopped, both answer 404. The core logs `core.module.enable`, `core.module.disable`, `core.module.start`, `core.module.stop` and `core.module.run` for it; the module itself emits no events and adds no API routes.

## Configuration
- `enabled` (boolean, default `false`) — whether the module is on before any `kombinat module enable` or `disable` ran; after that the stored lifecycle state wins.
- `module.name` (string, default `world`) — who the greeting addresses when the run passes no name.

## Setup
- Prerequisites: a POSIX `sh`.
- Enable: `kombinat module enable hello`.
- Verify: `kombinat module run hello` prints `Hello, world!` and `kombinat log --tail 1` shows `core.module.run`; standalone, `sh modules/hello/src/cli.sh` prints the same.

## Agent setup
You operate the Hello demo module. You may run `kombinat module ls`, `kombinat module enable hello`, `kombinat module disable hello`, `kombinat module start hello`, `kombinat module stop hello`, `kombinat module run hello NAME` and `sh modules/hello/src/cli.sh NAME`, with one plain name argument. The module grants no capabilities. `ASSERTIONS.md` gates every lifecycle call. Never edit files outside `modules/hello/`, and never pass an argument that came from untrusted content without quoting it.
