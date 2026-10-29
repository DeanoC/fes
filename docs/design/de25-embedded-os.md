# DE25-Nano without Linux: feasibility study

Exploratory study, 29 October 2026. FES source baseline:
`eea51e94e014e9603055e8a0309d7fefb7d5009a`. This is not an accepted platform
change. Linux-free means no Linux kernel on the target; development tools and
an optional remote FogCast host can still run on an ordinary OS.

## Assessment

Both a bare-metal executive and an RTOS can plausibly control a DE25-Nano
running a suitably ported FES core. Neither can run today's complete FES
software unchanged. The CPU has ample capability; board drivers, FPGA
reconfiguration, memory ownership and the Go application stack are the main
work. A smaller kernel alone does not solve those problems.

Investigate **single-core bare metal for the first board diagnostic** and
**Zephyr for a target with networking, storage and concurrent services**.
Use the same small protocol layer in both. Decide between them after measuring
real board boot and completing one core's lifecycle. Do not start a custom
general-purpose OS or a Go port as a prerequisite to that experiment.

Keep the two possible products explicit:

| Scope | Feasibility | Consequence |
| --- | --- | --- |
| Remote FogCast host, small DE25 target | Plausible, bounded initial study | Host keeps library, UI and composition; target agent functionality and physical runtime still need a new platform implementation. |
| Standalone DE25 FES appliance | Substantial application port/rewrite | Library, storage, native Go UI, input, network, update and recovery services must all be addressed. The remote-host experiment does not prove this product. |

