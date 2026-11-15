# Contained firmware and FPGA identity/drain endpoint

FES-owned exploratory work on `codex/de25-embedded-os`, based on
`c5189ef504b01276651ee531c81d23db90ff7b1a`. Implementation is
`95016eac34e8a632b034e6e96208d69f0ec9433a`; test environment isolation is
`71d5f8a30`. The result includes the commit containing this record.
Evidence is host software/RTL integration and AArch64 component cross-builds.
No physical hardware, shared kit, GPU or Powerboat process was used.

## What changed

The internal `fes.fabric.embedded.control` definition owns an immutable
128-bit build identity, ABI identity, reader generation and disable/drain
cookie. Its C++ and Verilog consumers are generated together. This adds no
playable ABI or Nano programming profile. No shipped core instantiates it.

The synthesizable endpoint guards one single-ID AXI read port. Disable stops
new reader requests, preserves an already offered address, drains accepted
bursts despite reader backpressure, and acknowledges the exact generation and
cookie only after all tracked work ends. Protocol faults remain sticky; reset
cannot acknowledge an old drain. The freestanding C++ adapter verifies complete
identity and exposes individual control operations. The existing runtime keeps
lifecycle ownership; sequence orchestration here is a host-only test fixture.

The source transformer accepts one exact TF-A v2.12 source blob and produces a
private contained variant. It rejects partial configuration flags, checks bridge
disable before CANCEL/RECONFIG, validates cancellation and transfer parameters,
fixes the inspected null remapper argument, and removes ISDONE's automatic
bridge release. It preserves WRITE/completion address translation and accepted
BUSY semantics. An experimental capability query identifies this variant; it is
neither an assigned Intel interface nor authenticated firmware measurement.
The existing explicit bridge service remains, so this relies on exclusive
trusted-runtime ownership of configuration calls.

## Verification

- The actual patched C configuration functions and actual Verilator RTL ran
  together with simulated platform operations and undefined-behaviour checks.
  Six groups passed: contained identical-image reload; AXI faults/reset and
  generation/cookie checks; identity/firmware/deadline failures; outstanding
  queue and CSR backpressure; forwarding/profile/late responses; simultaneous
  AR/RLAST and 256-beat bursts. No SMC instruction or physical SDM call ran.
- The actual TF-A v2.12 tree at
  `4ec2948fe3f65dba2f19e691e702f7de2949179c`, with the prepared source,
  built `PLAT=agilex5 bl31` for AArch64. The portable adapter also cross-built
  for AArch64 without undefined symbols. BL31 is a firmware component, not a
  complete Nano boot image.
- `make generate`, committed generated-consumer consistency and `make deadcode`
  passed. There are 23 generated consumers and 37 fixture copies.
- The affected parent, host/UI, runtime, contract and firmware/RTL lanes passed.
  The FPGA software lane initially failed because root Make exported a relative
  `PYTHON` path into nested component builds. The runner now removes that
  inherited override while retaining explicit interpreter arguments. All 31
  runner regressions passed, and the isolated FPGA retry passed all 1,368 tests
  (35 skipped) with that environment. The adjacent receipt records its result and
  preserves the original failed outer report; successful unchanged lanes were
  not repeated.

Earlier dependency, generated-fixture and dirty-source provenance failures are
retained in the local logs. Required dependencies were installed privately,
fixtures fixed, and source bytes committed before provenance-sensitive checks;
no provenance or test assertion was relaxed. The adjacent receipt binds source,
commands, compiler/artifact identities and logs. Larger local outputs remain
under this worktree's `out/embedded-*` paths.

## Remaining integration

The Nano backend remains read-only. No production Linux runtime code or playable
shared ABI changed. The new internal contract has regenerated consumers.

Next, connect these per-phase mechanisms to the native runtime through injectable
platform hooks, retaining admitted buffers and the existing failure ownership
rules. A board binding still needs a concrete control aperture, separate control
and data bridge masks/readback, clock/reset/CDC and DDR integration, and proof
that every other master is contained. This endpoint is neither a full scanout
engine nor an address firewall. A deadline cannot interrupt a hung SMC/MMIO call.
Complete boot-chain assembly, FPGA synthesis/timing, and physical identity,
drain, configuration and launch/return acceptance remain pending hardware.
