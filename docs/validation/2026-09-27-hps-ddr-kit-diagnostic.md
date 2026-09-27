# HPS DDR: kit diagnostic of the RAM tester's three fpga2sdram ports

On 2026-09-27 the designated MiSTer Pi (`192.168.10.84`) ran `fes.ramtest`
1.1.0, which declares `fes.memory.hps-ddr` 1.0. It scanned the whole core DDR
window on all three fpga2sdram ports and passed:

- the sealed OSS package at 100 MHz
- the Quartus 17.0.2 comparison packages at 100 and 130 MHz

The runtime released the SDR FPGA ports only for packages that declare the
interface, and only after the layout check. A package without the interface
kept them in reset.

This is a **hardware diagnostic**. The packages are exact artifacts. The
runtime was a diagnostic build, temporarily bind-mounted over the kit's
installed one, and the loads were development core loads. It is not image,
package or appliance acceptance.

## Setup

- Kit lease: the helper claimed it as `claude-hps-ddr` for each sequence and
  released it at the end. Stop ran before every release.
- Boot: `/media/fat/menu.rbf` is the resealed FES splash
  `43dc7e9db350dbdef87b290bfde61f8df37483957c93e81cbd753c30d4cd93b6`
  (misteross `808bd7c2`), which carries the port layout. The earlier card
  `menu.rbf` is kept on FAT as `bob-menu-idle-smoke.rbf`. U-Boot latched the
  layout at boot, and the new splash stays in place.
- Runtime: `16d805e199976fa9b75b789f521a97801413b49217185a1e72e5824374ea89c8`,
  built from FES `0eb54627` (libmister-runtime HPS DDR port release). It was
  streamed to `/tmp`, bind-mounted over `/usr/sbin/mister-runtime`, and
  restarted by its supervisor. The later window change `fdedc5f1` only
  regenerates window constants the runtime does not read. Afterwards the
  bind mount was removed, the installed runtime `d5776191…` restarted and
  reached idle, and the staged file was deleted. The installed image was
  never modified.
- Verification: ShadowCast HDMI capture at 1280×720 of the tester's status
  screen. SDR mirror registers and `FPGAPORTRST` (`0xFFC25080`) read with
  `busybox devmem`. After a scan, a Linux read of the ADDR signature
  `{~a, a}` across the window.

## Packages

| Package | Lane | Memory clock | Package ID | Build ID |
| --- | --- | --- | --- | --- |
| OSS seal | Yosys 54ea7109, nextpnr f95cef3d (seed 4) | 100 MHz, signoff 103.30 MHz | `e4251e820533677eda97dde2e52aa377560ccf36a58312220aa20b825628fbce` | `760d9d6009265338297d871afee8f607` |
| Quartus comparison | Quartus 17.0.2 | 130 MHz, slack +0.520 ns | `9813e3ce721524d7bef38310656038854064c0dcba16025b6ca8f76a5ccc5d97` | `8f6dbd7c13d6ba03aa5d8477b7d67170` |
| Quartus comparison | Quartus 17.0.2 | 100 MHz | `bdccabccdd6e25d863679e604b86266ff6a17ed690299171ff53a56951238456` | `faf5475111d28ebe201ea2befebf5868` |
| `fes.pong` (no interface) | merged baseline `e6a85546` | — | `55497314c644d193638c861800eb6facd592f2413a6a7d27fc3f40744ce57b84` | `771e0be93ad21679d8bcbd02561b29c3` |

