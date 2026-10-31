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
retry and CI evidence. That record covers userspace software only; the later physical Linux/native-control
record below adds board boot and direct CPU control.

The [2026-10-30 physical CPU proof](validation/2026-10-30-de25-board-cpu-proofs/README.md)
runs retained AArch64 executables directly on both boards using their factory
Ubuntu images. It checks deterministic base/persona composition, runtime unit
tests and actual C++ controllers. The controller clients reach frozen RTL
simulations on the host through private SSH-forwarded Unix sockets. This adds
physical CPU execution evidence; FPGA programming, physical MMIO/DDR and the
FES image remain separate qualification steps.

The [physical memory diagnostic](validation/2026-10-30-de25-physical-memory/README.md)
records a volatile JTAG RAM-Tester load with the preserved Terasic SPL, local
LPDDR4 B/SDRAM checks and factory-SD Linux boot. The retained transcripts do
not bind this session to a USB serial or board UID. HPS DMA, production runtime
loading and the FES image remain outside that diagnostic.

The [cold-reset investigation](validation/2026-10-30-de25-reboot/README.md)
separates working factory-QSPI reboot from FPGA-first JTAG bootloader delivery.
After a diagnostic cold reset, reload the bootloader-complete SOF to recover
Linux. The retained record does not establish whether a physical power cycle
occurred, and preserves the closed warm-reset/DMA gates.

## Board-local ZX81 maintenance

The runtime's explicit `de25-computer` utility controls an already configured
ZX81-HPS diagnostic directly from the DE25 CPU. It is built separately from
the headless image with `make -C sources/libmister-runtime de25-computer`,
selecting the retained AArch64 SDK compiler through `CXX` for board execution.
The default host build supports help/early-refusal checks and does not open
hardware on other architectures. This utility installs no daemon or core package.

Before access, claim the board and bind the loaded full SOF, generated HPS
handoff and selected computer build ID to that board's boot epoch. The audited
ZX81-HPS design places the computer at CPU address `0x20010300`, span 32,
through the HPS bridge at base zero plus peripheral offset `0x10300`.
Its fabric JTAG address `0x00110300` belongs to a different master. The CPU
bridge window follows Altera's [Agilex 5 Linux example](https://altera-fpga.github.io/rel-26.1.1/embedded-designs/agilex-5/e-series/premium-065a/fabric-config-linux/ug-fabric-config-linux-agx5e-premium-065a/);
the actual generated connection/map and physical endpoint are checked separately.

The CLI requires an explicit boot UUID, full computer build ID, CPU resource
base and command. It checks the FES rootfs/model and boot epoch before opening
registers and holds an exclusive cooperating maintenance lock. `probe` reads
only endpoint ID/version, without identifying the computer build or writing
control. `hold`, `run`, `reset`, `keys` and `media` additionally verify the
selected computer build. Reset completion acknowledges execution control;
observe the ROM prompt before typing LOAD or program input. Hardware bus stalls
cannot be interrupted by these software deadlines. This is confined maintenance
of an already verified, exclusively owned image; production MMIO, programming,
reset fencing and package admission remain separate work.

The [physical Linux/native-control record](validation/2026-10-30-de25-linux-zx81/README.md)
retains the initial FES boot, DHCP failure, direct CPU control, observed BASIC
sequence, separate image/client identities and remaining image qualification.

The [normal Linux kit acceptance map](design/de25-linux-platform.md) tracks the
remaining managed workflow. The [Linux FPGA-manager bring-up](validation/2026-10-30-de25-linux-fpga/README.md)
records the initial transfer/cleanup failure. The later
[HPS-first hardware proof](validation/2026-10-31-de25-hps-first-hardware/README.md)
boots Linux with only the HPS bootstrap, then loads ZX81 and runs BASIC using
the board CPU. Factory-start loading and ordinary JTAG-epoch reboot still fail;
managed runtime, MENU and normal catalog launch remain separate gates.

