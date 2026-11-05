# DE25 static expansion: off-board startup proof

The requirement matches DE10: select an expansion for the next core launch.
DE25 composes a base plus the selected persona on AArch64, loads both while
execution is held, then starts the core. Live expansion changes are outside
this milestone.

The default-off kernel candidate defers display ownership until the initial
persona completes. A failed attempt cannot admit allocation or retry a second
persona against the same base. A completed persona cannot be replaced until
ordinary Stop and a new full base configuration. Existing allocation leases,
uncertain client storage, supplier protections and the DMA UAPI remain intact.

The kernel source tested here is `fcf2c6dbc376dda29c9f07344015ad92f01a9829`.
It uses the pinned Terasic kernel, GCC 9.4 SDK and immutable builder in the
[dependency lock](../../../image/kernel/de25-dma/linux.lock.toml). These are
real AArch64 KUnit kernels under QEMU, actual OF overlay transactions and owner
ioctl callbacks. Firmware transfer and register reset are scripted fixtures;
SDM, physical PR, SMMU streams and DDR visibility are not modeled.

| Kernel selection | Result | Evidence |
| --- | --- | --- |
| Default, static expansion disabled | 49/49 pass; startup refused before programming | [receipt](disabled-checks.json), [run](disabled-kunit-run.log) |
| Explicit `--static-expansion` | 53/53 pass; five startup lifecycle cases plus existing cases | [receipt](enabled-checks.json), [run](enabled-kunit-run.log) |

The runner verifies the final configuration, exact complete case list and no
skips or kernel warnings. Eleven focused Python tool tests also passed. The
locked builder is selected by immutable image ID and initializes the build
volume with its existing user ownership. The two earlier pre-compilation
failures and their operational repairs remain identified in
[validation.json](validation.json).

The FPGA and composer source is separately identified as
`aaf5155eed2f77683742f96d1a95ee83dfd4b235`. Its primary and independent fits
passed the existing compiler, constrained internal timing, partition reuse,
integrity and metastability gates. All three RBF payloads are byte-identical.
The [comparison](independent-fits.json) retains distinct raw manifests, source
seals and opaque QDB identities. Its narrow generated-input audit establishes
only the reported path/bytecode differences; no source seal was rewritten or
opaque QDB difference waived. Each revision still has the four raw Design
Assistant finding groups and failed whole-board Design Closure. No internal16K
review exception is inherited and no normal package is admitted.

The [composition proof](composition-proof.json) executes the existing composer
natively and under Cortex-A76 AArch64 QEMU against those actual primary base,
empty-slot and 16K payloads. Repeated bundles match and all 12 rejection cases
pass. This is separate from the incomplete whole-standard-core AArch64 BASIC
emulation matrix and from native-kit CPU or FPGA acceptance.

No board, image, boot configuration or flash changed for these tests. Normal
runtime base/persona loading, protected startup control, admitted package
selection and exact-artifact board acceptance remain required.
