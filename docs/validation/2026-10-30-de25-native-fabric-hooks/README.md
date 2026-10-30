# Native runtime with contained firmware and FPGA hooks

FES-owned exploratory work on `codex/de25-embedded-os`, based on
`ebe46a19d`. The implementation is `521c6bc01`; the result includes the
commit containing this record. All evidence is software-only: host integration,
TamaGo/QEMU execution and a Nano-targeted cross-build. No physical hardware,
shared kit, GPU or Powerboat process was used.

## Scope and behavior

The existing lifecycle controller supplies the outgoing reader identity and
display generation, and reserves the next display generation. The new Go
endpoint executes its commands through injected CSR, firmware, DMA reservation,
bridge and save hooks. It checks the contained-firmware tuple before disabling
the reader; waits for matching drain acknowledgement; requires all-master
containment; transfers retained bytes using the reviewed full-configuration
TF-A protocol; verifies the entire FPGA build/ABI identity through a
control-only bridge; then enables the new reader and separately releases data.
One transfer write may be outstanding, and accepted BUSY is not replayed.

The same endpoint can be injected into the existing tenfoot model actor; that
actor still owns admission, save ordering, generations and recovery. Unknown
completion, cancellation, bridge, clock or register outcomes retain ownership
and require reboot. A known identity mismatch can use the controller's bounded
idle recovery. Actor work per tick is bounded, and a slow platform call does
not authorize a later release after the deadline.

The internal shared fabric definition now generates a Go consumer alongside
its C++ and Verilog consumers. This adds no playable GP ABI, Nano programming
profile or physical board binding. The normal model backend remains the default;
`NewWithEndpoint` requires explicit hooks and the existing model-only policy.

## Checks and evidence boundary

- The host verifier built the actual pinned patched TF-A configuration functions
  and actual Verilated FPGA endpoint, using simulated memory, mailbox and bridge
  operations. Native Go drove the complete boot, launch and return sequence.
  It held an accepted FPGA read until drain, rejected unknown firmware and a
  foreign completion, and recovered from wrong identity without releasing the
  requested target. It did not execute an EL3 SMC or access physical SDM.
- Tenfoot's actual renderer/client and model actor launched a locally retained,
  linked package through the injected endpoint and returned to idle. A separate
  run exercised wrong identity, and cancellation during an unresolved transfer
  retained its reserved buffer. These tests ran with Go race detection. The
  existing software-model actor's seven regression tests also passed under race.
- TamaGo/QEMU executed four injected-hook diagnostic cases: launch/return with
  drain, unknown firmware, foreign completion and identity recovery. This is
  native Go execution with software devices, not board timing or boot evidence.
- The Nano-targeted `fabric` diagnostic cross-built using a synthetic handoff.
  Symbol inspection found the new native endpoint and no board SMC entry.
- `make generate`, `make check`, `make deadcode`, focused Go/Python checks and
  `make test-changed` passed for the implementation commit. The latter records
  the affected parent, host, runtime, contract and FPGA software checks.

The adjacent receipt binds the tested source, tool/artifact identities, exact
commands and log hashes. The host verifier selected the module's Go 1.26
compiler after an initial system-Go mismatch. An earlier full tenfoot trial
exposed excessive delay between busy polls; bounded per-tick polling fixed it
without extending the existing lifecycle deadline.

## Remaining integration

The physical platform must supply and qualify the CSR aperture, control/data
bridge masks and readback, clock/reset/CDC, coherent DMA reservation, and
containment of every master. The live build/ABI mapping must be bound to a
qualified producer and admitted package. The endpoint covers one AXI read port,
not a full display or address firewall. Real save/input/frame handoff, complete
boot-chain assembly, FPGA synthesis/timing and hardware launch/return remain
open. The firmware capability tuple is experimental and is not authenticated
firmware measurement. The Nano backend remains read-only.
