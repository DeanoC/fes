# DE25 normal Linux development kit

The goal is the same managed development workflow as DE10: boot FES, discover
the kit, show MENU, install and launch a core, deliver input/media, stop back to
MENU, and recover/update through supported interfaces. Native CPU maintenance
and JTAG diagnostics are intermediate evidence only.

| Step | Owner | Acceptance on DE25 |
| --- | --- | --- |
| FES Linux baseline | FES image | Cold boot, DHCP, SSH identity and ordinary reboot verified on the replacement SD; factory QSPI retained |
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
bundle or MENU. The first Linux-manager probe succeeds; actual core loading is
still under investigation in the [bring-up record](../validation/2026-10-30-de25-linux-fpga/README.md).

Keep the approved FES SD plus factory QSPI as the baseline while preparing the
new boot/programming artifacts. Preserve the original factory Ubuntu card and
known-good images. A persistent QSPI or boot-configuration change needs Deano's
explicit authorization after the artifacts and recovery procedure are concrete.