The [persistent bootstrap procedure](de25-reboot-bootstrap.md) prepares a
closed candidate/recovery/vendor-rollback set for the next reboot milestone.
Preparation never programs the board. The explicitly approved bottom-board
[hardware trial](validation/2026-10-31-de25-reboot-bootstrap/README.md#physical-trial)
now passes three software reboots with this QSPI bootstrap, plus Linux ZX81
loading and native BASIC in two epochs. Its backup and working bootstrap are
retained independently of the task checkout.

## FPGA kernel modules

The image builder selects the four unchanged vendor FPGA modules for kernel
`6.12.11-gd7d192a9ddd9`. The [module lock](../image/de25-nano/kernel-modules.lock.json)
binds their byte hashes to the selected vendor kernel Image. Preparation
validates the BSP archive and decompressed image, reads the modules from a
private regular partition file with debugfs, then checks AArch64 ELF identity,
vermagic and the complete dependency closure. No card is mounted or written.

The rootfs contains that closed module tree, generated `modules.dep`,
`/etc/fes-kernel-release` and `/usr/share/fes/kernel-modules.json`.
`S15fpga` refuses a different running kernel before invoking modprobe, loads
the manager/region dependency graph and reports ready only when both kernel
interfaces exist. It does not configure an FPGA or unload drivers on shutdown;
physical configuration and lifecycle remain the runtime's responsibility.
Rootfs verification checks the module bytes, metadata, dependency file and
executable boot service in both tar and ext4 outputs. The native runtime's
AArch64 production-admission gate is unchanged.

The module packaging and boot service have off-board checks, including real
ELF files matching the factory board's hashes and isolated execution of the
actual service. A newly built image still needs its own boot/autoload check;
these checks do not promote the earlier manually loaded diagnostic image.

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
DHCP on `eth0`. If the first discovery window expires while the PHY or switch
is settling, the DHCP client backgrounds and continues retrying while boot
proceeds. The development login is `root` with password `1`, matching the
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

## First-boot evidence collection

[`scripts/de25_first_boot.py`](../scripts/de25_first_boot.py) prepares expected
identities offline, collects read-only SSH observations and imports an existing
serial capture. It never deploys an image, programs an FPGA, opens a serial
device, changes boot configuration, performs MMIO or issues a reboot. Use it
only against the explicitly identified DE25 after the applicable board claim
and separately authorized media delivery. It is not a FogCast/runtime client;
the headless image has BusyBox and Dropbear only.

Prepare expectations against the retained image before the board arrives:

```sh
python3 scripts/de25_first_boot.py prepare \
  --image-output /absolute/de25-linux-output --output /absolute/de25-expected.json
```

Preparation reuses the boot-contract inspector above to verify the image
receipt, pinned FIT/DT and boot files. It binds the selected kernel release and
seven diagnostic binary/configuration hashes from the verified rootfs tar.
Matching those observed files does **not** attest the whole SD-image digest:
this image installs no immutable image-identity marker, and its filesystem is
writable. The receipt preserves that limit rather than claiming an exact image
from a board name or kernel release.

During the eventual physical check, retain serial output from power-on through
the login prompt at 115200 baud. The tool imports a caller-supplied regular log
file; it does not manipulate termios/DTR or transmit serial commands. Obtain
the board's public SSH host key over the trusted serial console and verify its
fingerprint before creating a dedicated `known_hosts` file. For example,
`dropbearkey -y -f /etc/dropbear/dropbear_ed25519_host_key` prints the public key
and fingerprint; retain the public key with the exact SSH hostname/IP token.
Dropbear may defer key creation until its first incoming connection. If that
key is absent, generate it once through the trusted serial console with
`dropbearkey -t ed25519 -f /etc/dropbear/dropbear_ed25519_host_key`, then print
and pin its public key before opening SSH. Keep an existing key across reboots.
The dedicated file must contain exactly one unhashed `host algorithm key`
entry (`[host]:port` for a nondefault port). There is no automatic key enrollment
or `ssh-keyscan` trust step.

OpenSSH on the collection host is required. Supply either a private password
file with mode 0600 containing the diagnostic root password, or
`--identity-file /absolute/private-key` if a public key was separately
provisioned. Credentials are never copied into the receipt. Password mode uses
a temporary local askpass helper, one explicit password attempt and no
interactive fallback; private-key mode disables passwords and SSH agents.
Neither mode reads ambient SSH configuration, proxy settings or default keys.

```sh
python3 scripts/de25_first_boot.py collect \
  --expectation /absolute/de25-expected.json --board-id DE25-revB-1 \
  --host 192.0.2.25 --port 22 --user root \
  --known-hosts /absolute/de25-known-hosts --password-file /absolute/de25-password \
  --serial-log /absolute/boot-one.log --output /absolute/boot-one-evidence
python3 scripts/de25_first_boot.py verify --receipt /absolute/boot-one-evidence
```

Replace the illustrative host with the observed DHCP address. Collection
requires authenticated SSH 2.0 Dropbear with the pinned host key, AArch64,
the selected kernel/DT model/compatible strings, matching diagnostic files,
the expected root/console arguments, a writable ext4 root matching the
`mmcblk0p2` sysfs device, a unicast Ethernet MAC and carrier. It records the
observed IPv4 address, routes, memory, root mount and boot UUID. An assigned
address alone is not proof of a DHCP lease, and the root device number is not a
filesystem UUID. Raw SSH output/errors, the fixed read-only script, expectation,
known host key and serial log are hashed into a new receipt directory. Failed
connections or identity/protocol mismatches retain diagnostic bytes without
publishing a successful receipt. Existing output paths are refused.
SSH stdout and stderr each have a 1 MiB cap enforced during collection; a
timeout or excess output stops only the collector's own SSH process and keeps
bounded partial logs without a successful receipt.

After a separately authorized **manual reboot**, capture the second serial log
and run `collect` again with a new output directory. The DHCP address may
change; prepare an entry for the new address containing the same independently
verified public host key. Then compare entirely offline:

```sh
python3 scripts/de25_first_boot.py compare \
  --before /absolute/boot-one-evidence --after /absolute/boot-two-evidence \
  --output /absolute/manual-reboot-comparison.json
```

Comparison reparses and verifies both raw captures, requires the same board
label/image expectation, a changed boot UUID, stable SSH host key and stable
MAC, and exits unsuccessfully on any mismatch. A random vendor MAC therefore
stays a visible bring-up failure; the tool does not persist or replace it.
The board label is caller-supplied, and the UTC collection time is unsigned
receipt metadata; verification checks their syntax and that the time is not in
the future but cannot independently attest either value. Imported serial logs
are not independently bound to the SSH boot UUID. Retain board revision/serial
details and review the serial logs alongside the captures. Receipt hashes
detect inconsistency; they are not signed remote attestation or whole-board
acceptance.

## DDR interface preparation without a board

The [shared DE25 DDR contract](../sources/mister-packages/docs/de25-ddr.md)
defines a bounded read-only probe ahead of interactive MENU integration.
It uses the GHRD's 100 MHz F2SDRAM path and a separate lightweight control
endpoint. The probe defaults to no reads; START snapshots one bounded DMA
descriptor, CANCEL drains accepted reads, and response errors remain visible.
Its local IDLE status does not certify recovery across a hard or warm reset.

The offline descriptor planner checks that contract and prints a register-write
plan without opening any device. For example, using an illustrative DMA bus
address rather than an address obtained from a board:

```sh
python3 scripts/de25_dma_descriptor.py probe --dma-base 0x10000000 \
  --grant-bytes 4096 --length 256 --generation 1 --sequence 1
python3 scripts/de25_dma_descriptor.py frame --dma-base 0x10000000 \
  --allocation-bytes 8388608 --slot 1 --generation 1 --sequence 1
```

The frame mode plans a read of one 1280×720 BGRX8888 slot inside a prospective
kernel-owned two-slot allocation. It does not submit a frame for display.
Both modes report `admission: not-qualified`: bounds and an architectural
CPU address are insufficient to establish a Linux resource, allocated DMA
backing, IOMMU stream mapping, firewall coverage or firmware compatibility.
DMA bus addresses can be IOVAs and need not start at the physical DDR base.

The [combined diagnostic](../sources/misteross/boards/de25nano/combined/README.md)
now builds idle and menu-pattern HDMI alongside the preserved HPS/LPDDR4,
mailbox and DDR probe. The display uses its local responder. The combined SOFs
retain the external HDMI timing/electrical gates and unqualified boot pairing.
Their warm-reset acknowledgement is explicitly inactive: probe-local idle
cannot establish safety for the other fabric masters and HPS-origin traffic.

The [Linux DMA ownership diagnostic](../image/kernel/de25-dma/README.md)
implements kernel-owned coherent allocation, descriptor publication and
revocable mappings. ABI 2 adds ordered scanout submission, explicit coherent
status polling and repeated-frame slot ownership: command acceptance does not
release the old slot; a matching complete displayed frame does. The allocation
stays pinned for the configured engine's lifetime until an identity-matched
STOP/drain/CLEAR proof. Ordinary builds refuse physical binding. The default-off
permanent-owner candidate adds allocation/reap control and early overlay
exclusion; normal FES runtime admission remains closed. A separate AArch64 QEMU kernel exercises the real DMA/file/VMA code on a KUnit device:

```sh
python3 scripts/de25_kernel_test.py fetch
python3 scripts/de25_kernel_test.py run --image-output out/de25-nano-linux-cxx \
  --output /absolute/new-kernel-test-output --jobs 8
```

The runner requires clean committed sources, the locked SDK/vendor Image and
the pinned Linux 6.12.11 source commit. The existing builder supplies QEMU;
the compile and boot run with container networking disabled. It checks all
required ownership cases and rejects raw kernel warnings, oopses and
panics before publishing artifact receipts. This test kernel's configuration
and device differ from the board's preserved kernel and FPGA. It establishes
no board module-loadability, stream translation or physical DMA visibility.

The [permanent-owner kernel recipe](../image/kernel/de25-platform/README.md)
builds a separate candidate kernel and compiled base device tree from the locked
Terasic source/SDK. It makes the owner and FPGA suppliers built-in, protects the
region/manager/service bindings and describes the permanent SID-13 device.
The recipe verifies the linked symbols and DTB with pinned libfdt. The separate
offline assembler described with that recipe prepares matching rootfs and boot
files, removes incompatible vendor modules and requires two identical SD image
assemblies. Neither recipe installs an image. Board boot, positive loader/control
integration and physical DMA acceptance remain required.
The [matching image record](validation/2026-10-31-de25-owner-image/README.md)
retains the identical full assemblies, independent verification and receipt
refusals; its candidate kernel is still unbooted.

The [positive owner lifecycle record](validation/2026-10-31-de25-owner-positive/README.md)
executes real OF overlay apply/remove, the owner control callback and private
file-descriptor lifetime through two cycles. Programming/register responses
remain modeled; this is not an SDM firmware-load or userspace /dev syscall test.

The [allocated-buffer FPGA diagnostic](../sources/misteross/boards/de25nano/scanout/README.md)
connects the real menu reader to the preserved memory path. Its shared
`fes.fabric.de25-scanout` contract owns the two-slot grant, full-frame
completion and frozen multiword snapshots. Explicit control and memory bridges
cross between the 100 MHz system and 74.25 MHz pixel clocks. Pixel PLL-lock
loss blanks output and retains live transaction accounting; local CLEAR never
resets the crossing bridges. This diagnostic starts disabled and is separate
from the image and runtime.

Hardware integration still needs a compatible firmware/SOF pair and the real
DMA device/stream binding, bounded register-access behavior, firewall coverage
and global reset/drain lifetime. Interactive MENU also needs the production
presenter integration and exact-artifact physical DMA/HDMI acceptance. The
existing AArch64 runtime guard continues to refuse DE25 hardware access.

## Validation and the board gate

The [2026-10-28 host validation record](validation/2026-10-28-de25-headless-linux/README.md)
retains the exact image, verification and QEMU receipts plus the exported SDK
probe. That record is off-board evidence; the later
[HPS-first hardware record](validation/2026-10-31-de25-hps-first-hardware/README.md)
demonstrates FES Linux boot, Linux FPGA loading and native ZX81 BASIC on its
named diagnostic artifacts. It does not accept this new kernel or managed runtime.

The [scanout ownership record](validation/2026-10-28-de25-scanout/README.md)
retains the sixteen actual AArch64 kernel cases and links the generated
two-clock transport evidence with its explicit RAM-model boundary.

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
