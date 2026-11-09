# Executable-mode guarded MMU hardware procedure

This is a corrected host procedure for the same never-booted MMU candidate
`e4ecad3bc8a33357`, compiled from firmware source `d117e486a`. The native
SDK/entry and all selected packages, ROMs and tape bytes remain unchanged.
The current recovery chain was rebuilt with current producers and reproduces
the exact previously qualified FIT/SPL/BL31/SOF bytes.

The preceding [host attempt](../2026-11-09-de25-mmu-trial-v2/hardware-result.json)
consumed its recovery reservation before a copied probe's missing execution
permission stopped the process. It performed no scan, FPGA programming, native
boot or SD write. Its complete failure and independent cleanup are retained.
The old nonexecutable copy and reservation remain untouched.

This new private tool preserves qualified mode `0775` and identical bytes.
Preflight checks its frozen digest, mode and execution access before transport
setup. The actual programmer checks them before creating an attempt and during
every live observation. Mode changes after a scan are terminal and consumed;
restoring permission cannot revive an operation. The earlier reservation's
path/hash is bound into this procedure and rechecked during live preflight.
The existing programming policy, native/recovery collectors, uploader, clock
policy and private-server code remain byte-identical. Injected tests exercise
the added mode checks; the parent suite passes 1,775 tests with three existing
skips. Full file admission covers 5,547 files across eight trees.

Only top `TRWU8CBL` is in scope. One fresh recovery/native operation each must
have its own durable reservation and successful forced scan. Recovery startup
must pass before thirteen fixed uploads and fresh reads. Native startup must
pass the selected MMU register and private memory diagnostic before accepting
U-Boot or the native launcher. Only success permits the existing complete
three-launch/four-menu/seven-configuration exercise.

Original limits remain: one-second tape transfer, native 450/process 480,
upload 300/process 320, recovery process 270, overall 800/host 890 seconds.
The first failure stops commands and permits only the existing two-second
passive trace tail before host cleanup. No deadline, live ownership/device
predicate or old attempt is relaxed. UART Select supplies input; keep physical
keys idle. The spare card remains installed; factory restoration is waived for
this ongoing top-kit work. The bottom kit remains untouched.

This record prepares a bounded diagnostic, with no hardware result yet. It
does not grant normal package/core, physical display/input, cache-coherency or
general appliance acceptance.
