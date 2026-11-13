# Retained ROM links and activation receipts

Exploratory change from `dab234a6760ea60471c14bcce6f9fed6893a12aa` on
`codex/de25-embedded-os`. The result is the commit containing this record.
Evidence is host software and AArch64 EL1 QEMU bare metal. No DE25 board,
physical FPGA, shared kit, GPU or Powerboat process was used.

## Result

The existing `corepackage.Archive` now offers `LinkROMs`: format 3 accepts its
one declared ROM, and format 4 accepts BIOS then cartridge in sealed descriptor
order. IDs, roles, lengths and source count must match before private allocation.
Linking uses the retained package's map and the same `linkPreparedROMInput` /
`linkPreparedROMInputV2` implementations as ordinary target staging. Callers
cannot supply maps, programmed bitstreams, expansion overlays or receipts.

The opaque `LinkedArchive` owns the package, copied ROM sources and derived
programming bytes. Returned descriptors/receipts are independent, and source /
programmed readers copy into caller buffers. The content receipt binds package,
map, ordered source identities (IDs, roles, hashes and sizes), and programmed
hash/size. Its identifier is SHA256 over the domain
`FES-RETAINED-ROM-ACTIVATION-1\n` followed by the receipt's JSON encoding. This
identifies retained content; it is not an authenticated or durable install token.

The model's `LoadLinkedModel` requires both the expected package ID and the
expected receipt digest, plus the opaque object. It still checks the existing
host C++ oracle's exact-package compatibility decision and the restricted model
driver. It verifies programmed bytes before any lifecycle transition. Model
identity and released status include the full receipt and programmed digests.
A wrong live-model receipt starts bounded idle recovery before package release.
Raw format-3/4 loads remain rejected.

## Evidence

The [native receipt](result.json) records compiler, oracle, native ELF, compiled
source/header hashes, generated cases, golden output identity, disk hashes and
commands. Its separately hashed [system header inventory](oracle-system-headers.json)
records the host oracle's compiler inputs. The [native console](console.log)
passes the original 17 admission cases plus three linked cases:

- Format-3 firmware.
- Format-3 cartridge.
- Format-4 BIOS and cartridge.

These use existing **synthetic** Mistral ROM fixtures, with actual CRAM framing
and checksums. Each programmed output equals the ramp golden vector:
`9df2faea863aa537564d1e0dffd981212003a349e44ee129a66679c95f21f555`,
1,971,149 bytes. They contain no routed playable core or machine ROM.

For every case, the native program changes a runner-owned source ROM on FAT32
and independently links the changed bytes. The changed source produces a new
receipt, while the original retained source/output remain unchanged. Mutating
returned receipt data and read buffers cannot alter the original object.
Activating a changed object with the original receipt, or the original object
with the changed receipt, rejects without changing the owner or frame. Wrong
package IDs, nil/zero handles, raw ROM packages, missing sources and wrong roles
also reject. Correct activation waits for frame quiescence and publishes the
complete original identity; a later simulated live receipt mismatch recovers
to idle. Source files are restored, and the entire disk's final hash equals its
initial hash.

[Host race checks](embedded-race.log) independently run those fixtures and compare
**all receipt and programmed digests** with native results. The portable lifecycle
and admission regressions also pass. [Core-package race tests](retained-rom-race.log)
compare format-3/4 retained outputs byte-for-byte with actual filesystem staging,
then test caller-buffer/receipt mutation and distinct ROM identity. They also
reject missing/extra/wrong sources, wrong roles/sizes, cancellation, nil context
and an uninitialized archive. The full [core-package suite](corepackage.log) passes.

## Limits and next integration

Sources live as long as their opaque RAM object; the model owns a separate
programming-byte copy through transition/recovery. There is no durable package
installation, restart adoption or power-loss guarantee. Model buffers now allow
8 MiB to accommodate these approximately 2 MiB vectors; raw format-2 loads keep
their 1 MiB bound. Production-sized packages, heap pressure and concurrent
UI/network latency during linking remain unqualified.

This extends the FogCast-owned portable package/linker API and the FES-owned
embedded diagnostic. Existing filesystem staging/linking behavior, production
runtime, shared hardware/protocol definitions, producer locks and package
formats are unchanged. No extra registry or physical backend is introduced.
Compatibility still uses the host C++ oracle's trusted runner-owned model policy.
Nothing here authorizes a Cyclone V bitstream on an Agilex board, and no physical
GP observation or DE25 acceptance follows.

Expansion/parts composition receipts and tenfoot launch integration are the
next hardware-independent boundaries. Real board resources, containment,
programming and live identity remain separate hardware integration. Reproduce
with [the native run guide](../../../platform/embedded/tamago/README.md#retained-rom-linking-and-activation-receipts).

## Repository checks

The [affected-check receipt](test-changed.json) passes planner checks, whitespace,
all 1,106 parent regressions, generated-consumer consistency and the selected
host Go/UI lane. The host lane includes the full core-package race suite,
runtime-adapter consumers and tenfoot regressions. [Focused checks](checks.json)
also pass core-package/embedded races, vet and repository dead-code analysis.
No production image or playable core was built or deployed.
