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


## Kernel capability and current runtime checks

Source `5bf7346481ca2fad27c096da42962086c336177f` builds both complete production
kernels through the pinned Terasic configuration and owner SDK. The actual Image's
embedded configuration must exactly match its retained config. The disabled
[kernel receipt](kernel-static-disabled.json) has no static-startup contract;
the enabled [receipt](kernel-static-enabled.json) declares only
`de25-cold-persona-retained-v1`. Root-manifest construction verifies the actual
Image/config pair before publishing that optional contract. Neither new kernel
has native board-boot acceptance here.

The same runtime source requires that contract from the matched running-kernel
receipt before static programming or PR-register reads. Missing, wrong, duplicate
and wrongly typed values are refused. Its address adapter admits eleven identity
and status reads and two command writes, with ordering barriers; constructing it
opens no mapping. The production factory still does not construct this startup
path. [Native](capability-native.json) and [AArch64](capability-aarch64.json)
builds, archive audits and programmer/controller/register-model suites pass.

The [f7 affected run](static-f7-affected-result.json) passes parent, generated,
host and UI checks, then exposes a missing test-link dependency. Source `2ab7cfc071bb7c7e5bfd730ec2a50b0d752d6565` supplies that dependency. Its full runtime run
reaches the archive guard, which rejects the new word `staged` under its existing
retired-stage-name rule. Source `48084b8b438e1cb66797c9dfb94e5a886a987d36` changes
only that diagnostic wording; the entire unchanged `make -j2 test` then
[passes](runtime-complete-480.json), including all unit suites, incremental/version
checks, strict production/archive/header guards and hardware-support truth. The
guard itself is unchanged. These receipts retain their original source identities.

## Native AArch64 kit CPU and file composition

The bottom kit `TRWU8J8G` executes the actual static AArch64 binaries on two CPU
cores. The programmer, PR-controller and register-adapter model suites pass;
all FPGA/kernel operations within those tests remain injected. The actual fitted
base and empty/16K persona files are also composed twice per selection on that
CPU. All four resulting archives exactly match the previous native/QEMU controls.
Wrong base, wrong descriptor and pre-existing output controls are refused.

The [native CPU proof](native-kit-cpu-composition.json) names the exact `5bf`
unit binaries, `aaf` composer and input/output hashes. The private
[supervisor](native-cpu-supervisor.json) preserves 45-second per-unit and
90-second total bounds, terminates only its own child groups, and proves inherited
two-CPU affinity. Earlier transport and missing-fixture failures remain recorded
in [the validation history](capability-validation.json); the corrected second
actual model attempt passes without changing the binaries, assertions or bounds.

[Authenticated release](native-kit-released.json) confirms the same image `6de`,
bundle `a14a`, software generation 38 and boot, idle and lease-free, with private
payloads removed. This proves native CPU execution and file composition. It does
not prove SDM transfer, physical PR, the new kernel's native boot or normal FES
static-package admission. The whole-core AArch64 RTL emulation matrix remains
incomplete and separate from these results.

## Managed image and SDK workflow

Source `8aa493a80c35342dd222d5215bf1395fda71d84f` selects the immutable owner-SDK
builder for managed factory assembly and virtual boot, extending the already
repaired managed-base selection. The [base verification](managed-base-verify.json)
and real-kernel/rootfs [virtual boot](managed-base-virtual-boot.json) pass. Virtual
boot checks read-only root, temporary writable mounts and D-Bus; physical
peripherals and Terasic boot/reset are outside that proof.

The first factory build correctly refuses a modified runtime. Reproducing the
stock SDK strip operation on a private copy gives exactly the same rejected
bytes ([diagnosis](factory-runtime-strip-diagnosis.json)). Source `467052bc5f088d25acb5c398c9a9740f4efc6deb` preserves the selected `mister-runtime` bytes
in factory configuration. The subsequent factory build passes its exact
executable-hash checks, as recorded below.

