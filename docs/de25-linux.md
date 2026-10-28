# DE25-Nano headless Linux

The DE25-Nano Linux lane builds a standalone AArch64 diagnostic image with a
serial console, Ethernet DHCP and Dropbear SSH. FES owns the root filesystem;
the boot files and kernel/device tree come from the pinned Terasic console BSP.
The lane lives in [`image/de25-nano/`](../image/de25-nano/) and has its own inputs,
cache, outputs and verification. It does not use `native-integration-dev` or
change the DE10-Nano boot-media and toolchain locks.

This is the first Linux bring-up milestone. It contains no FPGA runtime,
FogCast target agent, MENU display, playable core or managed appliance update
service. A successful image build does not qualify those interfaces.

## Build and inspect

Run these commands from the FES repository root on a Linux build host with
Python 3.11+, Git and Docker available:

```sh
make fetch-de25-linux
make de25-linux
make verify-de25-linux
make smoke-de25-linux
```

`fetch-de25-linux` downloads the locked external inputs into
`out/de25-linux-cache/`. Fetching dependencies can run before source edits are
committed. `de25-linux` requires a clean, committed source tree, selects that
commit in a separate snapshot, builds the root filesystem and assembles the
diagnostic card under `out/de25-nano-linux/`.

The lane reuses the existing pinned native-image builder container and selects
Buildroot 2021.02.4 through its own
[`buildroot.lock.toml`](../image/de25-nano/buildroot.lock.toml). Its AArch64
configuration produces a separate glibc C/C++ toolchain and SDK. Compiler outputs live
in a dedicated Docker volume with a cache key for the DE25 rootfs inputs;
downloads stay in the explicit cache directory. This does not select the
DE10-Nano SDK or rebuild its runtime. No Go host or target services are included:
the initial userspace contains BusyBox and Dropbear.

The runner accepts explicit output and cache directories, parallel job count
and container runtime through `DE25_LINUX_ARGS`:

```sh
make de25-linux DE25_LINUX_ARGS='--output /absolute/de25-linux --cache /absolute/de25-cache --jobs 8'
make verify-de25-linux DE25_LINUX_ARGS='--output /absolute/de25-linux --cache /absolute/de25-cache'
make de25-linux DE25_LINUX_ARGS='--help'
```

The default container runtime is `docker`. These commands prepare and inspect
regular files. They do not write a card, program the FPGA or change boot flash.

## AArch64 runtime and agent checks without a board

After building the C/C++ SDK, run:

```sh
make smoke-de25-runtime
```

`DE25_RUNTIME_ARGS` accepts `--image-output`, `--output`, `--jobs` and
`--container-runtime`. For example, a retained image in a separate directory
can be tested without replacing it:

```sh
make smoke-de25-runtime DE25_RUNTIME_ARGS='--image-output out/de25-nano-linux-cxx --output out/de25-runtime --jobs 2'
```

The diagnostic requires clean committed sources, verifies the selected image
receipt and SDK hash, relocates that SDK and uses the runtime's existing
Makefile to build its archive, daemon and six unit-test binaries. It checks the
canonical archive and runs the runtime/protocol, fake-MMIO, fake FPGA-manager,
file-offset and production-board guard tests under QEMU user emulation. It also
cross-builds the FogCast agent and exercises its early-exit command plus five
existing client/readiness/rejection tests. The production daemon is compiled;
it is not started by this diagnostic.

`out/de25-runtime/runtime.json` binds the runtime source revision, image/SDK
producer, compiler identities and output hashes. Test logs and the resulting
runtime archive, daemon and agent remain in that output. Its classification is
`userspace-only`: it establishes AArch64 build/software behavior, without
claiming a DE25 FPGA programming backend, hardware MMIO map or device access.
The current production runtime accepts only its identified ARMv7 DE10 target
and refuses other boards before loading drivers or constructing hardware.

The [2026-10-28 AArch64 software validation record](validation/2026-10-28-de25-aarch64-runtime/README.md)
retains the selected C++ SDK/image and runtime receipts, QEMU test logs, native
retry and CI evidence. Physical DE25 boot remains pending.

## Image artifacts

The image output contains:

| File | Meaning |
| --- | --- |
| `rootfs.ext4` | Writable AArch64 FES diagnostic root filesystem |
| `rootfs.tar` | Root filesystem contents for inspection |
| `fes-de25-nano.img` | Complete diagnostic SD-card image |
| `aarch64-sdk.tar.gz` | Buildroot SDK for this AArch64 userspace |
| `buildroot.config` | Resolved Buildroot configuration |
| `vendor-boot/` | Locked vendor boot prefix, extracted boot files and factory firmware dependencies |
| `image.json` | Source revision, recipe/builder identities and artifact hashes |
| `verify.json` | Root filesystem checks, written by verification |
| `smoke.json` | QEMU userspace probe results, written by smoke |

