# Restricted QSPI window qualification

This operator qualifies three fixed 4 KiB windows at byte offsets 0, 0x200000
and 0xfff000 of TRWU8CBL's inferred 16 MiB main flash array. It permits no write,
erase, generic device opcode, controller handover, image programming or reboot.
It does not constitute a complete backup or authorize a flash change.

A held System Console configuration packet service runs one exact sequence:
CONFIG_STATUS, OPEN, SELECT chip 0, AFh identity, two status reads, three READ3a
commands, CLOSE and CONFIG_STATUS. The identity must be 20bb18, both status
bytes zero and configuration unchanged. Each 1024-word read must return the
complete successful header plus exactly 1024 words; partial success is rejected.
The verified Tcl8 binary codec preserves little-endian 32-bit words.

A root-owned loopback guard admits the exact packet before sending and checks
ownership again after it returns. Serial, immutable packet and session nonce
must match. Lost ownership stops further IO. A transport, response or output
failure permits at most one CLOSE while ownership remains valid. An uncertain
CLOSE cannot be retried. Flash commands are restricted independently by the
immutable Tcl body and root guard. The guard runs on the parent main thread;
there is no thread/preexec interaction. A separate CLI session cross-checks all
three windows only after the console has exited and released its packet claim.
Full commands, responses and binary bytes are retained for independent replay.

The existing clean-source, frozen-input, claim, sixty-second parent/top USB,
free UART, private top-only JTAG server, vendor-first device listing and fresh
forced-scan gates remain. The whole attempt occupies a private network namespace;
the console runs as UID65534 with no direct Altera USB write permission. Tcl,
client config and literal runtime configuration are root-owned and read-only.
The exclusive private server uses 3 MHz with automatic adjustment disabled;
clock readback occurs at session boundaries. Host cleanup verifies 15 MHz/auto1,
exact USB permissions, private client mode, no namespace/server survivors and
free UART. Each tool call is capped at 30 seconds, experiment 300, host 330.
The previous native Park is prior evidence, not a new CPU-state observation.

Offline tests use the installed vendor Tcl8 engine with injected transport:
twenty reader cases cover success, malformed/partial replies, uncertain IO,
wrong identity/status, existing output, duplicate/foreign paths, output failure,
ownership loss, release failure and replay. Root gate tests reject unknown
operations, ranges, stale serials and extra closes. The complete raw-eval
collector, independent Python byte/journal/CLI parser and real restricted-UID
staging also pass. A Tcl signed-word comparison found during development was
fixed before admission; its failed model log is preserved.

After a passing physical window test, two complete independent 16 MiB passes,
matching hashes and a usable rollback artifact remain required before any
flash-change approval. OTP/security areas are outside this main-array backup.
The factory card/flash and the other DE25 remain preserved.
