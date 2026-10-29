# Verified packages admitted to the native lifecycle model

Exploratory FES base-system change from
`c8fec7acf677b9080cef1c17d0acf335524f8848`. The result is the commit containing
this record. Evidence is host software and AArch64 EL1 QEMU bare metal only.
No DE25 hardware, shared kit, physical FPGA, GPU or Powerboat process was used.

## Result

The native `admission` image reads canonical packages from FAT32 through the
existing `corepackage.RetainArchive` API. A restricted model gate checks target,
profile/ABI pairing, version and required interfaces before looking up an exact
package-ID decision from the existing C++ runtime compatibility checker. It
calls the existing single-owner lifecycle only after admission succeeds.

The host-only oracle builds the actual `OpenCorePackage`,
`CheckCoreCompatibility` and error-name implementation. Generated runtime
constants supply the model's simple-game/gamepad/fixed-video contract identities.
There is no second Go implementation of C++ compatibility semantics. The
oracle binary hash binds the producer in the runner-owned policy snapshot.
The snapshot is trusted fixture data, not a signature or production receipt.
The gate has only a software-model load method; there is no physical backend.

Admitted payloads remain private and bind the full package ID, core ID, ABI
ID/version and build ID to simulated identity verification. Release publishes
that binding and new input/display generations. An identity mismatch starts
bounded idle recovery before publishing the new package. These are simulated
observations, not physical GP responses.

## Evidence

The [native receipt](result.json) records compiler/QEMU/oracle/ELF hashes,
module versions, compiled source and compiler-reported FES/vendored headers,
commands, policy, cases and unchanged final FAT32 hash. The compiler-reported
[system header inventory](oracle-system-headers.json) is separately hashed in
that receipt. The [console](console.log) records **17 cases: seven admitted,
ten rejected**, all matching explicit fixture expectations:

- Eleven structurally valid shared format-2/3/4 entries, including an unknown
  ABI that is valid for inspection and rejected for model activation.
- Missing mandatory gamepad, a newer ABI, diagnostic-only profile and unknown
  required interface reject. Unknown optional extension accepts.
- Optional persistence with invalid requiredness rejects through the C++
  semantic decision, despite passing required-capability preflight.

Every case begins with an outstanding display frame. Rejections preserve the
entire current-owner status and frame token, issue no commands and leave the
frame finishable. Accepted replacement saves its previous owner, waits for the
frame to finish, then verifies/programs/releases model bytes. The complete
package identity matches the retained archive after every successful release;
old input generations reject. Additional native checks reject nil/zero gates,
missing package decisions and a wrong expected package ID without changing the
owner. A full build-identity mismatch recovers to idle without publishing the
package.

[Host race checks](admission-race.log) pass on these same generated fixtures and
cover copied policy ownership, independent returned metadata, target/profile/
ABI/interface rejection and invalid or duplicate policy entries. Focused Go vet
also passes. The [92-case native inspection control](packages-control.json)
still verifies all original cases, source-mutation rejection and restoration of
the original disk. The [standalone fabric control](fabric-control.json) passes
its eleven lifecycle invariants after extending model identity.

## Limits and next integration

This is model admission informed by the existing runtime compatibility checker,
not the full physical runtime admission path. The latter additionally requires
real driver resolution, composition and linked-ROM receipts, board DDR layout,
display adapters, containment and live identity. The C++ checker remains on the
host; its native port is still required. No tenfoot Play action is enabled.

Only format-2 simple-game 1.0/gamepad/fixed-video is modelled, with a 1 MiB
payload bound. Format-3/4 packages reject because this backend cannot provide
linked activation. The test target is the existing DE10-Nano corpus target;
no DE25 package support or hardware acceptance follows. Maximum-size production
package memory/latency and physical device access remain unqualified.

All code changes are under FES `platform/embedded`; the existing runtime,
FogCast validator, shared definitions and producer pins are unchanged. Next
hardware-independent integration is retained ROM linking and identity-bound
activation receipts. Board firmware, resources and physical lifecycle acceptance
remain separate work when the Nano arrives. Reproduce with the
[native run guide](../../../platform/embedded/tamago/README.md#runtime-admission-of-verified-packages).

## Repository checks

The [affected-check receipt](test-changed.json) passes the planner regressions,
parent regressions, whitespace and generated-consumer consistency against the
base above. Only parent-owned platform code changed, so no component or FPGA
simulation lane was selected. [Focused checks](checks.json) also pass the race
suite, vet and repository dead-code analysis. Cold image builds and physical
acceptance are outside this hardware-independent scope.
