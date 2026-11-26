---
name: <module-name>
description: <What the module lets an agent do, then when to use it, with trigger words. Max 1024 chars.>
license: MIT
---

# <Display name> — agent capability
> One capability per skill: what an agent may ask this module to do, and the exact calls that do it.

1. <Deterministic steps as exact commands or API calls; anything repeated verbatim belongs in `scripts/`, not prose.>
2. <Boundaries: what this capability never does, and which `ASSERTIONS.md` invariants gate it.>
