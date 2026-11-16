# Fresh CLI-only window cross-check

The previous console read completed, then the first CLI command returned JTAG
scan error87 before any SDM response. That attempt remains failed and archived.
This new one-use operator isolates the client transition: a fresh network
namespace and private top-only JTAG server run only the CLI sequence. No console
is launched or repeated. A new 60-second USB observation, vendor-first listing
and frozen forced scan must pass before the first SDM command. A failed gate is
terminal; no fallback, retry, reset, image programming or write is permitted.

The fixed sequence and three 4 KiB windows are unchanged: CONFIG_STATUS, OPEN,
chip0, AFh identity20bb18, two zero status reads, READ3a at 0/0x200000/0xfff000,
CLOSE and unchanged CONFIG_STATUS. Each call requires root ownership grants
before and after, exact echoed command, successful header and complete data.
Any uncertain reply retires normal IO and allows one close-only cleanup while
the same ownership is established. Uncertain CLOSE is never repeated. Ten
injected CLI-session models cover success, partial data, identity/status failure,
transport uncertainty, lost ownership, initial scan failure and one-use replay.

Successful data must match all12288 preserved console bytes exactly. Frozen
inputs bind that binary, its complete raw journal, failed result, independent
cleanup audit and verified archive. The prior result is not promoted to a
passing attempt; the combined evidence is recorded separately if this new
experiment succeeds. A main-array window qualification is not a complete backup.

The existing source/claim/node/UART/private-server/3MHzauto0 gates and exact
cleanup checks remain. Tool30/experiment300/host330 caps are unchanged. Factory
flash/card and the bottom kit are preserved; the spare card stays installed.
CPU Park is prior evidence, not reobserved. Two matching full main-array reads,
usable rollback and explicit flash-change approval remain open.
