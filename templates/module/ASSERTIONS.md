# <Display name> — assertions
> Deterministic invariants that gate every state-changing action (`AGENTS.md`, The one law). An action that fails an assertion is rejected, logged as an event, and the core stays up.

- Precondition: <action> is rejected unless <literal condition>.
- Invariant: after <action>, <what always holds>; check: <command>.
- Fallback: on assertion failure — <event emitted, action rejected, no partial state>.