Source `e212fd51c0de96bf660207bf0c96a0e1355eed4d` authenticates a retained headless
SDK against its actual Git producer read as data, while requiring the same current
compiler/rootfs/container key. It preserves the original SDK receipt and builder
match. Factory-only changes can reuse that compiler; changed compiler policy,
relabelled recipes, missing producers and poisoned archives are refused.
[Twenty-nine focused tests](SDK-producer-focused.log) and the complete selected
[image/SDK affected plan](image-sdk-e212-affected-result.json) pass.

The actual new normal bundle, matching host, both static target helpers and
[immutable factory inputs](normal-e212-factory-artifacts.json) are built from
`e212`. The packaged runtime's [early-entry probe](normal-e212-early-entry.json)
executes against the real reviewed `6de` loader/libraries under AArch64 QEMU.
No hardware adapter is constructed by that probe. The new factory rebuild from
an immutable `e212` snapshot passes in 777.050 seconds, including exact selected
executable bytes, package/bundle verification and actual kernel/rootfs virtual
boot. Its [factory receipt](managed-factory-e212.json),
[structural verification](managed-factory-verification-e212.json) and
[virtual boot](managed-factory-virtual-boot-e212.json) retain that producer.
The normal release CLI independently verifies and exports image
`d11a2284af6409c8963a5fb20ac29f60a3e39ecb2f2660732708182ea0fff1ea`
with kernel `9ac6c330019fb77d853be835f82f7b9fe8e2e0187ce5610a7b4bae82cf2df91f`;
the normal bootstrap CLI also passes independent two-pass construction.
[Release manifest](managed-release-e212.json),
[release provenance](managed-release-evidence-e212.json) and
[bootstrap selection](managed-bootstrap-e212.json) describe uninstalled regular
files. Native boot of this new image remains open.

Source `603e1ac17f8d849b17ca608a4fe08e9b5c140ba9` fixes media assembly's
rejection of the existing launcher's dedicated port 18789. Actual pairing-tool
output round-trips through media provisioning at the default, dedicated and
boundary ports; malformed endpoints still fail before publication. The source
also records the static next-launch scope in the normal-kit design guide.
The normal bootstrap, provisioned card build and independent card verification
pass in a [143.785-second job](provisioned-card-603-job.json). The
[card receipt](provisioned-card-603.json) binds two matching constructions of
`4d30060bc2218f66a2aaef2f4d8566d2fe07c5db838667874ca487382520c077`
and preserves the existing agent/launcher pairing. Credentials stay in the
private card; this record contains only their digests. Assembly keeps the
authenticated `e212` image producer and actual `9ac` kernel, and rebuilds the
bootstrap against the new assembly source. No block device, kit, boot flash or
current selection changed. The native image trial remains pending.

The exact committed-source [affected plan](launcher-port-603-affected-result.json)
passes in [363.179 seconds](launcher-port-603-affected-job.json): 1,635 parent
cases with 45 reported skips, planner/whitespace checks and generated-consumer
consistency. The earlier uncommitted working-copy run correctly refused the
bootstrap fixture's committed-source identity; its failure is retained in the
private job evidence. Committing the recipe resolves that mismatch without
changing the source-authentication check.

The [canonical native ZX81 regression tail](canonical-zx81-tail-8aa-terminal.json)
terminates at its 3600-second bound. The 1 KiB PAL OpenROM case completes, but
the whole matrix does not. Earlier completed checks remain bound to their
original inputs; this incomplete tail supplies no full regression pass. Its
source matches the original `aaf` FPGA bytes. The retained failure requires a
simulation-cost investigation before another run, rather than an unchanged
replay or a longer deadline.

The [six fitted clock observations](PR-fitted-clock-observations.json) retain
four corners and 32 paths for each fitted revision, with conditional setup/hold
minima of 159.294/159.269 ns. PCB delay, jitter and signal integrity remain
unqualified. Raw PR Design Closure is still failed, no prior exception is
inherited, and no static PR package or physical programming path is admitted.
