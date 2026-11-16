# Native fabric package identity binding

FES-owned exploratory work on `codex/de25-embedded-os`, based on `71354732d`.
The implementation is `d111c3b57`; the result includes the commit containing
this record. Evidence is host software, Verilated RTL, TamaGo/QEMU and a
Nano-targeted cross-build. No board, shared kit, GPU or Powerboat process was
used.

## What changed

The model admission gate now preserves the package's target platform, device,
programming profile, ABI version, producer build ID and programmed-byte digest
with its retained image and activation receipt. It can prepare that image
without initiating a lifecycle transition. The injected tenfoot actor checks the
endpoint identity profile and retained digest before loading it, so an unmapped
package leaves the current reader and session untouched.

An explicit identity profile binds the exact target/profile/ABI-version tuple
to the FPGA control ABI personality. There is no default DE25 profile and no
inference from a GP ABI tag. The endpoint converts the existing 32-digit
lowercase producer build ID to four low-word-first FPGA register values; it
compares every word and the configured ABI personality after programming.
Unpackaged idle/model fixtures continue to use their injected identity hook.
Profiles are validated and copied so later caller mutation cannot change them.
Malformed IDs, duplicate profiles, wrong target/version, receipt syntax and
payload digest mismatches reject. The actual programmed bytes are checked again
before reservation/programming.

## Validation

- The actual tenfoot model actor launched and returned through the patched TF-A
  configuration functions and Verilated FPGA identity/drain endpoint with the
  retained package's build ID. Wrong live identity recovered to idle without
  releasing the target. A package with no matching profile was rejected before
  the old reader was disabled. Cancellation, deadline and transfer failures
  remain covered. Host tests ran under Go's race detector, including the existing
  model actor regressions.
- An asymmetric build ID test verifies all four word positions, and tests reject
  malformed or empty IDs and each mismatched profile selector/digest. The actual
  integration endpoint was compiled with the retained fixture's build ID.
- Six native TamaGo/QEMU diagnostic cases passed, including a bound package and
  unmapped-package rejection. The Nano-targeted diagnostic cross-built using a
  synthetic handoff. Both use software hooks; neither booted a board.
- `make test-changed`, `make check` and `make deadcode` passed for the
  implementation commit. The adjacent receipt records commands, source and
  artifact hashes, and the affected checks. Full local logs remain beneath this
  worktree's `out/native-fabric-binding-*` paths.

## Boundary and next step

The DE10 tuple in the test fixture is model-only. No physical DE25 producer,
programming profile or board address was registered. Existing package identity
and explicit profiles are not signatures or proof that a real bitstream contains
the advertised endpoint. A future producer must actually connect the endpoint
to its sealed build ID and documented ABI personality, and board integration
must qualify control/bridge routing, DDR reservations, all-master containment,
reset, clocks and real drain behavior. Hardware acceptance remains pending.
