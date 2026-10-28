# DE25 boot firmware, device tree and HPS handoff inspection

The committed inspector `6229b5c46529148e7b2bf48fcca45b2bfb1419dc` verifies the
existing headless image and separately retained HPS build without programming
or contacting a board. The [report](boot-contract.json) binds its own script
hash, original image producer, boot files, selected FIT payloads and HPS handoff.
Evidence is host-only; physical boot and custom firmware pairing remain open.

```sh
python3 scripts/de25_boot_contract.py \
  --image-output out/de25-nano-linux-cxx \
  --hps-output /home/deano/.codex/worktrees/c134/fes/out/dev/de25-nano-bringup/out/de25-hps-reset-4ab2472b0b \
  --output out/de25-boot-contract-6229b5c465.json
```

## Preserved boot pair

The image receipt and every artifact hash pass verification. The card remains
SHA-256 `e8836b89667899efe1350d94668644fda19ebf397a3c18d770f6e93dd2d3e854`;
this inspection does not rebuild or replace its rootfs, kernel or firmware.
It retains Linux `6.12.11-gd7d192a9ddd9` and the BSP's exact DTB/script/FIT.

The FIT configuration `board-0` selects ATF, U-Boot and its embedded device
tree. All selected external payload CRCs pass, with bounds checked relative
to the aligned 1,216-byte FIT structure; the report records SHA-256 identities
for the extracted payload bytes. The U-Boot image's compiled environment names
`/dev/mmcblk0p2`, matching the card. A saved environment on a physical board
can differ and remains an observation to retain at first boot.

The script enables FPGA bridges, loads `Image` and
`socfpga_agilex5_de25_nano.dtb`, and overrides stale DTB chosen arguments with
`console=ttyS0,115200 root=${mmcroot} rw rootwait`. The DT's serial0 alias selects
UART1 at `0x10c02100`; ethernet0 selects the enabled MAC at `0x10810000`.
Those are declarations for the preserved BSP, not hardware acceptance.

The BSP factory QSPI JIC is SHA-256
`e3d20c2d066761fd02897422ba361aa19faafcd0240dd2782965754cc61b38a4`.
The resource package's GHRD JIC instead hashes to
`f0326cb10f2b1c6109dff2b124f7875e3dc719a6682201dc9753a54422c7c9b9`.
Both have 16,777,447 bytes. Their different identities establish no safe
substitution or source provenance between them; the factory dependency stays
the exact JIC selected by the console BSP lock.

## RAM declarations and firmware evidence

The Linux DT declares a 2 GiB default at `0x80000000`. The U-Boot FIT's embedded
DT declares 1 GiB there; the resource SPL's embedded DT also declares 1 GiB.
This static difference is not evidence that Linux will use nonexistent RAM.
The source matching U-Boot's `2025.01-gd0f8813fd6bf` banner implements detected
RAM capacity, SPL bank handoff and Linux memory fixups:

- [Board DT's 1 GiB override](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/dts/socfpga_agilex5_de25_nano-u-boot.dtsi#L60).
- [DDR capacity detection and bank reconstruction](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/drivers/ddr/altera/sdram_agilex5.c#L289).
- [SPL handoff bank import](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/mach-socfpga/board.c#L60).
- [Linux memory-bank fixup](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/arch/arm/lib/bootm-fdt.c#L34).
- [Linux board DT's raw default](https://github.com/terasic/linux-socfpga/blob/d7d192a9ddd955fe206aa350afed8f82e368a74c/arch/arm64/boot/dts/intel/socfpga_agilex5_de25_nano.dts#L34).

The pinned U-Boot defconfig enables SPL Altera SDRAM and BLOBLIST/HANDOFF.
The [source review](source-review.json) retains immutable revisions and file
hashes. A separate [SPL DT inspection](spl-dtb-inspection.json) checks every
Intel HEX record checksum and the exact extracted DTB bytes.
Actual detected capacity, effective DT and booted RAM remain board observations.
The binary inspector reports raw declarations rather than inferring those values.

## Custom HPS and address boundary

The Linux DT has an FPGA region/manager, but no lightweight bridge window,
mailbox endpoint or region ranges. The generic SoC identity mapping does not
establish a CPU mailbox address. U-Boot's embedded firewall nodes at
`0x10d21200` and `0x10d21300` describe security-register initialization, not
qualified bridge windows. The inspector leaves the CPU address unestablished.

The optional HPS input authenticates its build/input receipt bindings and the
SOF/handoff hashes for producer `4ab2472b0b22b64d8756fd7c6e83d117e794b214`.
The handoff has 1,629 bytes and SHA-256
`a17de6f2dbf5f4e93643b50b4c8291a33f930064f9a43bad3ece24e9c3dda04a`.
The independent source comparison finds the original resource HPS IP, EMIF IP
and bundled SPL hex unchanged. Matching source parameters/resource identity
supplies no provenance tying this regenerated bitstream to the BSP factory JIC.
The tool therefore records `factory_boot_pairing: not-qualified`.

Linux declares Agilex 5 SMC service and FPGA-manager interfaces. The existing
Cyclone V programming, reset and DDR mappings remain unavailable on this target.
This inspection adds no runtime profile, image substitution or MMIO access.

## Checks and remaining work

Sixteen [focused tests](focused-tests.txt) pass, including bounded FDT/FIT
decoding, selected configuration, payload corruption, same-size replacement
after initial verification, HPS receipt mismatch and symlink refusal. The
real-artifact invocation above passes against the preserved image and HPS build.
[Consistency](consistency.txt) checks 23 generated consumers and 37 fixtures;
the [deployment classifier](hil-plan.json) reports `NO_DEPLOY_CHANGE`.
The [affected parent suite](test-changed.json) passes 1,068 tests (two skipped)
for the committed inspector. Its selected lanes contain no host, runtime,
contract or FPGA source changes.

Before custom bridge access, establish an identified-board firmware/handoff/DT
pairing and a real CPU bridge map. First physical Linux acceptance remains serial
boot/login, rootfs identity, DHCP, SSH, reboot and persistent host keys. No card,
FPGA, QSPI, kit or block device was touched by this inspection.
