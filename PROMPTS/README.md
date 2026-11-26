# PROMPTS
> The agent prompt suite that builds MkII. Paste one prompt per agent session; each is self-contained and assumes `AGENTS.md`. Replace `{...}` inputs. Rules for writing more: the szmelc conventions, `AGENTS/PROMPT/` tree.

## Build order
1. **Foundation** — `CORE.md` builds the kernel once; `AUDIT.md` may run in parallel (it only reads the private v1).
2. **Port loop** — `PORT.md` runs per `docs/PORTING.md` row, many agents in parallel, one feature = one module = one PR, each PR gated by `REVIEW.md`, the whole program kept coherent by `MASTER.md`.
3. **Done** — the queue has no `todo` rows left.

## Map
| Prompt | Run by | When | Output |
|---|---|---|---|
| `CORE.md` | One agent, once | The repo holds prompts and templates only | The Go + PHP kernel in `core/`, `make` targets, extended CI |
| `AUDIT.md` | One agent, once | Parallel with the core | `docs/PORTING.md` queue + local `PORTING.local.md` source map |
| `PORT.md` | One agent per queue row | Per row, after the core and the audit | One module, one PR |
| `REVIEW.md` | A second agent or a human | Every PR, before merge | Findings + verdict |
| `MASTER.md` | One coordinating agent | Long-lived, alongside the port loop | Dispatch, decisions, wave reports |
