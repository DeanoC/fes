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

## Runtime overlay formats

Source `fdbbab3547c96886135e071b728bfbc8cbe86eb8` adds the base and initial-persona formats to the existing runtime generator.
The complete Linux programmer unit suite passes natively (29.652 seconds) and
as a statically compiled AArch64 binary under Cortex-A76 QEMU (27.026 seconds).
All four generated overlays are byte-identical across architectures, and both
existing full-load formats also match the actual pre-change generator bytes.
An independent pinned DTC reader/compiler verifies exact sorted decompilation
equality with the kernel-tested static fixtures after only the test region path
and firmware filename substitutions. The fixture source bytes still exactly
match the tested kernel source.

[Native proof](runtime-overlay-native.json) and
[AArch64 proof](runtime-overlay-aarch64.json) retain the source, binary/tool and
output identities. Their finalization verifies immutable runner outputs and
refreshes the two changing job/log metadata hashes only after the supervisors
exit; original pre-completion receipts remain retained in the named job
directories. This is format preparation: the production programming-profile
dispatch, package admission and physical programming behavior are unchanged.
The base/persona lifecycle and protected startup control remain unfinished.

## Retained two-image startup lifecycle

Source `a7ef20a9a4837d68a3e441caafc4f541c27cdf51` adds `LinuxDe25FpgaManager::ProgramStatic` to the existing
physical programmer. The base and persona digests are checked before effects;
closed read-only staged copies must still match before configuration. Each
selection loads a fresh full base and identifies an untouched cold frozen PR
controller. The existing logical PR client confirms freeze before invoking
the child-overlay callback, then validates identity before resume. The caller
keeps guest execution held and allocates display/starts only after success.

The full programmer and PR-controller unit suites pass natively in the
36.022-second job and as static AArch64 binaries under Cortex-A76 QEMU in
the 31.279-second job. Both jobs also build the production daemon/archive
and pass the unchanged 29-member archive and file-access-owner audit. All
four overlay formats still match the previous native generator byte for byte.
The AArch64 build reuses the existing SDK read-only; compiler executables and
QEMU are checked against the retained hashes before/after execution.

[Native receipt](lifecycle-native.json),
[AArch64 receipt](lifecycle-aarch64.json),
[native programmer log](lifecycle-native-de25_fpga_manager_test.log) and
[AArch64 programmer log](lifecycle-aarch64-de25_fpga_manager_test.log) identify
the actual outputs. Tests cover three fresh A/B/A launches and MENU replacement,
plus 39 refusal/fault/inheritance scenarios: input/staged-byte races, malformed
selection, wrong/non-cold identity, partial or late configfs operations, failed
search restoration, wrong persona, changed retained identities, late child
removal and unknown inherited state. The real logical PR controller consumes
a shared CSR model; configfs programming/completion are modeled file operations,
not SDM transfers or physical partial configuration. The existing PR protocol
suite uses the same unchanged model extracted into a shared test header.

Release verifies both overlays and payloads before any removal and removes
the child before the base. An error or expired deadline after child removal
prevents another base operation; both staged images remain retained. Ambiguity
poisons the context, with no automatic retry, resume or inherited adoption.
The actual permanent-owner lease/mapping veto remains a separate kernel proof.

Production composition admission, a matching flag-enabled kernel, construction
of the protected ordered/bounded register backend and exact-artifact board
acceptance remain open. No new programming profile, daemon operation, public
DMA ioctl, image/boot change or board programming is enabled by this source.
