# Retained CPU/video parts and composed tenfoot sessions

Exploratory change from `d69dd0f2144df0d2e3b8512d1edc36837d7b011f` on
`codex/de25-embedded-os`. The result is the commit containing this record.
Evidence is host software and native AArch64 EL1 QEMU bare metal. No DE25,
physical FPGA, shared kit, GPU or Powerboat process was used.

## Result

`Archive.LinkROMs` now accepts up to two selected CPU/video `PartInput` archives.
It checks their bounds and exact asset IDs, retains private copies, and reuses
existing parts admission and ROM-input composition. Sealed video shells require
an explicitly selected video part. Initial media and other expansion families
remain separate. Filesystem staging and production runtime behavior are unchanged.

The opaque linked object owns the original package, ROM sources, part archives,
pre-ROM overlay and final programmed bytes. Its receipt adds the canonical
`PartsComposition`: exact package/layout, ordered roles/asset IDs, shell digest
and pre-ROM overlay digest/size. The existing ROM receipt separately binds the
final programmed bytes. Copying readers and deep-copied metadata cannot mutate
the object. The same retained activation digest covers all selected content;
no second package identity, linker or compatibility policy is introduced.

The embedded model's restricted driver snapshot now includes application 1.0
with gamepad/fixed video, using generated identities. The existing C++ checker
still decides exact-package compatibility. This does not advertise the factory
Coleco package's HPS DDR, audio, controller-port or display-plane capabilities.
The active session publishes its parts composition only after model release.

## Evidence

The [native receipt](result.json) records compiler/oracle/fixture-producer/native
ELF identities, source, policy, inputs, screenshots and unchanged FAT32 hashes.
The [header inventory](oracle-system-headers.json) is separately hashed. The
[composition report](composition.json) contains the canonical selected tuple,
full receipt digest and distinct overlay/programmed digests. Those identities
match the selected config and released native session.

The [console](boot-1/console.log) passes all **eleven** real tenfoot USB-driven
cases: the previous seven plus composed CPU/video Play/Back and changed,
missing and incompatible part rejections. Screenshots include the
[composed return](boot-1/model-composed-returned.png) and
[changed-part rejection](boot-1/model-part-changed-returned.png).

The fixture uses an existing synthetic Cyclone V blank RBF. Public linker
helpers select blank stored lanes inside the closed CPU/video fences and patch
them into two independently bound part payloads. A separate 24 KiB ROM map
excludes both sockets. Both parts change actual stored bits; the composed
pre-ROM overlay differs from the shell and from the final ROM-linked output.
These are synthetic bit changes, not routed CPU/video logic or playable cores.
No alternate CRAM encoder is added.

The changed-part case is a valid asset with a different recipe/asset ID and
identical payload bytes. Its metadata and archive hashes are internally valid,
but the old activation receipt rejects it even though programmed bytes match.
The missing-video case supplies only the CPU part. The incompatible case has a
valid asset ID but another shell's package identity. Every rejection preserves
the entire current-owner status and issues no programming command. Successful
composition returns to the same title; stale input/frame rejection, save retry
and clean tenfoot/model exit still pass. The final disk is byte-identical.

[Reader race tests](reader-race.log) and the final
[parts-specific race run](parts-final-race.log) compare receipts and output bytes
against actual `StageROMInputV2` filesystem staging. They cover caller archive
and receipt mutation, independent readers, canonical reorderings, changed valid
identity with identical programmed bytes, and rejected missing/duplicate/invalid/
incompatible parts. The [full core-package suite](corepackage.log) passes.
[Model/UI races](model-race.log) exercise the expanded catalog through public
App commands and verify session composition identity. A separate running-session
test rejects each bad-part request while preserving the active composition,
input authority, programming/save counts and an outstanding frame. Vet and repository
dead-code analysis pass. The [standalone admission control](admission-control.json)
still passes 17 admission cases plus three ROM-link golden-vector cases.

## Scope and next board work

This changes the FogCast portable retained-link API and its FES embedded
consumers. Shared definitions, package formats, physical runtime, producer locks
and production launcher are unchanged. There is no durable package install,
restart adoption or physical programming. Model completion/Save is software
observation, not physical quiescence or persistent game state. Native memory,
interrupt, device and concurrent latency acceptance remains open.

The [board adaptation plan](../../../platform/embedded/board-adaptation.md)
recommends starting a Nano backend beside QEMU: boot/console/time/interrupts,
then SDMMC, USB/network, and qualified FPGA/display ownership. Altera's source
HEAD was checked at `66bdcc347564c5698a8997169bce83a31647d618` on 2026-10-29.
Its source and firmware contract are useful references; development-kit memory
maps and a C-driver build do not qualify the Nano or a CGO-disabled Go image.
No real-board drivers are enabled by this change.

Reproduce with the [native guide](../../../platform/embedded/tamago/README.md#cpuvideo-parts-in-retained-activation)
and [host test instructions](../../../platform/embedded/tamago/modelbackend/README.md).

## Repository checks

The [affected-check receipt](test-changed.json) passes planner/whitespace checks,
all 1,107 parent regressions, generated-consumer consistency and the host Go/UI
lane, including the full core-package race suite and downstream session/runtime
consumers. [Focused checks](checks.json) record the retained-parts and model/UI
races, full core-package suite, vet and repository dead-code analysis. No cold
image build, physical deployment or playable-core acceptance was performed.