The OSS package was sealed from misteross at FES `5c15931a`. The OSS lane
does not close 130 MHz yet ([#264](https://github.com/DeanoC/fes/issues/264)).

## Results

| Package | SDRAM | P0 (128-bit) | P1 (64-bit) | P2 (64-bit) | MB/s write / read: P0, P1, P2 |
| --- | --- | --- | --- | --- | --- |
| OSS 100 | PASS | 7/7 PASS, 0 errors | 7/7 PASS, 0 errors | 7/7 PASS, 0 errors | 1573/1564, 702/708, 702/703 |
| Quartus 130 | PASS | 7/7 PASS, 0 errors | 7/7 PASS, 0 errors | 7/7 PASS, 0 errors | 2023/2007, 818/864, 818/918 |
| Quartus 100 | PASS | 7/7 PASS, 0 errors | 7/7 PASS, 0 errors | 7/7 PASS, 0 errors | 1571/1566, 736/687, 736/679 |

Port gating, with `FPGAPORTRST` at `0xFFC25080`:

- **Splash as Stop-idle** (earlier sequence, with the splash bind-mounted as
  `idle.rbf`): the mirrors read the splash's layout, and `FPGAPORTRST` was
  `0` (`splash-as-idle.png`).
- **Tester loaded:** the mirrors read `CPORTWIDTH 0x016`, `CPORTWMAP` and
  `CPORTRMAP 0x0D0`, `RFIFOCMAP` and `WFIFOCMAP 0x2100`, `CPORTRDWR 0x03F`
  and `PORTCFG 0`. The runtime logged `hps_ddr.ports ok`, and
  `FPGAPORTRST` was `0x3FFF`.
- **After Stop:** the image's own idle core was loaded. It has no fpga2sdram
  cell, so the mirrors read all ones and `FPGAPORTRST` returned to `0`.
- **Pong loaded:** Pong has no fpga2sdram cell, so the mirrors read the
  undriven all-ones pattern. `FPGAPORTRST` stayed `0` and no
  `hps_ddr.ports` event was logged.

Linux signature check, after the OSS 100 and Quartus 130 scans:
- Every sampled lane from `0x30000000` to `0x3FFFFFFC` held `{~a, a}`, for
  example `0x3FFFFFF8 → 0x3FFFFFF8, 0xC0000007`.
- The kernel stayed healthy (no new dmesg, uptime continuous).

## FES U-Boot loading FAT `/idle.rbf`

Later the same day, with explicit authorization for this device, the kit's
card was updated in place. The card is `/dev/mmcblk0`, `SD64G`, serial
`0x6e6a902d`, in the appliance layout with the A2 partition p2 at sector
2099200. The update ran under the kit lease:

1. **Backup.** The whole 1 MiB of p2 was saved to this host and to FAT
   `/fes-backup/p2-upstream-e2d46cf9.bin` (sha256 `59cebe7a…`). Its first
   515141 bytes are the upstream MiSTer U-Boot, `e2d46cf9…`.
2. **U-Boot.** The derived FES U-Boot,
   `21533e9903675329e3273aebced63c87dfa781f79327c121f24153ee2a11ecb9`
   (`core=idle.rbf`), was written to p2. The read-back matched, and the rest
   of the partition was unchanged.
3. **FAT.** FAT `/idle.rbf` was added: the sealed splash `43dc7e9d…`. FAT
   `/menu.rbf` was renamed away for the test, so only `/idle.rbf` could
   supply a layout.
4. **Reboot.** A warm reboot, not a power cycle, gave boot ID `c0bd4172…`.
   The installed runtime reached idle.

On that boot:

- **The gate refused DDR.** The runtime built from FES `877f382c`
  (`ef51402e…`) was bind-mounted over the installed runtime. The installed
  runtime had already loaded its own idle core (`feb0a66a…`, no fpga2sdram
  cell), so the new runtime's boot capture read all-ones mirrors. It
  recorded `absent` (`hps_ddr.boot`, `latched=false`), and the OSS 100
  package load was refused. That is the correct result for what it could
  observe.
- **The scan passed from `/idle.rbf`.** The record was then seeded with
  `latched` by hand, an operator override for this diagnostic only. The
  runtime was restarted and the same package loaded. `FPGAPORTRST` became
  `0x3FFF`, and SDRAM plus all three DDR ports passed 7/7 with zero errors.
  MB/s write/read: P0 1572/1564, P1 704/690, P2 703/684. The Linux signature
  check held `{~a, a}` from `0x30000000` to `0x3FFFFFFC`.
- **What that proves.** With `/menu.rbf` absent, a failed core load would
  still have been followed by `bridge enable` latching unusable values. So
  the pass shows the FES U-Boot programmed FAT `/idle.rbf` and latched its
  layout.
- **Cleanup.** The bind mount and the seeded record were removed. The
  installed runtime `d5776191…` restarted and reached idle, and `/menu.rbf`
  was restored. The card now keeps the FES U-Boot, `/idle.rbf` and
  `/menu.rbf`.

**Not covered:**
- A cold power cycle.
- The boot capture seeing the U-Boot core itself. That needs an image whose
  installed runtime has the gate, so that the gated runtime is the first to
  program in a boot.
- Exact-artifact media acceptance. The rest of the card (kernel, rootfs,
  agent) is still the earlier image.

Evidence: `uboot-idle-oss100-pass.png` and `uboot-idle-run.log`.

On the same boot, with the same seeded record and runtime, the OSS 100 MHz
package resealed on the merged tool pins was scanned. The pins are Yosys
`b27035fc` and nextpnr `f60b33aa`, from misteross at FES `877f382c`, seed 1,
with signoff memory 100.75 MHz and pixel 76.65 MHz. The package is
`4b6847eed6d9846722577cf03c6d0b555acf54d5b70dfa322a007761de777d1c`
(archive `96e4327c…`, build `810c0554ac8c5abf9119b5c557a81488`). SDRAM and all
three DDR ports passed 7/7 with zero errors. MB/s write/read: P0 1571/1563,
P1 712/707, P2 712/703. `FPGAPORTRST` was `0x3FFF` while it ran and `0`
after Stop. Evidence: `merged-oss100-pass.png` and `merged-oss100-run.log`.

## Found on the way

- **Old splash:** with the pre-FES card `menu.rbf`, U-Boot latched unusable
  port values, and every DDR command was refused. The tester showed `NACK`
  (`before-old-tester-hps-fail.png`). The splash now carries the layout.
- **Window overlap:** a window starting at `0x20000000` overlapped the
  MiSTer_fb console at `0x22000000`. fbcon's cursor corrupted port 0 at
  `0x22001000`, giving 128 errors (`quartus130-fbcon-overlap.*`). The
  contract window is now `0x30000000`–`0x3FFFFFFF`.

## Evidence

In [`hps-ddr-kit-2026-09-27/`](hps-ddr-kit-2026-09-27/):

- `oss100-pass.png`, `quartus130-pass.png` and `pong-no-interface.png`, with
  their register reads and signature checks in `final-run.log`
- `quartus100-pass.{png,log}`
- `splash-as-idle.png`, `before-old-tester-hps-fail.png` and
  `quartus130-fbcon-overlap.{png,log}`

## Next

1. Image integration: build the image with this branch's runtime so that the
   port release and boot capture need no bind mount.
2. Provision media with the FES U-Boot and `/idle.rbf`, and check a cold
   boot.
3. Run package acceptance of the OSS package through a library launch.