## Boot contract

The card keeps the pinned vendor MBR and FAT boot prefix and replaces the
vendor Linux partition with the FES root filesystem. Kernel, device tree and
boot configuration remain the selected vendor artifacts, including Linux
`6.12.11-gd7d192a9ddd9` and `socfpga_agilex5_de25_nano.dtb`. The vendor boot
arguments select `/dev/mmcblk0p2`; they do not depend on a rootfs UUID or label.
The vendor inputs and reviewed partition layout are declared in
[`vendor.lock.toml`](../image/de25-nano/vendor.lock.toml), independently of
`boot-media.lock.toml`.

Terasic's BSP instructions require a microSD card of at least 8 GB, the matching
HPS QSPI boot image and MSEL in AS mode. The build retains that BSP's exact
`golden_top_hps.jic` and instructions under `vendor-boot/factory-boot/` for
inspection. It does not program or qualify the board's QSPI contents. The
separately retained resource-package SOF is a source reference; it is not
identified as the build source of the BSP's JIC.

Inspect an existing image and, optionally, a separately built HPS diagnostic:

```sh
python3 scripts/de25_boot_contract.py --image-output out/de25-nano-linux-cxx \
  --hps-output /absolute/private-hps-build --output out/de25-boot-contract.json
```

This read-only inspection verifies the image receipt and locked boot files,
decodes the selected U-Boot FIT configuration and checks each embedded/external
payload hash. It resolves the script, console/Ethernet aliases, raw memory
declarations and firmware interfaces from the actual DTB bytes. The optional
HPS input checks the SOF, handoff and matching resource archive against its
committed-source build receipt. An existing report path is refused.

The pinned script enables FPGA bridges and replaces the DTB's stale chosen
boot arguments before loading the named kernel/device tree. U-Boot's embedded
DT declares 1 GiB, while the Linux DT declares a 2 GiB default. The pinned
[vendor U-Boot source](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/lib/bootm-fdt.c#L34)
implements memory-bank fixups before entering Linux. Actual booted memory and
the effective saved U-Boot environment remain board observations.

The Linux DT contains no lightweight-bridge window or mailbox endpoint. Neither
its generic SoC ranges nor the bootloader's firewall-register settings establish
a CPU mailbox address. The inspector leaves that address unestablished and
keeps custom SOF/factory-firmware pairing unqualified. Preserved source
parameters and matching archive identity do not replace that compatibility
check. Inspection never executes the boot script or performs bridge/MMIO access.

The [2026-10-28 boot-contract inspection](validation/2026-10-28-de25-boot-contract/README.md)
retains the exact report, firmware-source review and remaining pairing gates.

The serial console is `ttyS0` at 115200 baud. Ethernet requests an address by
DHCP on `eth0`. The development login is `root` with password `1`, matching the
existing FES development convention. Dropbear creates the board's SSH host keys
on the writable root filesystem at first boot; no shared host key is shipped.
`/etc/fes-board` identifies this root filesystem as `de25-nano`.

The selected vendor boot path may generate a random Ethernet MAC when no
valid address is available from firmware or hardware. Stable MAC identity
across reboots is not established by this milestone. Record the observed MAC
and DHCP address during board qualification, including distinct addresses
when testing both boards.

The normal FES appliance bootstrap is not used. Its current kernel contract,
trial watchdog reset preparation and software bundle identities target
DE10-Nano. The DE25 image does not accept DE10 bundles or claim managed update,
rollback, FPGA programming, HPS DDR or display compatibility.

## Validation and the board gate

The [2026-10-28 host validation record](validation/2026-10-28-de25-headless-linux/README.md)
retains the exact image, verification and QEMU receipts plus the exported SDK
probe. Physical board boot remains pending.

`verify-de25-linux` checks the retained artifact hashes, AArch64 executable
identity, card layout and required rootfs services. `smoke-de25-linux` probes
AArch64 userspace through QEMU user emulation: BusyBox reports its architecture
and Dropbear serves an SSH banner using a temporary host key. This is not an SSH
login or DE25 board emulation, kernel boot, Ethernet or FPGA check.

The remaining hardware check uses an identified DE25-Nano and the exact
`fes-de25-nano.img` digest. After the applicable board claim and authorized
media delivery, retain:

1. The serial boot log, kernel release, CPU architecture and device-tree model.
2. The mounted root partition and filesystem identity.
3. DHCP and an SSH login to the diagnostic rootfs.
4. A clean reboot and successful second serial/SSH startup.

Record those observations against the source revision, vendor input identity,
image digest and board revision. Until that check completes, evidence remains
host-only and physical boot is pending.

The HIL planner reports `FULL_IMAGE` with component `de25-nano-linux` for
`image/de25-nano/` changes. That identifies this DE25 image's boot check; it does
not select a DE10 kit software bundle or establish acceptance of the default
FES image.
