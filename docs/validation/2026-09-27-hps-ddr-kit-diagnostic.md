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

Image integration: build the image with this branch's runtime so that the
release is not a bind mount. Then run package acceptance of the OSS
package through a library launch.
