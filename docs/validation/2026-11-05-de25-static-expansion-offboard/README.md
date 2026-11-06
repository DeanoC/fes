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
Image/config pair before publishing that optional contract. At this stage both
native boots were pending; the enabled kernel's later ordinary native image
trial is recorded below. The disabled kernel still has no native boot proof.

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
[bootstrap selection](managed-bootstrap-e212.json) initially describe uninstalled
regular files. The later native trial below tests this exact image and kernel.

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
bootstrap against the new assembly source. This assembly job changes no block
device, kit, boot flash or current selection. The subsequent approved physical
card operation and native trial are recorded below.

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

## New Linux image: native ordinary-core trial

The approved spare-card trial on bottom board `TRWU8J8G` boots the exact `d11`
image, `9ac` enabled kernel and `e212` factory bundle/host above. The original
working card stays separate. The first boot correctly fails the production
storage gate because the fresh two-partition card has no `FESDATA3`; the
[original failure](native-d11-missing-data-failure.json) is retained. No launch
test bypasses that gate.

Sources `4e3a603d49ad66719b83b30fe904453d0d7e682b` and
`0b7d412da1151364b0b97ebca32f754874fb4f49` add DE25 data preparation to the
existing `appliance_expand_dry_run.py --board de25-nano` entrypoint. The normal
reader operation creates only the third partition and full ext4 filesystem in
unused capacity. It verifies the exact removable reader and card, unmounted
state, preserved MBR boot bytes/existing descriptors, unchanged current raw
FAT/bootstrap partition hashes, full filesystem check and an independent
receipt verification. No kernel, image, FPGA or boot flash is rewritten.

Eighteen focused cases and the exact committed
[affected plan](data-0b7-affected.json) pass, including 1,611 parent cases with
six skips. The [regular-file proof](data-0b7-file-proof.json) uses actual ext4
tools and the captured booted bootstrap, rejecting changed payload, ownership,
mtime and dirty state. Reference admission excludes only validated ext4
mount/read bookkeeping; the actual current raw partition bytes remain protected
before/after the operation. The [physical reader result](data-0b7-physical.json)
passes in 309.343 seconds. Native boot then mounts the 29,085,401,088-byte data
volume read/write and reports durable factory selection with no storage error.

The [native trial record](native-d11-trial.json) binds exact sources, artifacts,
boot epochs, receipts and capture hashes. Ethernet and serial-pinned SSH pass
with the actual kernel/runtime identities. A matching isolated `e212` host
uses the existing pairing and separate catalog/state. The qualified standard
OpenROM internal-16K package passes ordinary
[import/admission/launch/Stop](native-d11-package.json) and
[library tape delivery](native-d11-media.json). Full HDMI captures visibly show
[LOAD/RUN 7](native-d11-run7.png), [edit/RUN 9](native-d11-edit9.png), then
[reload/RUN 7](native-d11-reload7.png). These different pictures establish that
the capture updates; [MENU is visible after Stop](native-d11-menu.png).

One ordinary [software reboot](native-d11-reboot.json) passes without touching
power, returning a new stable ready/idle/free epoch with freshly authenticated
kernel/runtime/data identity. The first post-reboot normal ZX81 launch again
loads the tape, visibly [runs 7](native-d11-postreboot7.png), and stops normally.
This tests reboot after an ordinary Stop; it is not a forced-reset or
active-session reboot campaign. Local fixture and observation-wrapper errors
remain identified separately in the trial record; no failed hardware command
is replayed to obtain these results.

Restoring the original working card exposes a separate failure: its FAT
filesystem reports `fat_free_clusters: deleting FAT entry beyond EOF` and
remounts read-only, preventing FES services from starting. UART authenticates
the original card, kernel and `6de` image; the cause is not established. The
board is shut down cleanly and the original card is returned to the reader for
offline inspection. The original host service/configuration/catalog are
preserved and restored. The tested trial card is retained; no baseline is
promoted.

