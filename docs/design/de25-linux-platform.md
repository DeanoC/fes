# DE25 normal Linux development kit

The goal is the same managed development workflow as DE10: boot FES, discover
the kit, show MENU, install and launch a core, deliver input/media, stop back to
MENU, and recover/update through supported interfaces. Native CPU maintenance
and JTAG diagnostics are intermediate evidence only.

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

Each row requires its changed artifact and its actual hardware path. Passing a
simulation or a maintenance client does not complete the corresponding managed
flow. Headless Linux currently ships no runtime daemon, target agent, managed
bundle or MENU. The [HPS-first hardware proof](../validation/2026-10-31-de25-hps-first-hardware/README.md)
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
Complete DE25 card/media and managed factory assembly, installation,
watchdog/reboot recovery and the native workflow in the table remain open.

The approved bottom-board baseline is the existing FES SD plus the exact
HPS-first QSPI in the [persistent bootstrap trial](../de25-reboot-bootstrap.md).
Preserve the verified pre-write factory readback, vendor rollback and original
factory Ubuntu card. The top board remains on factory startup. Future QSPI or
boot-configuration changes still need explicit authorization. This exact-board
trial does not qualify other rebuilt profiles or production DMA/reset fencing.
