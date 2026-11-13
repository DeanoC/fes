# Native initial-tape platform hook

Source `e70fc71a3` connects initial media delivery to the existing computer
driver, native session and Tenfoot launch owner. `OpenWithInitialTape` selects
one trusted bounded loader; `LoadInitialTape` requires the caller's Hold and
passes private bytes plus the selected identity/current GPO/CRC. Size, CRC,
unchanged source and the exact final identity-query GPO/toggle parity must match
before the driver publishes the new protocol state. Context/clock/receipt
failures retire the handle without Run, replay or ordinary-protocol fallback.

`InitialTapePlatform` is an optional trusted CPU service. The native session
serializes it against closure and checks matching live bridge ownership before
and after. The service's existing held-fetch observations and whole-sequence
one-second limit remain. Ordinary constructors retain the CSR/model path, and
ordinary `LoadTape` keeps its execution-preserving behavior even when a secure
initial loader was selected. No shared ABI, FPGA source or timing changed.

The actual C engine now receives the caller's bytes and owner/generation through
the host-only captured-RTL supplier. Both the computer driver and native session
consume its receipt through their real hook APIs; the former manual test-only
assignment of driver GPO state is removed. Driver tests cover four sizes through
16,375 bytes and five C failures. Native-session tests cover three sizes through
16,375 bytes, five C failures and ownership loss after a completed transfer.
They read every tape byte, require stable held fetches, then test native Run,
simulated fetch progress and owned closure. Native bridge/CPU ownership services
are injected; this is not execution of the real TF-A ownership supplier.

Tenfoot and retained-store tests exercise both ordinary and platform delivery.
Both complete repeated launch/return, preserve privately retained bytes after
card/source mutation and clear the returned tape status. A failed platform
completion reaches the actual App, keeps execution held, refuses further
launch and leaves storage contained. Additional injected cases cover lost
ownership before/after the call, malformed receipts, source mutation,
cancellation, deadline/backwards clock and serialization. No failed hook can
retry through the ordinary transfer or issue Run. Platform delivery is modeled
in these App tests; the separate native-session tests execute actual C and RTL.

All nine affected commands pass, including 1,693 parent regressions with three
existing prerequisite skips, generated-contract consistency, computer tests,
native lifecycle/firmware/RTL integration and producer checks. The standalone
captured-computer check passes on the final source. A fresh AArch64 bare-metal
QEMU Play check also passes the existing model's launch/input/return and failure
cycles with its USB/VirtIO/FAT32 stack. It uses synthetic DE10 fixtures and does
not establish DE25 support or physical admission.

Raw source-bound results are under `out/nano-native-tape-hook-20261108/`.
The Nano `FabricCPUTransport` does not yet implement `InitialTapePlatform`:
the fixed uncached publication lease, descriptor/firmware channel and trusted
cold/USB/CPU supplier still need implementation. No physical fast path or new
hardware image is enabled. The original tape deadline trial remains consumed;
its failure is not repaired or qualified by these host/model results. Physical
timing and full appliance readiness remain open.

Next connect that source-window lease and the real secure supplier under the
existing SiP lock, following the secure ROM-step ownership pattern. Bind the
captured identity to the active selected program rather than trusting request
metadata. Test the joined firmware/native/App success and failure paths before
building another matched chain or attempting the board again.
