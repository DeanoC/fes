# Source-bound native computer scope and matched boot candidate

The computer-control driver is connected to the native CPU and secure firmware
through the existing owned bridge hooks. Code source is
`5b00f0486576bb192e3de1cb09569c59902cca2b`, based on
`5e6ad1d7b3e15c0335de91e19005fc7eabe4dca3`. This record changes documentation
only. The [input verification](input-verification.json) passes on the exact
compiled source. Hardware is not accessed by this work.

## Actual platform path

`hps_context.computer` derives the computer's aperture, clock/reset routes and
internal-RAM parameter from the checked generated FPGA hierarchy. It matches
the captured computer source and ABI, rejects another master/capability layout
and binds its descriptor to the same control-map digest, handoff, package and
build ID. The selected port is `0x20010300`, span 32. The FPGA package remains
`07b2d01003dc92e4993ba7f72761444ae801a1a005f87e0a1b65f9aad9dfeb7d`, build
`d2435ab5d8221892162a1044d81dfb76`; its payload is unchanged.

`HPSContextVerifier.OpenComputer` requires the original closed cold source,
opens only the control bridge and checks exact live owner/generation before
and after every CSR operation. Close first forbids new Run/media/input writes,
requests Hold through a usable driver, retires MMIO, performs the existing
complete master-stop/bridge closure and rechecks secure source identity.
Unknown writes and closure are not replayed. A retained client cannot access
registers after Close. Hold acknowledgement is recorded separately from
independent CPU-stop evidence.

The explicit `fpgaload --computer-control` build carries that descriptor through
SDK, entry, SPL and BL31 validation. Its firmware mode admits one control-only
opening after the exact configuration completes and fresh read-only SDM
readiness checks pass. It uses the established secure probe/platform callbacks,
including complete master stop at Close. While active, only ownership snapshots
and the expected held close are accepted. SD, configuration and data release
reject. Other cold profiles keep their original restrictions. This adds no
ordinary runtime profile or `BootHandoffVerifier` implementation.

## Tests and build

The [joined model check](model-result.json) uses the actual captured Avalon,
mailbox, Z80 and machine RTL. The Go session drives it through physical-address
hooks and owned firmware-wire observations, including run, tape upload/eject,
hold and reclosure. [Model output](native-control.log) includes the driver
tests, ownership failures, identity mismatch, uncertain opening/closing,
stalled ACK, revoked access and closing/Run exclusion. This models the firmware
wire; it does not replace the separate actual-C checks.

The actual scope guard and owned C bridge codec pass 21 direct variants. The
joined strong cold provider, pinned SHA implementation, configuration engine
and exact SiP dispatcher also pass the new enabled/disabled scope variants,
including actual board target callbacks, snapshots during control access,
blocked SD/configuration, uncertain open, failed close and stale generation.
The existing strong-provider/SD/configuration/reader-RTL paths still pass.

All 12 [selected software scopes](software-result.json) pass, including 1279
parent tests in three intact-module partitions capped at 300 seconds, native
driver/SD race checks, actual firmware/RTL integration and FPGA software checks.
Selection and complete partition coverage remain under
`out/nano-computer-native-20261102`. The first joined model run correctly rejects
a test input changed during compilation; its build/test logs remain in
`out/nano-computer-scope-model-20261102`. The final v3 model has stable inputs.

The fresh [native SDK](native-sdk-result.json),
[entry image](native-entry-result.json), BL31, SPL/U-Boot and
[HPS-first composition](native-composition-result.json) build successfully.
The [matched build](native-build-result.json) rechecks selected SDK/entry
sources and the one-shot computer mode. Native boot ID is `99332a5781433e12`.
This is a cross-built candidate, not a new boot observation. The original
20-second configuration deadline is unchanged and data bridges stay held.
The native program explicitly prints `execution_observed=0`.

## Compiled startup selection

The first selection checker uses an incorrect installed-header path and stops;
its [path failure](selection-path-failure.log) is retained. The corrected
checker then rejects the old exact linked-byte/address comparison with the
earlier DDR build. That [failure](linked-byte-layout-failure.log) is retained
and is not relabelled passed. This image omits the active DDR recipe, changing
linked code/data placement.

The [final selection proof](selection-proof.json) verifies the actual computer
firmware mode and phase state, strong platform/stop symbols, exact pinned vendor
assembly and unchanged entry executable object sections. For all six startup
functions it verifies exact pre-relocation instruction bytes and identical
relocation sites, types, symbols and addends. Every changed linked word lies
at such a site. The [relocation audit](relocations.py) also checks unchanged
non-relocation instruction bits, branch destinations, ADR/ADRP targets,
ADD/load offsets and exact merged string literals against the actual linked
ELFs. No startup source, timing or seal check is relaxed.

The actual SPL loaded-BL31 size/digest seal, barrier/checkpoint order and
contained-console call graph also pass. The original failed comparison remains
part of the evidence; the passing [audit result](relocation-verification-result.json)
establishes relocation-equivalent startup, not unchanged absolute addresses.

## Remaining qualification

The [previous physical fixed-read/drain result](../2026-11-02-de25-contained-console/README.md)
remains the hardware baseline. This candidate has not booted on TRWU8CBL and the
new EL1 computer aperture is not physically qualified. The next work is its
bounded serial test/decoder and controlled board run. Independent core-execution
observation, ordinary runtime package admission and Tenfoot launch/return remain
open. A successful command ACK does not establish playable-core operation.
