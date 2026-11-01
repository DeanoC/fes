# v1 firmware licensing

**Status:** research record, 1 November 2026, for [#1343](https://github.com/DeanoC/fes/issues/1343). This is not legal advice, and it does not decide anything. It feeds [#1344](https://github.com/DeanoC/fes/issues/1344) (bundle the legal firmware and add the first-use firmware card) and lines up with the licence-notice audit in [#387](https://github.com/DeanoC/fes/issues/387). The v1 scope is the four families Deano chose on 1 November: ZX81, Spectrum, C64 and Atari ST ([v1 first-release decisions](https://github.com/DeanoC/fes/blob/main/docs/plans/v1-first-release-first-boot-setup.md)).

Every source below was fetched on 1 November 2026. Quotations are verbatim. **Unverified** marks a claim for which no primary source was found.

## What #1344 can bundle

- **EmuTOS 1.4 for `fes.atari-st`: yes.** The distribution must provide the GPL notice and exact corresponding source. Make the source tarball available from the same release page as the binary; it need not be embedded in an image or sealed package. Keep the ROM a separate, unmodified file.
- **Amstrad's Spectrum ROMs for `fes.spectrum`, `fes.spectrum128` and `fes.spectrum-plus3`: only if Deano accepts the emulator-permission reading** ([Decision 1](#decisions-for-deano)). The conditions are: copyright messages unaltered, the attribution sentence shown, no charge for the ROMs, and free distribution only.
- **Everything else stays user-supplied in v1.** That covers the ZX80/ZX81/TS1000/TS1500 ROMs, TR-DOS, and the Commodore BASIC/KERNAL/character/1541 ROMs. The user imports them through the existing per-title firmware picker. No open replacement for any of them is good enough for v1.

## How firmware reaches a v1 core today

All ten v1 cores are format-3 packages: a blank-ROM RBF plus a sealed `rom-map.json`. Each declares one required ROM with role `firmware` and an exact `source_size`. The host imports the user's binary into the core-media store, binds it to a library title (`PUT /api/v1/library/core-entries/GAME_ID/rom`), and sends it with the package. The kit's Go linker writes it into the blank M10K lanes at download time. Tenfoot's package-firmware picker does the same binding ([core package library](../../sources/FogCast/docs/core-package-library.md#named-rom-selection-for-format-3-packages)). Library responses report `firmware_required` and `firmware_ready`, and a missing binding blocks launch.

No firmware bytes are in Git or in any sealed package today. The Spectrum README says so ([fes-spectrum](../../sources/misteross/cores/fes-spectrum/README.md)). [`fetch_atari_st_emutos.py`](../../sources/misteross/scripts/fetch_atari_st_emutos.py) downloads EmuTOS for simulation only, pinned by digest.

Late binding matters for licensing. A bundled ROM can be a separate file with its own licence notice, selected by default and replaceable by the user. It never has to be baked into a sealed RBF.

| Core | ROM ID | Size | Contents the linker expects |
| --- | --- | --- | --- |
| `fes.zx81` | `machine-rom` | 8 KiB | ZX81 BASIC ROM. The kit linker also patches ROM INIT when it composes an expansion ([guide](../fes-zx81.md#producers)); the patched bytes exist only on the kit. |
| `fes.zx80` | `zx80-rom` | 4 KiB | ZX80 4K BASIC ROM |
| `fes.ts1000` | `ts1000-firmware` | 8 KiB | ZX81 ROM. The TS1000 used the same bytes. |
| `fes.ts1500` | `ts1500-firmware` | 8 KiB | TS1500 ROM, a ZX81 ROM with 61 bytes changed ([QZX, 1993](https://www.timexsinclair.com/article/rom-differences-in-the-ts1500/)) |
| `fes.spectrum` | `spectrum-firmware` | 16 KiB | 48K ROM |
| `fes.spectrum128` | `spectrum128-firmware` | 32 KiB | 128K or +2 ROM 0 (editor), then ROM 1 (48 BASIC) |
| `fes.spectrum-plus3` | `spectrum-plus3-firmware` | 64 KiB | +2A/+3 ROMs 0–3, including +3DOS in ROM 2 |
| `fes.pentagon` | `pentagon-firmware` | 48 KiB | The standard 128K ROMs (32 KiB), then TR-DOS (16 KiB). The Pentagon simulation that matches Fuse ran "the real 128 ROM and TR-DOS 5.03" ([TR-DOS record](../validation/2026-10-25-spectrum-trdos.md)), not a modified Pentagon 128p ROM. |
| `fes.c64` | `c64-firmware` | 36 KiB | BASIC (8 KiB), KERNAL (8 KiB), character ROM (4 KiB), then 1541 DOS (16 KiB) ([fes-c64](../../sources/misteross/cores/fes-c64/README.md)) |
| `fes.atari-st` | `atari-st-firmware` | 192 KiB | Any 196,608-byte ST ROM at `$FC0000`. EmuTOS `etos192*.img` fits as shipped. |

The core sizes and IDs come from `build_fes_zx81_oss.py`, `build_fes_spectrum_oss.py`, `build_fes_c64_oss.py` and `build_fes_atari_st_oss.py` under `sources/misteross/scripts/`.

## Per-core table

| Core | Firmware | Rights holder | Licence or permission | Conditions to ship | Recommendation | Sources |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.atari-st` | EmuTOS 1.4, `etos192us.img` (or `uk`), 196,608 bytes | EmuTOS team; GEM parts from Caldera/DR | The official project site says the project is distributed under GNU GPL version 2 (GPLv2) (E1); use that explicit release-level statement as the basis for the bundle's **GPL-2.0-only** label. Individual source headers retain their own terms. | Provide the GPL notice with the firmware. Make `emutos-src-1.4.tar.gz` available from the same release page as the binary; same-page source access is sufficient. Pin tag `VERSION_1_4`, commit `978e37569bff…`, archive and ROM SHA-256 as in `fetch_atari_st_emutos.py`. Keep the `.img` an unmodified separate file outside the sealed package and link it on the kit. | **Ship.** Keep the slot open so a user can select their own TOS. | E1–E7 |
| `fes.spectrum` | Sinclair 48K ROM | Amstrad (now Sky, owned by Comcast) | Amstrad's 1999 permission to emulator writers | Copyright messages unaltered. Show Amstrad's attribution sentence. No charge for the ROM code. The bytes are unmodified anyway. | **Ship in the free download if Deano accepts Decision 1**, otherwise user-supplied. Never preinstall on sold hardware without a written licence. | A1–A6 |
| `fes.spectrum128` | 128K ROM 0+1, or +2 ROM 0+1 | Amstrad | Same permission. Lawson: Amstrad bought "Spectrum 48/128" and produced the + machines itself. | Same as 48K | Same as 48K | A1–A6 |
| `fes.spectrum-plus3` | +2A/+3 ROMs 0–3 (+3DOS) | Amstrad | Same permission | Same as 48K | Same as 48K | A1–A6 |
| `fes.pentagon` | 128K ROMs plus TR-DOS 5.03 (the [MiSTer README](https://github.com/MiSTer-devel/ZX-Spectrum_MISTer/blob/master/README.md) lists 5.04T) | Amstrad for the 128K half. TR-DOS is © Technology Research Ltd, "current status unknown". | 128K half: the Amstrad permission. TR-DOS: **no permission found.** Lawson did not grant anything for Pentagon ROMs; asked about them, he only made a joke. | TR-DOS cannot be shipped. The slot takes one 48 KiB binary, so the Amstrad half alone cannot be bundled without a composition step ([Decision 4](#decisions-for-deano)). | **User-supplied** (whole 48 KiB) | A1, T1–T4 |
| `fes.zx81` | ZX81 BASIC ROM | Nine Tiles (John Grant, Steve Vickers). Amstrad disclaims it. | **None found.** One secondary page says "Nine Tiles Networks permits distribution under the GNU GPL"; there is no primary Nine Tiles statement (**unverified**). | None until a rights holder grants permission | **User-supplied.** Ask Nine Tiles ([Decision 5](#decisions-for-deano)). | A1 Q5, Z1–Z3 |
| `fes.zx80` | ZX80 4K ROM | Nine Tiles | Same as ZX81 | Same | **User-supplied** | A1 Q5, Z1 |
| `fes.ts1000` | ZX81 ROM (same bytes) | Nine Tiles | Same as ZX81. The same imported binary serves both cores. | Same | **User-supplied** | Z1 |
| `fes.ts1500` | TS1500 ROM (ZX81 plus 61 bytes) | Nine Tiles for the ZX81-derived base; rights to the 61 changed bytes are not established by these sources | No TS1500-specific permission found. Lawson's "Ask Timex" answer concerns legal Spectrum clones such as the Timex 2048; the World of Spectrum Timex claim follows its statement about TS2000 BASIC, and neither establishes TS1500 permission. | Same | **User-supplied** | A1 Q7, Z1, Z2 |
| `fes.c64` | Commodore BASIC V2, KERNAL, character ROM, 1541 DOS | Cloanto says "Amiga" (amiga.com) | **Not freely redistributable.** Cloanto: "copyrighted works … may only be used or distributed under license." VICE ships them with only a CBM copyright line and no permission statement. Debian strips them. | A commercial licence from the rights holder | **User-supplied** for all four parts. Open ROMs is not viable for v1 (see below). | C1–C6 |

## Evidence

### Amstrad and the Sinclair ROMs

**Primary statement (A1).** Cliff Lawson of Amstrad plc posted it on comp.sys.sinclair on 31 August 1999 and cross-posted it to comp.sys.amstrad.8bit. World of Spectrum archived it as `permits/amstrad-roms.txt` ([archive](https://web.archive.org/web/20021021124228/http://www.worldofspectrum.org:80/permits/amstrad-roms.txt); [comp.sys.amstrad.8bit cross-post](https://groups.google.com/g/comp.sys.amstrad.8bit/c/HtpBU2Bzv_U/m/HhNDSU3MksAJ)). The relevant answers:

> 1) … Amstrad are happy for emulator writers to include images of our copyrighted code as long as the (c)opyright messages are not altered and we appreciate it if the program/manual includes a note to the effect that "Amstrad have kindly given their permission for the redistribution of their copyrighted material but retain that copyright".

> 2) … No one should be charging for the ROM code … we have absolutely no problem with that as long as they aren't, in any sense, charging for the parts of the code that are (c)Amstrad and (c) Sinclair.

> 4) Can you distribute modified ROMs? — If you like (with that (c) proviso).

> 5) … I think Amstrad only bought the rights to Spectrum 48/128 from Sinclair and then produced the + machines ourselves. I do not believe the (c) for ZXs or IF1/2 has anything to do with Amstrad.

> 7) … Ask Timex. We only hold the copyright for code that was written by Sinclair or Amstrad for the Spectrum machines.

Question 8 asked about ROMs from the Scorpion and Pentagon clones. The answer is a joke about punishing pirates, not a permission.

**The "not in hardware" reading (A2).** Andrew Owen wrote it on comp.sys.sinclair in February 2002, and Debian's `spectrum-roms` copyright file quotes him ([Debian](https://sources.debian.org/src/spectrum-roms/20081224-5/debian/copyright/)):

> You cannot embed them in hardware either - this isn't made explicit in the FAQ answer but I have correspondence from Amstrad which clears up the fact - ROMs can only be distributed for use with emulators, not actual hardware.

That correspondence was never published. Debian calls the permission non-free partly because "distribution is only permitted for use with emulators, not actual hardware".

**Fuse's summary (A3).** Fuse's [`roms/README.copyright`](https://sourceforge.net/p/fuse-emulator/fuse/ci/master/tree/roms/README.copyright) covers its 48, 128, +2 and +3 ROMs:

> These are copyright Amstrad, who allow distribution of the ROMs but retain the copyright. You may not sell the ROMs or embed the ROMs in hardware, although it is allowed to sell a product which contains the ROMs, so long as the charge is being made for the product, not for the ROMs themselves.

This combines Owen's hardware clause with Lawson's answer 2. The message Fuse cites holds only the 1999 Q&A ([Debian fbzx copy](https://sources.debian.org/src/fbzx/4.8.0-3/AMSTRAD/)).

**FPGA practice (A4–A5).**
- **ZX Spectrum Next** ([tbblue](https://gitlab.com/thesmog358/tbblue)): its SD distribution ships `48.rom`, `128.rom` and `plus3.rom`. Its LICENSE says the ROMs "cannot be distributed nor modified legally without the permission of Amstrad / Sky plc except in cases already permitted by them (i.e. emulators)", and that the distribution cannot be sold.
- **MiSTer**: the ZX Spectrum core ships a `boot.rom` (with Amstrad ROMs, the Pentagon 128p ROM and TR-DOS 5.04T) in its repo ([README](https://github.com/MiSTer-devel/ZX-Spectrum_MISTer/blob/master/README.md)).

**Consumer emulator practice (A6).**
- **The Spectrum (Retro Games, 2025)** is an Arm/Linux emulator, not an FPGA implementation. The Register says users may load an original ROM image they own and calls that legal; it does not establish permission for Retro Games to distribute those ROMs. The product's built-in ROM is a replacement ([The Register](https://www.theregister.com/2025/12/01/the_spectrum/), [Retro Games support](https://retrogames.biz/support/thespectrum/using-custom-roms/)). This is consumer-emulator practice, not FPGA distribution evidence.

**Current position.** Amstrad was bought by Sky in 2007, and Sky by Comcast in 2018. No statement from either was found that withdraws or reaffirms the 1999 permission.

**Reading for FES.** A free FES download that carries the unaltered 48K, 128K, +2 or +2A/+3 ROMs and shows Amstrad's attribution fits Lawson's text as written. FES loads the ROM as a separate file into FPGA block RAM at launch. That is closer to an emulator's ROM file than to a mask ROM, but Owen's reading would still count an FPGA core as "actual hardware". The Next and MiSTer projects ship anyway. Selling a kit with the ROMs preinstalled is unsettled under every reading.

### TR-DOS and the Pentagon

- **T1.** Philip Kendall's ROM list (July 2002, [archive](https://web.archive.org/web/20020821221048/http://www.srcf.ucam.org/~pak21/spectrum/roms.html)) says of TR-DOS 5.01–6.04: "Copyright Technology Research. Current status unknown." It marks the Russian modified 128 ROMs "Illegal modification of an Amstrad copyright?"
- **T2.** Fuse ships no TR-DOS or Pentagon ROM. Its [man page](https://sourceforge.net/p/fuse-emulator/fuse/ci/master/tree/man/fuse.1) says: "Note that not all these ROMs are supplied with Fuse — you must supply your own copies of those which are not."
- **T3.** MAME refers to the TR-DOS ROMs only by checksum ([`beta128.cpp`](https://raw.githubusercontent.com/mamedev/mame/master/src/devices/bus/spectrum/beta128.cpp)), and its [legal page](https://www.mamedev.org/legal.html) says users need rights to the ROMs they use.
- **T4.** No open TR-DOS replacement was found.

### ZX80, ZX81, TS1000 and TS1500

- **Z1.** The World of Spectrum [Sinclair BASIC page](https://worldofspectrum.net/legacy-info/sinclair-basic/) says the ZX80 and ZX81 rights stayed with Nine Tiles, then separately says the TS2000 BASIC rights stayed with Timex. It later asserts that Nine Tiles Networks permits GPL distribution and that Timex permits distribution, without citing a grant or identifying which Timex ROMs that permission covers. No primary Nine Tiles or Timex statement was found (**unverified**); the page does not establish permission for the TS1500-specific bytes.
- **Z2.** Fuse's `se-0.rom` and `se-1.rom` (OpenSE BASIC) say "copyright Nine Tiles Networks Ltd, with modifications and extensions by Andrew Owen … GNU GPL v2 or later" (A3). That GPL grant covers Owen's derived work and rests on the same unverified Nine Tiles position.
- **Z3.** The Spectrum Next ships `zx80.rom` and `zx81.rom`, but its README credits "The ZX 80 and ZX 81 EMULATORS" to Paul Farrow. Whether those files hold the original Nine Tiles code was not checked.

Open replacements: OpenSE BASIC ([SourceForge](https://sourceforge.net/projects/sebasic/), GPL-2.0-or-later) and SE BASIC IV ([GitHub](https://github.com/source-solutions/sebasic4), GPL-3.0-or-later) are Spectrum-class BASICs derived from TS1000 BASIC, not drop-in ZX80/ZX81 ROMs. They depend on the same unverified grant. Neither is a 48K-compatible ROM for software that calls ROM routines. No open ZX80 or ZX81 ROM was found.

### EmuTOS

- **E1.** The [official site](https://emutos.sourceforge.io/) says: "It is distributed under the GNU General Public License, version 2 (GPLv2)" and "It can be freely used and redistributed with emulators that respect the GPL licence." The repo's licence file is [`doc/license.txt`](https://raw.githubusercontent.com/emutos/emutos/master/doc/license.txt), the GPL v2 text; there is no `COPYING`. EmuTOS-authored files (for example `bios/bios.c`) say "GPL, version 2 or at your option any later version". The Caldera/DR GEM files (for example `desk/deskwin.c`) say "licenced under the GNU Public License" without a version. The project's explicit version-2 statement, rather than an inference from missing header wording, is the source for the release-level label in the table.
- **E2.** 1.4 is current, released 7 June 2025 ([download page](https://emutos.sourceforge.io/download.html)), at tag `VERSION_1_4` = `978e37569bff95841e42675d11fcc6799aad8483`. `emutos-192k-1.4.zip` holds the `etos192*.img` images, `doc/license.txt` and `readme.txt`, but **no source**. The source is the separate `emutos-src-1.4.tar.gz` in the [same folder](https://sourceforge.net/projects/emutos/files/emutos/1.4/). Upstream gives no written offer.
- **E3.** [GPL v2](https://www.gnu.org/licenses/old-licenses/gpl-2.0.html):
  - §1 requires that recipients get "a copy of this License along with the Program".
  - §3 requires either (a) the "complete corresponding machine-readable source code" or (b) "a written offer, valid for at least three years".
  - §3(c), passing on an offer you received, is "allowed only for noncommercial distribution and only if you received the program in object code or executable form with such an offer". FES received no offer, so (c) is unavailable.
  - §3's last paragraph says that "offering equivalent access to copy the source code from the same place counts as distribution of the source code".
  - "Corresponding" source means the source of the exact binary shipped, which is why the version pin matters.
  - For v2, the GPL FAQ says: "If you distribute binaries via FTP, you should distribute source via FTP" ([FAQ](https://www.gnu.org/licenses/gpl-faq.html#DistributeWithSourceOnInternet)). Its answers that allow source on another server rely on GPLv3 §6 and do not apply to v2.
- **E4.** Aggregation: GPL v2 §2 says "mere aggregation of another work not based on the Program with the Program … on a volume of a storage or distribution medium does not bring the other work under the scope of this License" ([FAQ](https://www.gnu.org/licenses/gpl-faq.html#MereAggregation)). This holds while the `.img` stays a separate file that the kit loads at launch, and FES's late binding already works that way.
- **E5.** Image choice: `doc/install.txt` puts `etos192*.img` at `0x00fc0000`, the 520ST address. `readme-192k.txt` says the 192 KiB images suit "ST / STf, Mega ST", "only work with a plain 68000 CPU", and have "limited desktop features (comparable to Atari TOS 1)".
- **E6.** Compatibility ([`doc/incompatible.txt`](https://raw.githubusercontent.com/emutos/emutos/master/doc/incompatible.txt)): failures are "invariably caused by the use of undocumented addresses or interfaces". It says some floppy-only games "use very low memory areas that are also used by TOS… This can never work with EmuTOS" and gives Turrican as an example. Hatari's [EmuTOS notes](https://raw.githubusercontent.com/hatari/hatari/main/doc/emutos.txt), however, list Turrican among commercial games that work fine with EmuTOS. These sources conflict, so Turrican is not a settled incompatibility example. STOS games can lose input.
- **E7.** Practice:
  - [Hatari](https://raw.githubusercontent.com/hatari/hatari/main/doc/emutos.txt) ships an unmodified EmuTOS as `tos.img` with its binary releases.
  - [MiSTer's Atari ST core](https://github.com/MiSTer-devel/AtariST_MiSTer) bundles none: "Put the core.rbf and the TOS as tos.img to the SD-Card."

### Commodore 64

- **C1.** Cloanto's [KB 13-122](https://www.c64forever.com/kb/13-122) says "the older CBM 8-bit system ROMs (PET/CBM, C64, etc.), are copyrighted works owned and published by Amiga, and may only be used or distributed under license". It adds that its licences "include the right to sublicense, and are not limited to emulation purposes".
- **C2.** The [VICE README](https://raw.githubusercontent.com/VICE-Team/svn-mirror/main/vice/README) says only: "The ROM files in the `C128', `C64' … `DRIVES' … directories are Copyright C by Commodore Business Machines". It gives no permission. VICE's inclusion is therefore not a basis for FES to redistribute.
- **C3.** Debian's [`README.ROMs`](https://sources.debian.org/data/contrib/v/vice/3.9+dfsg-1/debian/README.ROMs) says the ROM images "are copyrighted, and you install them at your own legal risk" and tells users to obtain them outside the package. Debian's [`copyright` file](https://sources.debian.org/src/vice/3.9+dfsg-1/debian/copyright/) says the ROM files "have been removed from the Debian source package". Neither source states a general prohibition on redistribution.
- **C4.** The [X16 ROM licence](https://raw.githubusercontent.com/X16Community/x16-rom/master/LICENSE.md) points users to Cloanto for its Commodore KERNAL and BASIC.
- **C5. MEGA65 Open ROMs** ([repo](https://github.com/MEGA65/open-roms)):
  - **Licence:** [LICENSE](https://raw.githubusercontent.com/MEGA65/open-roms/master/LICENSE) is "GNU Lesser General Public License … version 3 of the License, or (at your option) any later version". Some BASIC files are under Microsoft's MIT licence. The X16 project's licence file calls the same component GPLv3.
  - **Status** (`STATUS.md`): missing "most BASIC commands", BASIC variables and arrays, expression handling and RS-232. Tape is LOAD only.
  - **Prebuilt binaries:** the files in `bin/` (`kernal_generic.rom`, `basic_generic.rom`, `chargen_openroms.rom`) date from 24 August 2021. Its hybrid KERNALs "cannot be distributed due to legal/copyright reasons".
  - **No 1541 ROM:** Open ROMs drives the IEC bus from the KERNAL and has no drive ROM, but FES's 1541 runs real drive firmware.
  - **Product obligations:** as LGPLv3, Open ROMs on a user-updatable device would carry GPLv3 §6 "Installation Information" duties.
- **C6.** cbmbasic is a native port, not a ROM. sd2iec is IEC device firmware, not a 1541 ROM. No open 1541 DOS ROM was found.

For v1, Open ROMs would boot to a prompt but would not run BASIC programs. It would also leave the 1541 slot without firmware. So the C64 stays user-supplied whatever the licence position. The user supplies one 36 KiB image today. Users typically hold four separate files (for example VICE's or the original chips' dumps), so #1344 should consider assembling the slot from them.

## First-use firmware card text

If Spectrum ROMs ship, the card and the release notes should carry Amstrad's requested sentence verbatim:

> Amstrad have kindly given their permission for the redistribution of their copyrighted material but retain that copyright.

The EmuTOS card should say that EmuTOS 1.4 is free software under the GNU GPL version 2, and point to the bundled licence and source.

## Decisions for Deano

1. **Ship Amstrad's Spectrum ROMs in the free download?** The options are to rely on Lawson's 1999 permission, as the Spectrum Next and MiSTer do, or to keep the ROMs user-supplied until Sky or Comcast confirms in writing. *Recommended:* ship them as separate, replaceable firmware objects in the free download, with the attribution and no charge. Keep the user-supplied path, and remove them if a rights holder objects.
2. **Sold or preinstalled hardware.** No reading settles whether the Amstrad ROMs may be preinstalled on a kit that is sold. *Recommended:* none preinstalled on sold hardware without a written licence from the current rights holder. EmuTOS has no such limit beyond the GPL.
3. **Which Spectrum ROM versions to recognise.** This covers the 128K or +2 ROM 0, and +3 ROM 4.0 or 4.1 (MiSTer bundles 4.1). Current Spectrum kit records identify package and RBF hashes but not firmware digests. *Recommended:* have #1344/#1353 record each ROM's SHA-256 alongside the package and RBF hashes before describing a version as having passed a kit check. Then bundle only those identified versions and accept the others as user-supplied.
4. **Pentagon firmware composition.** The 48 KiB slot mixes the Amstrad 128K ROM with TR-DOS, which has no permission. *Recommended for v1:* keep the whole 48 KiB user-supplied. Later, #1344 or a follow-up could compose the bundled 128K half with a user-supplied 16 KiB TR-DOS.
5. **Ask Nine Tiles and Timex.** The ZX80, ZX81 and TS1000 ROMs have only a second-hand claim of a Nine Tiles GPL grant. The TS1500 ROM has 61 bytes changed from the ZX81 version, and the cited sources do not establish who controls those changes or grant permission for them. *Recommended:* keep all four user-supplied for v1 and seek a written Nine Tiles grant for the shared ZX80/ZX81/TS1000 code. A Nine Tiles grant alone would not clear the TS1500-specific bytes; seek written permission from Timex or the verified rights holder for those before shipping the TS1500 ROM.
6. **EmuTOS image and where its source goes.** The repo pins `etos192us.img`, which is the image tested in simulation and on the kit. The archive also holds other language images, such as `etos192uk.img` with a UK keyboard. *Recommended:* ship the tested `us` image first. Add a language choice from the same pinned archive only after the release HIL checks it on the PAL core. Make the exact source tarball available from the same release page as the binary; it does not need to be embedded in the image or a sealed package.
7. **Commercial C64 licence.** *Recommended:* user-supplied for v1. Only ask Cloanto or the current rights holder if a bundled C64 becomes a product goal.

## Notes for #387 and #1344

- The Atari ST RBF already contains FX68K under GPL-3.0-or-later. That notice belongs to the #387 RBF audit, not to firmware.
- `fetch_atari_st_emutos.py` describes EmuTOS as "GPLv2-or-later". For the bundled release label, use the EmuTOS project's explicit statement that the project is distributed under GPL version 2 (E1); do not infer the label from the absence of "or later" in another component's header.
- A bundled firmware object needs a manifest: name, version, SHA-256, licence, notice text and source location. It should be a separate core-media object that the host imports and selects by default, never part of a sealed package.
