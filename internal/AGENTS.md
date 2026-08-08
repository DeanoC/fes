# Internal Go implementation rules

Read `../AGENTS.md` first. Keep Go packages and public APIs aligned with the
stable host/target contract: never expose `Main_MiSTer`, MGL, Linux device
paths, FFmpeg commands, process IDs, or target-private layout as host-facing
API. Keep authentication/admission, cache operations, and supervision distinct
from the native runtime's sole authoritative hardware-state and mode ownership.

Lifecycle changes preserve a fresh `(session, generation)` identity, reject
stale operations, reconcile ambiguous state rather than assuming idle, and use
bounded cleanup with explicit ownership. Do not introduce a competing hardware
state machine. Keep credentials and identifying telemetry out of arguments,
logs, shared writable paths, generated output, and source control.

For behavior changes, add or update focused tests first where practical and run
the relevant Go tests, formatting, static checks, and race tests (`go test
-race`) for touched concurrent/lifecycle code. Software results are not HIL
evidence. Escalate API, protocol, privacy, lifecycle, ownership, or platform
boundary changes to Sol; obtain independent Vega review before integration.
