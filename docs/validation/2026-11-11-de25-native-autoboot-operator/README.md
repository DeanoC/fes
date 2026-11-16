# Bounded automatic native startup and installed-core trial

Observer code `50f9bc8cd`; native producer code `303771739` and exact boot
`548caa50395f3d94`. This is a prepared diagnostic operator, not a hardware pass.

The UART collector opens before programming and receives throughout it. The
native phase waits for the EL1 handoff and verified initial launcher without
sending `fesboot` or a U-Boot environment command. Its first UART write is a
normal App command after startup admission. Explicit startup policy reaches
both the live observer and independent saved-wire replay. Manual or interrupted
startup, unsolicited lifecycle input and claim loss stop the attempt.

The separate recovery epoch checks the previous FIT, thirteen other staged
sources and three installed records; it refreshes only `FES/UPLOAD.BIN`, then
reads the complete seventeen-file set again. The native epoch discovers both
installed targets, issues no Install, verifies ZX81 trial/tape and ZX80 trial
launch/return, and reaches final Park. Target USB serial is TRWU8CBL; the
factory card and flash stay preserved. Private JTAG services, one-use grants,
clock policy and cleanup checks retain their existing bounds and invariants.

## Evidence before hardware

The parent suite passes 1,978 tests with three skips. Focused decoder checks
execute the pinned vendor countdown with injected timer/key callbacks and
exercise the selected paired launch/return observer. Operator models cover
receive-only startup, prior/manual/missing/input failures, actual uploader
preservation of three records, refresh-to-native gating, saved-wire replay,
claim loss, uncertain writes and independent service epochs.

The previous recovery package correctly rejected a stale producer hash. Fresh
baseline BL31, recovery U-Boot and return-trace U-Boot builds plus both recovery
packages now pass. Their FIT/SPL/SOF/manifest/boot-prefix bytes exactly match
the protected recovery files already installed on the spare card. No card
replacement or protected-file update is needed. See `recovery-refresh.json`.

Fresh full source/artifact admission checks 5667 files and
rejects wrong admission digest and lost ownership. That file-only record
confers no programming authority. `operator-source.json` freezes helpers,
inputs, tools, source-tree membership and the candidate. The subsequent live
preflight independently pins the current checkout/claim, current parent/top USB
identity and sixty-second stability, free correct UART, and one-use operations.

## Bounds and remaining gates

Native phase 450 seconds, experiment 800 seconds, host supervisor 890 seconds;
programming 90 seconds, initial loader/handoff 50 seconds, initial launcher
100 seconds, each launch 100 seconds, core close 5 seconds, menu restore
100 seconds and final Park 5 seconds. First failure is terminal, with the
existing two-second receive-only tail and no retry.

No hardware operation was executed to create this preparation record. Separate
live trial, independent cleanup and archived evidence are required. Even a pass
here establishes bounded automatic startup and two selected core cycles;
standalone SD power-on boot, physical current-image HDMI/key actions, native
Ethernet and general DE10-equivalent appliance acceptance remain open.
