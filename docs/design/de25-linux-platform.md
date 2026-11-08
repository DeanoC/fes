# DE25 normal Linux development kit

The goal is the same managed development workflow as DE10: boot FES, discover
the kit, show MENU, install and launch a core, deliver input/media, stop back to
MENU, and recover/update through supported interfaces. Native CPU maintenance
and JTAG diagnostics are intermediate evidence only.

Expansion selection is static for the next launch, matching the DE10 workflow.
The DE25 target's AArch64 CPU composes and verifies the full base plus selected
persona before the runtime starts the core or allocates its display. Changing
selection requires ordinary Stop and a new full base load. Live expansion
replacement is outside this milestone; using a partial image does not change
that lifecycle. The [startup preparation record](../validation/2026-11-05-de25-static-expansion-offboard/README.md)
separates tested CPU/model behavior from normal package admission and physical
configuration, which remain required.

On 2026-11-08 Deano explicitly deferred physical partial-persona acceptance
from the ordinary development-kit milestone. The hang remains tracked in
[#1568](https://github.com/DeanoC/fes/issues/1568); normal development software
keeps expansions disabled and its catalog offers ordinary ZX81 only. The
[scoped waiver](../validation/2026-11-08-de25-ordinary-dev-kit/README.md)
does not waive other qualification or transfer-safety requirements.

The [3 November Terasic-based factory proof](../validation/2026-11-03-de25-terasic-managed-factory/README.md)
now completes ordinary source compilation, sealed MENU admission, actual
kernel/rootfs virtual boot, release, bootstrap and two-pass card construction.
The managed bottom-board workflow now has native evidence for its named artifacts;
full development-kit acceptance remains open as described below.

| Step | Owner | Acceptance on DE25 |
| --- | --- | --- |
| FES Linux baseline | FES image | Bottom-board FES SD and HPS-first QSPI pass cold startup and three software reboots; original flash readback retained |
| Linux FPGA programming | runtime physical lifecycle; FES kernel packaging | Matching kernel modules, boot configuration and core RBF; repeat load while Linux/SSH and boot UUID survive; failed loads recover within a bounded lifecycle |
| Board/package identity | mister-packages; misteross producers | Sealed DE25 artifact, exact handoff and CPU resource map; reject mismatches before device access |
| Production runtime | libmister-runtime | AArch64 board identification, serialized load/reset/stop, register backend and safe epoch handling; preserve DE10 construction |
| Managed software | FES image; FogCast agent | AArch64 bundle, supervisor, target discovery, install/activate, baseline restore and compatible updates |
| MENU display | misteross scanout; kernel/runtime | Real device DMA/translation, coherent frame ownership, bounded stop and reset fencing, captured MENU |
| ZX81 normal launch | FogCast orchestration; runtime | Catalog install, MENU launch, keyboard and BASIC LOAD/RUN/reload, stop/return-to-MENU, relaunch and persistence |
| Shared kit operations | FES | Claim, idle/lease boundary, artifact identity and recovery logs usable without physical intervention for ordinary operations |

## Current native evidence

The retained development setup now uses kernel `9cec7e77`, managed root
`0858eef0`, diagnostic FIT `e649e2c6` and default-off bundle `61a4b0aa`,
generation 1. Normal software install/activation and native executable
authentication pass. Standard ZX81 `3da0f1df` passes Play, RAMTOP128, BASIC
7/9/7, fresh HDMI, Stop/MENU and relaunch. One idle and one active-session OS
reboot pass without physical intervention, preserving pairing, software,
catalog and a saved-data probe. The current boot is
`bd1544c0-9143-48a3-b43d-fc47860824a6`.

Physical Kinesis USB input produces PRINT; physical F12/Escape opens and closes
the allocated Change media chooser in the same session and package generation,
restoring the PRINT line. This is one observed open/close cycle rather than
display stress or arbitrary-failure qualification. The
[current dated record](../validation/2026-11-08-de25-ordinary-dev-kit/README.md#software-reboot-physical-usb-and-in-play-menu)
retains exact identities, captures and the remaining boundaries. The board is
released at healthy MENU with this user-requested development setup retained;
no main HIL baseline is promoted. Diagnostic firmware admission, broader
failure recovery and untested peripheral capabilities remain open.

## Earlier native evidence

The bottom board TRWU8J8G runs managed FES Linux image `6de6a392` with the matched
ABI3 kernel. Its recorded shared-kit software baseline is `a14a`. Normal software
activation and restoration, sealed standard ZX81 package `138ab5cb`, the actual
FES picker, BASIC SAVE/load/run/edit/reload, Stop/Home and runtime-exit recovery
have source-bound native records. One ordinary active-core reboot at source
`a35f3207a` passed, followed by a failed first-session display reopen;
[that failure remains retained](../validation/2026-11-04-de25-standard-zx81/README.md#reset-export-package-and-native-trial).

Runtime source `510933314` adds first-failure reporting without changing display
ownership, timing or recovery gates. Its immutable AArch64 bundle `4ecf08d8`
passes a first-session SAVE/load/run/edit/reload proof and three genuine F12/Esc
allocation close/reopen cycles. The close-cycle fixture's BASIC images are
program listings, so that case supplies partial evidence rather than a run proof.
One ordinary active-core reboot and the subsequent first-generation SAVE/picker
proof also pass. The earlier display fault does not recur; the reporting change
does not establish its root cause or repair. Exact evidence is retained in the
[standard-core validation record](../validation/2026-11-04-de25-standard-zx81/README.md).

The subsequent [ten-cycle MENU trial](../validation/2026-11-04-de25-standard-zx81/README.md#bounded-native-display-retirement-follow-up)
passes with fresh display allocations and no display errors or underflows.
Captured images show the media chooser and return to the ZX81 K cursor. This
uses virtual keyboard input; the intermittent fault is neither reproduced nor
fixed, and this case adds no BASIC, physical USB, audio or PR acceptance.

An additional [allocated-chooser crash trial](../validation/2026-11-04-de25-standard-zx81/README.md#native-crash-with-the-allocated-chooser-visible)
records one runtime crash with an exposed display allocation, followed by
automatic MENU recovery and a fresh ZX81 session. It uses the retained
source-510 software; the later inherited-drain repair still needs its own native
acceptance.

The latest [new-image trial and restoration](../validation/2026-11-05-de25-static-expansion-offboard/README.md#new-linux-image-native-ordinary-core-trial)
boots enabled kernel `9ac` and image `d11`, verifies durable data, Ethernet/SSH,
normal standard ZX81 BASIC 7/9/7 and an ordinary software reboot followed by a
fresh LOAD/RUN7. Its original-card restoration repairs verified filesystem
bookkeeping while preserving all files, then authenticates baseline `a14a` at
generation 38 in boot `35d95481-a33d-4d4b-a042-28e9163b3cc4`, with the original
`6de` image, kernel and card. The
original host, catalog and settings are restored, owned fixtures are removed,
and the board is independently verified ready, idle and lease-free before
release. These trials leave the top board untouched and promote no main baseline.

The unresolved display failure, bounded whole-load/secure-call recovery, physical
USB and audio acceptance, native ROM/persona composition and physical standard
partial reconfiguration remain open. QEMU, a diagnostic selection model and
internal-memory hardware success do not complete target-CPU or persona gates.
Current-head CI and independent review remain required.

## Evidence requirements and earlier stages

Each row requires its changed artifact and its actual hardware path. Passing a
simulation or a maintenance client does not complete the corresponding managed
flow. The separate headless Linux profile ships no runtime daemon, target agent,
managed bundle or MENU. The [HPS-first hardware proof](../validation/2026-10-31-de25-hps-first-hardware/README.md)
records volatile startup and native BASIC loading. The later
[persistent bootstrap trial](../validation/2026-10-31-de25-reboot-bootstrap/README.md#physical-trial)
loads ZX81 through Linux and runs BASIC in two epochs separated by ordinary
reboot. Managed loading, MENU and failed-load recovery remain separate gates.
The [earlier bring-up record](../validation/2026-10-30-de25-linux-fpga/README.md)
retains the failed FPGA-first/factory-start attempts.

Shared release storage and the update agent/API have explicit DE25 board
selection. The agent and storage gate bind the immutable image's board file to
its CPU architecture, while the upload parser and client refuse cross-board
payloads. The base storage helper builds for AArch64. The FES PID1 source also
supports an explicitly selected DE25 bootstrap and
reproducible AArch64 ext4 construction. Its CPU/root/volume checks and the
refusal of unqualified trial boots are covered in the
[bootstrap record](../validation/2026-11-01-de25-appliance-bootstrap/README.md).
Explicit board/CPU artifact selection and the shared factory's AArch64,
network, persistent SSH and software-bundle wiring are covered by the
[factory-input record](../validation/2026-11-01-de25-factory-inputs/README.md).
Its rootfs fixture is not a real CPU, package seal or physical boot proof.
The subsequent [managed-base record](../validation/2026-11-02-de25-managed-base/README.md)
builds a real AArch64 rootfs and SDK with the shared policy, matching built-in
USB Bluetooth/HID kernel and actual FES helpers. Filesystem/ELF checks and an
isolated SDK/C++/QEMU/SSH-banner proof pass. This intermediate retains its
bootstrap guard, lacks runtime/factory/MENU and has no native boot acceptance.
The [normal SDK/runtime record](../validation/2026-11-02-de25-managed-sdk/README.md)
then corrects the bootstrap mount point to a directory and SDK export links,
adds producer-authenticated SDK reuse, and passes the existing AArch64 runtime
builder, canonical archive audit and five Cortex-A76 CPU model executables.
The [factory action record](../validation/2026-11-02-de25-managed-factory/README.md)
adds managed-factory/verify-factory actions to the existing DE25 builder, using
the shared package selector, runtime install recipe and native bundle assembler.
The real pinned Buildroot configuration and real AArch64 runtime installation
pass; shared factory fixtures also cover the absent-startup-script transition.
These are configuration and assembly-path checks. The current unsealed MENU
still prevents a complete admitted factory build.
The [disk-construction record](../validation/2026-11-02-de25-managed-media/README.md)
adds DE25 to the shared regular-file disk constructor/reconstructor with 2 GiB
FAT rollback capacity and a partition-2 bootstrap. Real vendor boot bytes,
matching kernel/DTB and a fresh AArch64 boot selector reproduce the same disk
in both construction paths, with an explicit factory/idle binding fixture.
This establishes disk construction and payload checks, not factory admission,
CPU execution or native boot. The high-level release path's image-level boot
smoke requirement is preserved.
The [image boot record](../validation/2026-11-02-de25-image-boot/README.md)
then adds a real full-system AArch64 packaging boot action and factory receipt
binding. The unchanged actual managed base/kernel pass the shared filesystem
smoke on QEMU `virt`; base/factory profiles and userspace-only probes remain
distinct. The subsequent
[D-Bus rootfs repair](../validation/2026-11-02-de25-dbus-rootfs/README.md)
fixes that observed machine-ID failure and passes a freshly rebuilt image's
stronger virtual boot check, including a live system-bus response.
This does not establish full factory services, physical boot or native recovery.
The [public appliance workflow](../validation/2026-11-02-de25-appliance-workflow/README.md)
adds explicit DE25 factory selection to the existing release/bootstrap/card
commands, binds structural verification into factory hashes and reuses the
shared AArch64 builder, media lease and card reconstructor. Public commands
refuse the real intermediate as an incomplete factory. Positive routing and
integrity fixtures do not qualify a package or produce an admitted factory.
The [ordinary image/verification/export entrypoints](../validation/2026-11-02-de25-image-entrypoints/README.md)
also select DE25 explicitly, reuse that same factory producer and export actual
rootfs binaries, including rebuilt base helpers, against their bound identities.
They preserve default DE10 behavior. Ext4 extraction fixtures do not qualify factory authority, CPU behavior
or an FPGA package.
The [SDK development workflow](../validation/2026-11-02-de25-sdk-workflow/README.md)
adds `make kit-sdk BOARD=de25-nano DE25_IMAGE=...` and the same shared selection
for `make kit-software`. It caches authenticated original SDK bytes and uses
the existing private runtime builder. Actual Make cache/import, C++ and
Cortex-A76 QEMU checks pass; complete bundle publication still requires the
unavailable admitted factory. Factory SDK receipt cases use unqualified unit
fixtures, not a produced factory.
The [factory base-cache repair](../validation/2026-11-02-de25-factory-incremental/README.md)
separates compiler/base policy from selected software generations and refreshes
the prebuilt runtime on every actual factory build. A real pinned Buildroot
control retains stale software without that refresh, then installs the newly
selected runtime while keeping 59 prerequisite stamps unchanged. All factory
package, ABI, image and boot gates remain required. This is the reuse foundation.
The [board-scoped registry](../validation/2026-11-02-de25-board-recipes/README.md)
now selects the existing DE25 Quartus Pro MENU producer through the shared
resolver API, preserves DE10 defaults and binds package inspection/publication
to the requested board. Actual source/tool authentication passes with no
eligible DE25 package. The [source-development workflow](../development.md#de25-source-development)
now connects the selected AArch64 Go/C++ consumers and existing managed-factory
cache to ordinary `build`/`rebuild`/`dev`. Explicit SDK/base, owner-kernel/compiler
and MENU admission are required. Complete immutable generations and verified
cache hits preserve old output on failure; no artifact-only alias replaces
source compilation. Host/doctor use the shared frontend. An actual admitted
MENU, complete source-produced factory and native acceptance remain open.
The ordinary [one-core preparation command](../core-development.md#prepare-one-package-without-an-image-build)
also accepts `prepare --board de25-nano` for MENU or BRAM ZX81. It selects that
same registered resolver before staging source, preserves sealed package and
source-selection provenance, and rejects unsupported board/core pairs or
failed producer admission. Its default remains DE10-Nano; board selection
adds no package, factory or native admission.
The [source-workflow record](../validation/2026-11-02-de25-source-workflow/README.md)
retains actual AArch64 software/CPU and host/cache checks, full parent validation
and the public Make/vendor-input refusal. Standard verification now accepts source
workspaces or explicit factories, and source publication supplies the shared closed
package-selection set. Its offline acceptance reader does not establish native
readiness.
The [source verification proof](../validation/2026-11-02-de25-source-verification/README.md)
records the current checks and external-cache/read-only verification boundary.
The later factory/card proof completes managed factory and card/media assembly.
Installation, watchdog/reboot recovery and the native workflow in the table
remain open.

The approved bottom-board baseline is the existing FES SD plus the exact
HPS-first QSPI in the [persistent bootstrap trial](../de25-reboot-bootstrap.md).
Preserve the verified pre-write factory readback, vendor rollback and original
factory Ubuntu card. The top board remains on factory startup. Future QSPI or
boot-configuration changes still need explicit authorization. This exact-board
trial does not qualify other rebuilt profiles or production DMA/reset fencing.
