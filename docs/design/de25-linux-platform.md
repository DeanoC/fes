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
Complete DE25 card/media and managed factory assembly, installation,
watchdog/reboot recovery and the native workflow in the table remain open.

The approved bottom-board baseline is the existing FES SD plus the exact
HPS-first QSPI in the [persistent bootstrap trial](../de25-reboot-bootstrap.md).
Preserve the verified pre-write factory readback, vendor rollback and original
factory Ubuntu card. The top board remains on factory startup. Future QSPI or
boot-configuration changes still need explicit authorization. This exact-board
trial does not qualify other rebuilt profiles or production DMA/reset fencing.
