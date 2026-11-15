# Fresh Nano ROM package and matched native file chain

Producer source `505993c08155d71794bacd1260155ace736fe19a` builds a fresh
format-three ZX81 ROM diagnostic package, not a relabeled older artifact.
The build identity is `edafcc95c494b73f59492f3a060bf095`; the package identity
is `7f1d138774d7f0a0212f80184a6349d726f885095c86d6360e380817cedae566`.
Its separate 8 KiB firmware input is extracted from the previously verified
captured board image. The existing retained linker derives its activation
receipt. No firmware bytes are inserted into the FPGA payload or package.

The 3090 Quartus build held the shared lock, used four processors and completed
all ten unchanged 120-second phase bounds. The longest phase took 109.5 seconds;
the whole producer took 577.1 seconds. All 1,864 transferred project files were
read back and hashed against the remote manifest. The actual generated-project
model passed 25 DDR cases and three identity/window rejection cases. These
results do not qualify timing or the physical boot profile: the producer keeps
`timing_acceptance` and `boot_firmware_compatibility` as `not-qualified` and
`hardware_acceptance` as `not-run`. No constraint or seal was relaxed.

Native source `83c5f5d12` builds the actual SDK ELF, entry, BL31 and U-Boot/SPL
chain. All four bounded builds passed. The compiled ROM map agrees across the
SDK, entry and cold profile. Actual ELF call sites select the ROM verifier and
version-two descriptor encoder. BL31 contains the strong ROM provider, private
ROM state and descriptor capture; its compiled cold header selects the 8 KiB
ROM aperture. These are code/identity checks, not evidence of execution.

The SDK's actual catalog, package, separate firmware and metadata pass through
the retained store and `ProgramLoader` in the opt-in artifact test. Both exported
choices preflight without a firmware call or SD containment, using a modeled
active SD lease. All 393 first-party compiled source files match their Git blobs.
The 1,681 cached SDK/module sources also match the build receipt. All 2,074
compiled inputs are separately archived and read back. The initial source audit
incorrectly treated cached SDK files as Git inputs; that failed classification
is retained alongside the corrected separate checks.

Composer source `75a5095d6` independently rechecks the ROM resource against the
sealed FPGA project. Six file-composition tests passed, including missing,
changed and out-of-profile ROM rejection before conversion. The real composition
held the Quartus lock and passed. Its SPL is independently decoded, FIT/entry/
BL31 receipts agree, and the split FPGA core payload remains byte-identical.
It records `held_rom: true` and the same ROM-resource digest. The full matched
chain is retained at `/home/deano/fes-retained/de25-rom-matched-chain-20261105-r1/`.

No board, SD card or flash operation ran. TRWU8CBL's previous parked state and
continuing claim are unchanged. The next step is to extend the existing bounded
operator procedure for the separate firmware upload and ROM receipt, validate
its failure fixtures, then run a first physical ROM launch/return test. Physical
ROM acceptance, ordinary FES package admission, native video/network/media and
full DE10-equivalent loading remain open.
