# Hello — assertions
> Deterministic invariants that gate every state-changing action (`AGENTS.md`, The one law). An action that fails an assertion is rejected, logged as an event, and the core stays up.

- Precondition: `enable` and `start` are rejected unless `kombinat module check` reports `hello` valid.
- Invariant: after `disable` or `stop`, `GET /api/modules/hello` answers 404; check: `curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:PORT/api/modules/hello` prints `404`.
- Invariant: the CLI entrypoint writes only to stdout and always exits 0; check: `sh modules/hello/src/cli.sh x; echo $?` prints `Hello, x!` then `0`.
- Fallback: on assertion failure — the lifecycle call returns an error, no state row or event is written, the module keeps its previous state.
