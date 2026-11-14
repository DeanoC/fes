# Bounded recovery download-return observations

The [consumed hardware attempt](../2026-11-07-de25-hdmi-native-trial/README.md)
stopped after its fifth TFTP transfer reported all bytes but before its command
result/prompt. Native programming and core loading were never reached. This
change prepares targeted observations for a new recovery diagnostic; it does
not identify the stalled call or retry that attempt.

Implementation source: `dfd4863c02f571c0875cd5c30b5163d9f58a6eef`.

## Observation and source contract

The explicit `--network-recovery --network-return-trace` builder selection adds
34 ordered before/after markers around transfer-count printing, filesize and
fileaddr updates, Ethernet/XGMAC/PHY shutdown, net-loop return, environment
updates, autostart handling and final command return. The flag is off by default
and rejects without the separate recovery profile. It enables no native handoff.

The transform pins four inputs after the existing PHY preparation. Removing
only the added statements/header/build entry reconstructs every original byte.
The unchanged PHY validator then checks the reconstructed private tree. Foreign
inputs, changed original calls, changed helpers, edited limits and fabricated
self-consistent hashes reject. No vendor stop, register operation, status,
retry, timer deadline or return value was replaced.

The selected UART helper uses the existing `0x10c02000` transmit and
`0x10c02014` status registers. It allows 100,000 polls per wait and 96 bytes per
string, then parks on uncertainty. It calls no timer, heap, SD, PHY or console
framework service. Scope starts only in a successful nonempty TFTP return and
closes at command return. Nested scope parks. Other network protocols emit no
markers. A decoded complete trace grants no operation or command success.

## Completed checks

Sixteen focused recovery tests pass, including seven new trace tests. Host C
execution compares the actual vendor success/return and XGMAC stop blocks
before and after insertion, including modeled busy-drain, absent-start/PHY,
non-TFTP and autostart-error paths plus injected callback stops. Callback order
and final modeled state match. Separate tests compile the actual UART helper
and exercise normal, stalled transmitter, stalled drain, overlong string,
nested scope and inactive behavior. Partial, reordered, duplicated, malformed
and excessive console records cannot complete a trace.

The busy-drain path can finish its unchanged loop at its time bound. The
autostart-error path can emit the final marker and still return failure.
Those cases deliberately demonstrate that marker completion proves neither
idle DMA nor command success. Host callbacks model MMIO/timers; these are not
physical Ethernet or UART results.

The full affected run passes 1,590 parent tests (three skips), planner checks,
generated consistency and the selected native-fabric/control/producer checks.
The selected compiler-tooling suite passes 1,590 cases (78 skips).
See `result.json` and the full logs for exact suite scopes.

## Fresh compiled artifact

Fresh strict BL31 and the traced U-Boot/FIT/SPL builds pass their unchanged
480 s bounds. The initial BL31 invocation omitted its required cross prefix;
the argument parser rejected it before building. That log/receipt is retained
separately from the successful corrected invocation.

The actual ARM ELF links all three public trace helpers and the selected
network call sites. Disassembly confirms the 100,000-count UART wait, 96-byte
string comparison, fixed UART addresses and terminal WFE loops. The audit
records exact linked function bytes. This is static linked-code inspection,
not execution of the ARM artifact or proof of a physical fault location.

The new FIT is 954,288 bytes:
`0f15b11576e91a6cb705de1f1b97e0960a03437b1821628abbd74bd4367f9855`.
Strict BL31, SPL and recovery configuration are byte-identical
to the qualified baseline. The existing qualified SOF can therefore remain a
delivery input; this step performs no FPGA implementation build or PFG/JTAG
operation.
The default recovery package validator rejects the traced source, as intended.
The historical qualified recovery/source validator still passes unchanged.

## Remaining before hardware

The file-only delivery plan has `execute=false`. Separate trace-aware package
validation, regular-file manifest/card preparation and a UART-only upgrade
operator remain necessary. The earlier UART upgrade prototype needs the exact
current PHY-prepare banner, fresh rollback filenames, current expected card/
package/experimental FIT and ownership/memory/first-failure fixtures. A fresh
live card/files proof is mandatory before any write. The original factory card
and protected rollback files must remain intact.

TRWU8CBL's continuing claim is retained. The previous recovery prompt remains
unconfirmed; this milestone opens no UART, JTAG or device, performs no SD write
and does not restore or retry the board. The bottom kit is untouched. A new
matched diagnostic and operator must consume a new bounded attempt. Physical
core loading, menu/video/input/media/network acceptance and timing remain open.

Raw evidence: `out/nano-recovery-return-20261107/`.
Retention target: `/home/deano/fes-retained/de25-recovery-return-20261107-r1`,
layered on the prior physical-stop and matched native-chain archives.
