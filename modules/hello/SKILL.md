---
name: hello
description: Print a greeting through the Hello demo module, and check that the Kombinat module contract works (discovery, lifecycle, routes). Use when asked to greet, to smoke-test a Kombinat install, or to show how a module is enabled and disabled.
license: MIT
---

# Hello — agent capability
> One capability per skill: what an agent may ask this module to do, and the exact calls that do it.

1. Greet: `sh modules/hello/src/cli.sh "NAME"` prints `Hello, NAME!`; without an argument it prints `Hello, world!`.
2. Check the contract: `kombinat module enable hello`, then `kombinat module ls` shows `running` and `GET /api/modules/hello` answers 200; `kombinat module disable hello`, then the route answers 404 and `kombinat log --tail 1` shows `core.module.disable`.
3. Boundaries: Hello reads no files, opens no network connection and keeps no state; the lifecycle calls are gated by `ASSERTIONS.md`.