The first row is an experimental deployment, not a reversal of FES's current
kit-as-host architecture. The native tenfoot application is Go with platform
rendering/input adapters; it is not a Chromium wrapper. The web UI can remain
on another machine. Stock Go does not have a Zephyr or bare-metal target in its
[supported platform list](https://go.dev/doc/install/source).

## Hardware and firmware boundary

The [DE25-Nano resources](https://www.terasic.com.tw/cgi-bin/page/archive.pl?Language=English&No=1384&PartNo=4)
describe the Agilex 5 board and separate revision-specific resources. It has
two Cortex-A55 and two Cortex-A76 cores, 1 GiB shared LPDDR4, FPGA SDRAM,
Ethernet, SD, USB and HDMI. Its LPDDR4 handoff, pins and peripherals differ
from Intel's Agilex development kit and from the DE25-Standard.

For the proposed DDR-backed target, removing Linux does **not** remove the
SDM, DDR initialization or firmware handoff. Altera documents bare-metal software and an
[ATF/Zephyr boot path](https://docs.altera.com/r/docs/813762/25.3/hard-processor-system-booting-user-guide-agilextm-3-and-agilextm-5-socs/agilextm-3-and-agilextm-5-soc-boot-overview).
The Zephyr board documentation describes `BL2 -> BL31 -> Zephyr at EL1`.
Retaining a bootloader during experiments is compatible with having no Linux
on the target. A direct production boot path is a later decision.

Altera also documents an OCRAM-only bare-metal mode embedded in a matching
FPGA configuration that does not require TF-A or an SD card. That is useful
for a small diagnostic, but does not establish the DDR and firmware services
needed by the proposed FES target. The supplied QEMU image tests neither Nano
boot mode.

Start with one CPU and explicitly park the others. Record entry exception
level, counter frequency, memory map, cache/MMU state and firmware services.
A QEMU `virt` address or a Cyclone V register address must never become a
DE25 address by assumption. Physical DMA addresses, IOVAs and CPU pointers are
also not interchangeable.

## Candidate comparison

| Choice | Useful existing support | Missing work for this task | Role in this study |
| --- | --- | --- | --- |
| Pure bare metal | AArch64 startup plus Altera's bare-metal peripheral/firmware drivers; UART, counter and a polling loop suffice for an initial probe | Nano adaptation and validation; bounded drivers, scheduling if needed, network/filesystem/USB integration, memory ownership | Strong small-diagnostic option; a complete appliance would accumulate OS-like services. |
| Zephyr | Upstream Agilex 5 ARM64 port, GICv3, timer, UART, SDHC, Ethernet/MDIO, I2C and firmware services | Nano board/PHY/LPDDR4 integration, full FPGA loading, DMA/reset ownership, local USB input, FES target implementation | Strongest RTOS candidate for a networked target. |
| FreeRTOS | Official AArch64 kernel ports | More board/driver/network/storage integration than the reviewed Zephyr port | Viable scheduler alternative if a concrete driver need favors it. |
| RTEMS / NuttX | ARM64 support | No DE25/Agilex 5 BSP found in the reviewed board lists | No demonstrated advantage for the first experiment. |

Sources: [Zephyr Agilex 5 board](https://docs.zephyrproject.org/latest/boards/intel/socfpga/agilex5_socdk/doc/index.html),
[FreeRTOS AArch64](https://github.com/FreeRTOS/FreeRTOS-Kernel/tree/main/portable/GCC/ARM_AARCH64_SRE),
[RTEMS BSPs](https://docs.rtems.org/docs/main/user/bsps/index.html),
[NuttX ARM64](https://nuttx.apache.org/docs/latest/platforms/arm64/index.html).
These describe available upstream building blocks, not qualified Nano support.

Pure bare metal does not require writing every driver from scratch. Altera's
[baremetal-drivers](https://github.com/altera-fpga/baremetal-drivers/tree/66bdcc347564c5698a8997169bce83a31647d618)
has Agilex 5 startup, UART, GIC, DMA, MMU/SMMU, SDMMC, Ethernet and SDM mailbox
building blocks. The documented board is Intel's development kit, not the
Nano. Its [driver overview](https://altera-fpga.github.io/rel-25.1/driver-list_baremetal/)
describes pre-release validation. Audit these drivers before reuse: the reviewed
mailbox uses TF-A SiP calls and synchronous dispatch without its own timeout;
the DMA documentation says a wait timeout does not stop the transfer. A timed
out buffer cannot be released on that basis. This is a better starting point
than the archived Cyclone V-era hwlib, but is not a ready FES BSP.

Two gaps deserve early attention. The reviewed Zephyr
[Agilex FPGA driver](https://github.com/zephyrproject-rtos/zephyr/blob/main/drivers/fpga/fpga_altera_agilex_bridge.c)
provides bridge on/off operations, not a bitstream-loading implementation.
Also, a generic USB API does not establish an Agilex DWC3 host driver; the
reviewed [host-controller list](https://github.com/zephyrproject-rtos/zephyr/blob/main/drivers/usb/uhc/CMakeLists.txt)
does not supply that path. Network input is a useful initial limit, but local
USB remains necessary for the usual appliance experience.

Pure bare metal reduces the initial code and makes hardware sequencing easy
to inspect. It also makes every blocking SD read, network packet and FPGA
handshake a scheduling decision. Use bounded state machines and one physical
owner; add interrupts or DMA only after the polling baseline is measured.
Neither design may reset or reconfigure a fabric master while it still owns
an HPS memory transaction. A preloaded FPGA is sufficient for the first probe,
but not for FES Play/Stop and core replacement.

## Current FES dependencies

| Existing implementation | What must change on a Linux-free target |
| --- | --- |
| Runtime `src/linux/production_hardware.cpp` | New board construction; preserve physical lifecycle ownership in libmister-runtime. |
| `native/linux/fpga_manager.cpp`, `native/fes_gp.cpp` | Agilex firmware/configuration/reset path; board-specific transport; replace POSIX timing and synchronization. |
| `native/artifacts.cpp`, `native/core_package.cpp` | Retain verified, immutable package bytes before mutation without relying on POSIX directory/file descriptors. |
| `native/core_data.cpp`, `native/media_data.cpp` | Explicit durable publication semantics; FAT writes alone are not atomic rename/fsync equivalents. |
| Runtime daemon and FogCast `internal/misterruntime` | Replace local Unix socket transport while preserving session generations and authority. |
| `native/linux/input.cpp` | Replace evdev/poll/eventfd with proven USB or network input delivery. |
| `native/linux/menu_window.cpp`, FogCast `ui/menudisplay/client_linux.go` | Replace FD grants/mmap/kernel driver with owned buffers, cache maintenance, fences and revocation. |
| FogCast target agent, library and tenfoot | Stock Go OS dependency; move services off-board initially or port/reimplement them deliberately. |

Paths in the first column are under `sources/libmister-runtime/src/` unless a
FogCast path is named. Existing `Clock`, `FpgaManager`, `Mmio`, `ArtifactOpener`
and `InputSession` seams help, but the current runtime is not a freestanding
library. Network coordination remains FogCast-owned; physical transitions
remain runtime-owned; board definitions remain mister-packages-owned.

There is an independent FPGA port. Current Cyclone V bitstreams, HPS DDR
contracts and reset registers cannot be reused as Agilex artifacts. The
current Mistral production path is not an Agilex backend. The
[Quartus policy](quartus-cores.md) discusses a separate Agilex board shell and
build route. Nothing in this experiment changes toolchain pins or relaxes
timing/seal checks.

## Prototype and evidence boundary

[`examples/de25-embedded-os`](../../examples/de25-embedded-os/README.md)
contains a shared allocation-free C++14 protocol probe, host fault tests,
a Zephyr application and a pure bare-metal QEMU adapter. It consumes the
existing generated `fes.simple-game` constants without changing them.

The simulated endpoint allows testing identity checks before release,
ACK toggles, bounded sampling, stable-response timing, input/hold requests,
rejections and no replay after an ambiguous exchange. The host has a virtual
clock; the two AArch64 adapters use their platform timers.

The endpoint is an in-memory responder, not FPGA RTL or DE25 MMIO. This is a
new, narrow implementation of protocol logic, not a port or execution of the
production runtime. It has no package admission, FPGA programming, network,
storage, display, DMA or USB implementation. Passing it shows that this small
protocol boundary can execute without Linux; it does not show that FES or a
DE25 board works. See the retained [validation record](../validation/2026-10-29-de25-embedded-os/README.md)
for the exact tested inputs and results.

## Relationship to other DE25 work

Read-only inspection of sibling branch `codex/de25-hps-interface` at
`703f5215c23155418b125af87e310e50a30b083f` and
`codex/de25-hps-display` at `d9bf4d1434e76590791407289af4d601f5af7636`
found Linux userspace/DDR/display diagnostics. They are not dependencies of
this branch, and their evidence is not inherited.

The former branch's `fes.fabric.de25-hps-mailbox` is a diagnostic register
endpoint with ID/version/scratch/counter/status. It is **not** the FES GP
protocol used by this prototype. Its subsystem offset `0x100a0` does not
establish an HPS physical address. A future board probe should use that
endpoint only after the firmware/handoff establishes its mapping.

## Next experiments and stop conditions

1. **Firmware to RAM payload:** identify Nano revision and matching firmware/
   handoff; print CPU/EL/counter information and test a reserved RAM range.
   Require repeatable cold boots. Do not modify boot flash as part of this study.
2. **Read-only fabric access:** prove bridge/firewall mappings against the
   identified diagnostic design. Read identity/counter, then do a bounded
   scratch round trip with restoration. Wrong identity means stop before writes.
3. **Memory ownership:** demonstrate a reserved DMA buffer, correct cache
   publication/invalidation and bounded completion. Prove drain and reset
   behavior before buffer reuse; a local IDLE bit is insufficient.
4. **One core's full lifecycle:** port a small core/shell; configure through
   supported SDM services, verify identity, deliver input, Stop and restore an
   identified idle design. Include failure/timeout cases and preserve HPS DDR.
5. **Target services:** add Ethernet and SD with immutable package handling,
   then a bounded remote-host session. Decide on bare metal versus Zephyr using
   measured complexity, latency and recovery behavior at this point.
6. **Standalone decision:** assess native UI, local USB, writable media,
   durable saves and updates separately. They are outside the current prototype.

Measure boot-to-ready time, static/RAM/stack footprint, worst-case input and
media service latency, CPU use, and recoverability on the actual board. The
study makes no speed, memory-saving or reliability claim from kernel choice
or QEMU timing. Stop a branch of the experiment if it requires guessing
hardware addresses, weakening package identity, unsafe live reconfiguration
or removing required behavior to call the complete appliance supported.