The [offline repair proof](original-card-repair-proof.json) first retains a
verified FAT/bootstrap/MBR snapshot. Automatic repair adds a recovered file and
is rejected by the exact inventory guard before touching the card. A controlled
repair frees the unreferenced cluster without adding a file, clears the dirty
flag and corrects the free-space count. Its independent filesystem check is
clean; all 23 file contents and 40 directory paths match the original. The plan
changes 10 bytes across four existing bookkeeping sectors. The complete
[physical FAT readback](original-card-repair-verified.json), using `O_DIRECT`,
matches that clean candidate. MBR/bootstrap bytes are unchanged and the data
volume is not written. The first buffered-view readback failure is retained;
later buffered and direct reads match, without repeated writes. A device-cache
alias is a plausible explanation, not an established cause.

The repaired original card then boots successfully. The
[restoration receipt](original-card-baseline-restored.json) authenticates its
original card/kernel, exact `6de` image and active/selected `a14a` bundle at
generation 38, in new boot `35d95481-a33d-4d4b-a042-28e9163b3cc4`. The original
host is paired and ready in that same epoch; its unit/configuration/catalog are
unchanged. The board is released ready, idle and lease-free with no maintenance.
The private host and passive UART observer are stopped. The working original
card remains installed and the tested new card stays aside.

This ordinary native-image/core result does not admit a static PR package or
exercise physical partial reconfiguration. Production enablement, owned backend construction, raw PR Design Closure, physical expansion testing
and the full normal-kit goal remain open. No new physical USB, Bluetooth, audio,
whole-board or complete simulation-matrix acceptance is claimed.


## Native managed software and normal library launch

Source `b37a428f3c6f5f2df5b8722b78157cd6f3a97660` connects static packages to
normal library launch preparation, readiness, saved next-start selections and
exact reply validation. The programming profile stays unavailable; none of
these changes enables physical partial reconfiguration. The complete affected
parent/Go/UI selection passes, as do the focused full host service/API suites
and the normal-library AArch64 QEMU tests.

The [native software trial](native-b37-software-trial.json) runs nine actual
AArch64 binary suites on the bottom kit's two CPUs. C++ package/composition/
protocol and earlier Go implementation inputs remain bound to `ddf2ca7a4`;
the normal-library service/catalog tests are built from `b37a428f3`. Synthetic
opaque packages and target peers exercise composition and staging without
opening hardware. The unchanged supervisor bounds every unit and the whole run.

A `b37a428f3` immutable userspace bundle then installs through the managed API
on the existing `6de6a392` Linux image. Held installation, competing client
refusal, queue/cancel/idempotence/stale-generation checks, requester exit and
activation pass at generation 39. An independent authenticated observation
confirms all four executable hashes and the unchanged card/kernel/boot identity.
A collector filename mistake after successful activation remains recorded; the
same successful transaction is inspected without reinstalling it.

The ordinary qualified standard ZX81 package launches through the private
host's real catalog and tape-media path. Real host HID input loads and runs the
BASIC tape, edits its output and reloads it. All three HDMI captures are
visually inspected: [7](native-b37-02-load-run7.png),
[9](native-b37-03-edit-run9.png), then [7](native-b37-04-reload-run7.png).
Ordinary Stop returns to healthy MENU. This verifies that capture is updating;
it does not qualify physical USB input, audio or any PR persona.

The named `a14a53cd` baseline is explicitly reactivated at generation 40 in
the same boot. Its original dedicated host, configuration and catalog are
restored, the private host is stopped, and the kit is released ready, idle and
lease-free. Image, kernel, SD, boot and flash remain unchanged; no reboot or
power cycle occurs. Later launcher receipt-projection changes are covered by
separate software tests, without attributing this native trial to their head.


The launcher also preserves `static_composition` when decoding session status.
It validates the receipt identity and package binding before showing the running
empty socket or RAM pack, and refuses mixed legacy/parts/ROM/empty receipts.
The saved next-start selection never substitutes for that active receipt.
Full host-client and room suites pass against both shared conformance vectors,
including changed drafts, wrong identities/packages and each mixed receipt.
The first development test mistakenly waits for an unfitted-card caption after
fitting a card; its failed log is retained. The corrected test waits for the
actual session projection and keeps all receipt/status/Stop assertions.
These launcher checks are software coverage, separate from the b37 deployment.

## Correction: native ZX81 picture polarity

The later report that native ZX81 displayed white text on black paper was a
misreading of image previews. The [pixel audit](native-picture-polarity-correction.json)
checks nine original, hash-matched captures from three native 7 → 9 → 7 sequences:
the original image, the new managed image, and that new image after software
reboot. Every capture has white paper and black ink. An independent PNG
CRC/unfilter decoder and FFmpeg produce byte-identical RGB. Each first and
reloaded 7 is identical after decoding; 9 differs. The original captures are
retained in [polarity-audit](polarity-audit/).

The earlier scaler repair, commit `a963620af41a347d2ba058988d9519509d4c49b7`
(PR #1016), remains in the shared source and both actual generated DE25 fits.
ULA 1 represents black ink; ULA 0 represents white paper. The compiled ordinary
RBF still hashes to `4750b93da22ef9ac3ae5623a42193dee5a7a7d6cdcc06286eca815047e99c4b1`.
The polarity-failure classification is withdrawn; its original logs are
preserved. Two subsequent observer runs stopped before collecting ZX81 pixels
or BASIC input and remain diagnostic failures. They establish no display fault.
No new board run, scaler change or SD swap was needed for this correction.

## Live PCM loss and relock simulation

The [receiver test](../../../sources/misteross/cores/fes-coleco/sim/audio_output_tb.cpp)
now decodes the full shared PCM/I2S implementation through three clock ratios
and four phase offsets. It covers 36 lock losses/relocks, stopping the audio
clock across one relock and the sample-source clock across another, plus mute
and relock while muted. Completed stereo samples remain coherent, delay/padding
bits remain zero, mute produces silence, and changing samples resume after
each interruption. This checks the real serializer rather than the serializer-free
projection in the existing held-data formal proof.

The [fresh-model receipt](audio-serializer-relock.json) records all 12 passing
cases. A private mutation bypassing serializer mute fails the receiver's silence
check. Production RTL, fitted inputs, constraints and admission are unchanged.
This is digital simulation; it establishes no PLL electrical behavior, analog
metastability, PCB timing, physical audio or partial-reconfiguration acceptance.
Static package admission and physical empty/16K tests remain required.

## Static board reference checks

Both independent fits' base, empty and 16K revisions pass the existing
[stock-reference checkers](static-stock-reference-review.json): actual fitted
pin locations/voltages, pixel/audio PLL settings, forwarded pixel clock,
transmitter register sequence and derived HDMI controller bytes match the
locked Terasic resource package. Read-only file views adapt only revision
filenames for the ordinary checkers; fitted QSF/pin bytes are unchanged.

The existing asynchronous-input checker also passes all six revisions'
[physical topology and timing reports](static-hdmi-input-review.json).
Each HDMI input has exactly its two-stage synchronizer in one tile, with
only the expected consumers. Worst interstage setup/hold slacks are
19.507/0.133 ns. The tool models MTBF above one billion years at its stated
50 MHz receiving clock and 6.25 million transitions/second default input
activity. These are fitted observations, not new policy eligibility or an
independent PCB timing model. The [review proposal](static-board-reference-proposal.md)
records the scope approved by the user on 2026-11-06. The canonical static
producer and persona exporter now require that separate, source-bound review
on both fresh fits. The earlier compile-only candidates remain unqualified;
no new package or hardware acceptance is implied by the implementation.
