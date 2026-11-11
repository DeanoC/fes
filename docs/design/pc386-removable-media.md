# pc386 removable media: floppy drives and a CD-ROM

Status: proposed design and slice plan, for Deano, 11 November 2026. Refs [#1694](https://github.com/DeanoC/fes/issues/1694). Nothing here is implemented. Open decisions are marked **D*n*** and listed at the end.

Path convention: `M/` is `sources/misteross/`, `MP/` is `sources/mister-packages/`, `RT/` is `sources/libmister-runtime/`, `FC/` is `sources/FogCast/`. `docs/`, `scripts/` and `AGENTS.md` are at the repo root.

## Summary

pc386 boots DOS from one ATA hard-disk image in HPS DDR. It has no floppy drive and no CD-ROM (`M/docs/pc386-machine-design.md:65`, `:238`). That blocks four kinds of software:

- games that install from, or ask for, a second disk;
- games that need a CD, including those with CD-DA music;
- booting from a floppy or a CD;
- early Windows, OS/2 and Linux installs.

The design:

- **Floppy.** An AT floppy controller (8272A/82077AA register subset at 3F0h–3F7h, IRQ 6, DMA channel 2) with two drives. The default is A: 3.5" 1.44 MB and B: 5.25" 1.2 MB.
  - Each disk image sits whole in a fixed region of the HPS DDR window. The core reads and writes sectors there over port 2, as the ATA disk does.
  - Disks can be swapped while the machine runs. The AT disk-change line (DIR bit 7) tells the guest about the swap.
  - Writes reach the image at once. A dirty disk is saved as a whole-image record keyed by that disk's base image, at eject and at Stop.
- **CD-ROM.** An ATAPI CD-ROM drive, master on the secondary IDE channel (170h–177h, 376h, IRQ 15).
  - **Recommendation: ATAPI.** The locked Bochs BIOS already drives ATAPI and boots El Torito CDs (`rombios.h:110-111`). Every later target has a driver built in: Windows 95/98, OS/2 Warp, and Linux's `ide-cd`. DOS games never touch the interface; they call MSCDEX.
  - **The command set runs on the HPS, not in RTL.** A CD image (up to about 850 MB) cannot fit in the 256 MiB window, so the runtime has to serve the image on demand whatever the RTL does. The core is therefore a thin ATAPI transport: a task file, a packet buffer and a PIO streamer. The runtime's MMC interpreter answers each 12-byte packet, from a C library that `machine_tb` links too.
  - **CD-DA.** The runtime fills an audio ring in the window. A small player in the core reads it at 44.1 kHz into the mixer.
- **Media delivery.** Floppy images go into the window whole, through fes-window grants, while the machine runs. A CD stays a file on the kit. Only the sectors a command asks for, and the audio ahead of the play position, pass through the window.
- **ABI.** Three new `fes.computer` interfaces: `fes.media.pc-floppy` on units 1 and 2, `fes.media.atapi-cd` on unit 3, and unit-indexed Disk operations. The runtime gets a running-time CD service thread. FogCast gets a CD role, media sets (disk 1..N, disc 1..N), and a drive selector in the live-media API and the tenfoot picker.
- **Area.** About 2,300 ALMs and 7 M10K in all. That takes the current seal from 36,118 ALMs to about 38,400 (91.6%), right at the 92% ceiling the area audit found untested. CPU Phase 5 is still landing, so area is the main risk. It is open question **D7**.

## Goals

1. Install and run DOS games from multi-disk floppy sets, with in-game disk swaps that work.
2. Run CD games: data streaming, CD checks, CD-DA music and multi-disc swaps.
3. Boot DOS, Windows 98 setup and Linux from a floppy or an El Torito CD.
4. Keep disk writes durable, so saved games written to a floppy survive Stop and swaps.
5. Verify each device against Bochs's own device models, as the VGA is verified against `vgacore.cc`.

## Non-goals

| Not built | Why |
| --- | --- |
| Flux-level or copy-protected floppy formats (`.86f`, `.imd`, `.td0`, `.ipf`, weak sectors, XDF) | Raw sector images cover the install media on the LAN share and eXoDOS. A key-disk title needs a protected format and is a later slice if one matters |
| Proprietary CD interfaces (Mitsumi, Panasonic/Sound Blaster, Sony) | See [CD-ROM interface](#cd-rom-interface) |
| ATAPI DMA, CD writing, DVD | DOS and Windows 9x drive a CD by PIO well enough at period speeds. pc386's ATA disk is PIO only too |
| `.chd`, `.mds`, `.nrg`, `.ccd` images as inputs | FogCast can convert them to `.cue`/`.bin` on import later. `.iso` and `.cue`/`.bin` are what eXoDOS and the LAN share hold |
| A second hard disk | Its own design. Unit 0 stays the one ATA disk |
| Floppy timing exact to the rotation | Seek, step, spin-up and byte rate are modelled. Index pulse and rotational position are not |

## What exists

### PC/XT floppy

`M/cores/fes-pcxt/expansions/pcxt_fdc_card.sv` is a 765-family XT adapter (`:1-16`), and its limits shape what pc386 can take from it:

- **One drive, 360 KiB only.** The LBA is 10 bits and the geometry is hard-coded: `read_cylinder * 18 + 9` (`:337`), LBA below 720 (`pcxt_sector_writer.sv:43`).
- **XT guest-clock constants** for spin-up, step and byte time (`:64-71`).
- **The XT ISA endpoint** for its bus interface (`:2`, `:118-124`).
- **DMA on channel 2** through that endpoint (`:127-129`), and IRQ 6 as `8'h40` (`:132`).
- **No DIR or CCR register.** The guest learns of a change only because the drive goes not-ready (`:311-317`).
- **Writable.** `pcxt_fdc_card_rw` stages a whole sector before it reaches the image (`:42-46`, `:96-116`).
- **Licence:** GPL-2.0-or-later (`:1`). pc386's RTL is MIT (`M/cores/fes-pc386/rtl/pc386_disk.sv:1`).

Around the card:

- The image is a full copy in SDRAM, written two bytes per mailbox transaction (`fes_computer_mailbox.v:793-830`).
- `pcxt_media_arbiter.sv` shares the SDRAM port between host loads, guest writes, FDC reads and snapshot reads (`:1-4`).
- `pcxt_boot_media_gate.sv` holds the CPU after a cold start until the first disk commits, unless the host releases it with the drive empty (`:1-15`).
- Persistence is the whole-image `fes.pcxt-floppy.image` record (`RT/src/native/media_data.cpp:16`, `:188-225`).

**What carries over is the design, not the module:**

- stage a whole sector before it reaches the image;
- fence writes with `frozen` and drain an accepted sector;
- keep a sector accepted before a warm reset;
- the boot gate's rule;
- the whole-image record.

The command state machine is the one large reusable piece, but it is 360K-only, XT-clocked, built on the XT ISA endpoint and GPL. Whether pc386 shares one FDC with PC/XT is **D5**.

### pc386 today

**ATA disk.** `pc386_ata.sv` (`:1-50`) and `pc386_disk.sv` (`:1-21`) implement `fes.media.ata-disk` 1.0:

- the image sits at window offset `0x08000000`, up to 128 MiB;
- the runtime writes it through 4 MiB fes-window grants while execution is held;
- Commit runs a CRC-32 pass on port 2;
- a 64 KiB dirty map backs a chunk-delta save at Stop.

The disk is fixed while the machine runs: insert, eject and save are all refused (`M/docs/pc386-machine-design.md:1072-1092`, `:1195-1233`). Host import is `POST /api/v1/core-media/hard-disk` (#1547, `FC/fogcast/hard_disk.go:42-51`).

**Port 2.** The arbiter's priority is plane, presenter, capture, EMS, then `AUX` (`pc386_port2.sv:53-58`). `AUX` is `pc386_disk` alone today.

**DMA and IRQ.** Controller 1 already has a data path for channels 1–3 (`pc386_at_core.sv:47`, `:252-263`), and the Sound Blaster uses channel 1. Channel 2's DRQ is an input that nothing drives. IRQ 6 and IRQ 15 are free (`pc386-machine-design.md:230-241`).

**I/O decoder.** The decoder splits only 1F0h into 16-bit device cycles (`pc386_io.sv:73`). The ATAPI data port at 170h needs the same treatment.

**CMOS.** `pc386_rtc.sv` writes these bytes at every Execution release:

| Byte | Value today | Meaning | Line |
| --- | --- | --- | --- |
| 10h | 00h | no floppy drives | `:146` |
| 14h | 04h | no floppy | `:148` |
| 2Dh | 00h | boot C: before A: | `:164` |
| 38h | 00h | no third boot device | `:169` |
| 3Dh | 02h | boot from the hard disk only | `:172` |

**Bochs BIOS** (`M/cores/fes-pc386/firmware/rombios/`):

- `BX_USE_ATADRV 1`, `BX_ELTORITO_BOOT 1`, four ATA interfaces (`rombios.h:110-114`).
- Channel 1 is 170h/370h on IRQ 15 (`rombios.c:2726-2729`). Its ATAPI driver and El Torito code are compiled in, with floppy emulation and no emulation (`cdemu_*`, `:3750-3770`).
- POST and INT 13h floppy paths read CMOS 10h drive types 1–5, which are 360K, 1.2M, 720K, 1.44M and 2.88M (`:7425-7460`, `:8004-8034`).
- **It never writes CCR (3F7h).** It touches only DOR, MSR and data (the `PORT_FD_*` uses; `rombios.h:176-178`). Bochs's FDC ignores the data rate, and this BIOS depends on that.
- **It reports no change line.** INT 13h AH=15h returns AH=1, "does not support change line" (`:8081`). AH=16h always returns 06h with CF set (`:8086-8098`).

The firmware README says the floppy POST and INT 13h paths stay, and that the unclaimed controller ports read FFh (`M/cores/fes-pc386/firmware/README.md:107-110`).

**Mixer.** `pc386_mixer.sv` sums mono speaker, OPL2 and DSP levels into one 16-bit sample (`:15-24`). There is no stereo path yet; #1441 (SB Pro) asks for one.

### Media ABI, runtime and host

**The `fes.computer` 1.1 ABI** (`MP/docs/computer-io.md`):

- Media units are addressed by unit index, 8 of them (`MP/packages/abi/fes_computer.yaml:212`).
- The rule is one unit-0 media interface per shell (`computer-io.md:64-65`, `:310-311`). The Spectrum's disk on unit 1 beside its tape is the only multi-unit precedent (`:313-352`).
- Snapshot chunks are "unit 0 only" (`:681`).
- No operation tells the core that media changed. A change is just the unit going loading, empty and back to ready.

**Window.** The window is 256 MiB at `0x30000000` (`docs/hps-ddr.md:56`). The pc386 layout (`pc386-machine-design.md:291-301`) ends exactly at the window's end with the ATA image. That leaves two holes:

- 40 MiB at `0x01800000`;
- 4 MiB less 20 KiB at `0x01005000`.

**Runtime.**

- `insert_media`, `eject_media`, `insert_library_media` and `save_media` exist (`RT/src/daemon/protocol.cpp:1405-1450`).
- Library media is hard-coded to unit 0 (`protocol.cpp:1446`, `media_data.cpp:136`, `hardware.cpp:1423`, `:1507`).
- There is one persistent binding (`hardware.hpp:198-204`).
- Stop saves the media, then the ATA disk (`hardware.cpp:343-355`).
- The runtime has no thread that serves the core while it runs; input is pushed per host request. Menu frames are the precedent for fes-window grants while the FPGA runs.

**FogCast.**

- Live media is `POST /api/v1/session/live-media` with no unit field (`FC/protocol/live_media.go:12-15`). It is routed by file extension, and every computer disk goes to unit 0 except the Spectrum's (`FC/fogcast/live_media.go:25-57`, `media_units.go:154-164`).
- Writable units save before eject or insert (`media_units.go:17-19`).
- The tenfoot picker is one tape/disk browser with an Eject row and no drive choice (`FC/ui/tenfoot/tape.go:26-30`, `:259-264`).
- A library entry holds one media item (`FC/catalog/CORE_MEDIA.md:7-37`). Ordinary core media stops at 32 MiB (`CORE_MEDIA.md:16`).
- `.cue` is parsed only as a list of companion files (`FC/catalog/mediaset.go`).

**Larger media.** `docs/core-media-evolution.md` sets the rules any larger-media contract must follow: versioned interfaces, explicit lengths, integrity checks, abort and Stop behaviour, and ownership (`:75-128`).

## Floppy

### Controller

`pc386_fdc` is a new MIT module, an 82077AA in AT mode, using only the subset that DOS, the BIOS, Windows 9x's HSFLOP and Linux's `floppy.c` use:

| Port | Read | Write |
| --- | --- | --- |
| 3F2h | DOR (82077 reads it back) | DOR: drive select, /RESET, DMA+IRQ gate, motors A and B |
| 3F4h | MSR | DSR: soft reset, data rate (accepted, see below) |
| 3F5h | FIFO / data | FIFO / data |
| 3F7h | DIR: bit 7 disk change of the selected drive, other bits floating (FFh pattern as the AT) | CCR: data rate (accepted) |

3F6h stays the ATA device-control register. The 82077's 3F0h and 3F1h (PS/2 status A and B) read FFh, as an AT's do.

**Commands.** These are the PC/XT set and the AT additions:

| Command | Code |
| --- | --- |
| Read Data | 06h/46h/66h/E6h, MT and MFM honoured |
| Write Data | 05h/45h/C5h |
| Read Deleted Data, Write Deleted Data | 0Ch, 09h. Read as data with the CM bit; write as data. Images carry no deleted marks |
| Read ID | 0Ah |
| Format Track | 0Dh. Fills the track's sectors in the image with the fill byte; the guest's ID bytes must match the image geometry, or it ends with an error |
| Recalibrate, Seek, Relative Seek | 07h, 0Fh, 8Fh/CFh |
| Sense Interrupt Status, Sense Drive Status | 08h, 04h. ST3 carries WP, T0, two-side and ready |
| Specify | 03h. SRT, HUT and HLT drive the step time; ND is honoured as DMA versus PIO |
| Version | 10h. Returns 90h, an 82077. Windows 95 and Linux use it to pick the enhanced controller |
| Configure, Lock, Dumpreg, Perpendicular | 13h, 94h/14h, 0Eh, 12h. Accepted, with fixed answers. Linux probes them |

Anything else is invalid (ST0 80h), as the 82077 answers.

**Drives.** Two drives: DOR's select bits 0–1 for drives 0 and 1, with motor bits 4 and 5. Drives 2 and 3 are absent. Each drive has:

- its own cylinder;
- a disk-change latch, set by eject and insert and cleared by a step pulse with a disk present, as on a real drive;
- motor spin-up time (500 ms on 3.5", 400 ms on 5.25");
- a type from configuration (**D4**).

**Data rate.** The controller accepts CCR and DSR writes but does not fail a read whose data rate does not match the medium. The locked BIOS never writes CCR, and Bochs's controller ignores the rate (see [pc386 today](#pc386-today)). This is a recorded deviation, as Bochs's is. The byte rate the model *paces* reads at follows the medium:

| Medium | Byte time |
| --- | --- |
| 250 kbit/s, 360K/720K | 32 µs |
| 500 kbit/s, 1.2M/1.44M/DMF | 16 µs |
| 1 Mbit/s, 2.88M | 8 µs |

**40-track media in an 80-track drive.** The locked BIOS never double-steps: it sends the INT 13h track straight to Seek and Read (`rombios.c:7733-7741`), and Bochs's controller maps the cylinder to the image's cylinder directly. `pc386_fdc` does the same: a 360K disk in a 1.2 MB drive reads image cylinder `c` at cylinder `c`, so the BIOS and DOS work unpatched and the Bochs replay agrees. A real 1.2 MB drive needs the host to double-step; a driver that does so itself (Linux's `stretch` formats) would read the wrong cylinders. This is a recorded deviation. If a title needs real double-stepping, the fix is a BIOS patch that double-steps on 40-track media plus a physical-track model in the drive, in one slice. A B: drive configured as 360K (type 1) avoids the question for 5.25" DD sets.

**DMA and IRQ.** DRQ goes to `dma_drq[2]` of controller 1 (`pc386_at_core.sv:47`), and TC ends the transfer. Guest memory cycles go through the CPU's DMA port, as the Sound Blaster's do. IRQ 6 is `device_irq[6]`. PIO (non-DMA) mode works too, for Linux's `floppy=nodma`.

**Sector path.**

- A 512-byte M10K sector buffer serves both directions.
- A read fetches the whole sector from the window in one 64-beat burst on port 2's aux path before the first byte's DRQ. At the slowest it has 32 µs per byte, so ordinary port latency is far inside that.
- A write collects the whole sector before one 64-beat store, so a torn sector never reaches the image.
- The `frozen` fence and the rule that "a sector accepted before warm reset completes" follow `pcxt_fdc_card_rw` (`:42-46`).

**Timing.** Step time follows Specify's SRT. Head settle is 15 ms. Times count system clocks at 61.44 MHz through `pc386_rate`, not instructions, so the governor's speed presets do not change floppy speed. An eventual "fast floppy" option is out of scope here.

### Media formats

FogCast accepts a raw `.img`, `.ima` or `.vfd` file and identifies the format by its exact size:

| Size (bytes) | Format | Cyl × heads × SPT | Rate | Drive types that read it |
| ---: | --- | --- | --- | --- |
| 163,840 | 160K | 40 × 1 × 8 | 250 | 5.25" (360K or 1.2M) |
| 184,320 | 180K | 40 × 1 × 9 | 250 | 5.25" |
| 327,680 | 320K | 40 × 2 × 8 | 250 | 5.25" |
| 368,640 | 360K | 40 × 2 × 9 | 250 | 5.25" |
| 737,280 | 720K | 80 × 2 × 9 | 250 | 3.5" (any) |
| 1,228,800 | 1.2M | 80 × 2 × 15 | 500 | 5.25" 1.2M |
| 1,474,560 | 1.44M | 80 × 2 × 18 | 500 | 3.5" 1.44M or 2.88M |
| 1,720,320 | DMF 1.68M | 80 × 2 × 21 | 500 | 3.5" 1.44M or 2.88M. Windows 95 and Office floppies |
| 2,949,120 | 2.88M | 80 × 2 × 36 | 1000 | 3.5" 2.88M |

Rules:

- The host rejects other sizes, and media the selected drive cannot read, with a message that names the drive it needs.
- Commit carries the geometry, so the core never infers it.
- An image of a 1.44 MB disk with a short tail, such as `freesize` padding, is refused, not padded.

### Lifecycle, swap and the change line

`fes.media.pc-floppy` 1.0 lives on units 1 (A:) and 2 (B:). It uses the same unit-indexed operations as the ATA disk ([ABI](#abi-changes)), with different rules for when the machine is running:

| Operation | When | Effect |
| --- | --- | --- |
| Prepare | Any time | The drive becomes empty: door open, not ready, change latch set. A sector store in flight finishes first. The unit is loading |
| (runtime writes the image) | Unit loading | Through fes-window grants into the drive's region. The core never reads a loading region, so the CPU can keep running |
| Commit (geometry, write-protect, CRC-32) | Unit loading | The CRC pass on port 2 checks the bytes (about 2 ms for 1.44 MB). The disk is then present and the change latch stays set until the guest steps |
| Freeze, Resume, Saved | Any time | As for the ATA disk: fence new sector stores, drain, ACK. Saved clears the dirty flag |
| Eject | Any time, after Freeze ACK | The drive becomes empty and its change latch is set |

The rule for writes: **the runtime saves a dirty disk before it ejects it, and before Stop.** A swap is Freeze, then save if dirty, then Eject, Prepare, write, Commit. The runtime does this as one `swap` operation, so the UI cannot leave a dirty disk half-swapped.

**How the guest sees a swap:**

- DIR bit 7 is set from eject until the next step with a disk present.
- DOS, through the patched BIOS (F2, below), then rereads the FAT and the boot sector.
- Windows 9x and Linux read DIR directly.
- A game that asks for disk 2 either polls DOS file opens, which see the new volume, or asks the player to press a key. Both work.

**Boot gate.** When the launch boots from A: (CMOS 3Dh), the launch commits A: before releasing execution, the same order the ATA disk already uses (`pc386-machine-design.md:1110-1121`). pc386 needs no `pcxt_boot_media_gate` equivalent: its launch already holds execution until the media it boots from is ready.

### Persistence

A floppy is small, so it is saved whole, as the PC/XT floppy is:

- **Layout:** `fes.pc386-floppy.image` 1.0. The `fes-media-data-v1` envelope holds the whole current image, published with the existing tmp-fsync-rename rule (`MP/docs/media-data.md:44-50`).
- **Key:** the existing `fes-media-data-v1` namespace of core, game, unit and base image SHA-256 (`MP/docs/media-data.md:9-12`, `:28`), with the unit field fixed at **1** for every pc386 floppy record, whichever drive holds the disk. The drive (unit 1 or 2 on the wire) is never part of the key, so the base SHA alone tells disks apart. Disk 2 of a set keeps its saved game whether the player puts it in A: or B:, and whichever disk was in a drive at Stop. The runtime maps a floppy unit to namespace unit 1 when it binds the record; `media-data.md` records the rule beside the layout.
- **One drive per writable image.** The runtime refuses to insert a writable disk whose namespace is already bound to the other drive; FogCast says the disk is already in A: (or B:). The same base image may sit in both drives only write-protected, where no record is written. A disk keeps one binding, so each save has one writer.
- **Dirty state:** one flag per unit, set when a sector store lands. A Format Track also sets it.
- **Read-only inserts:** the host can insert any disk write-protected (ST3 WP; writes end with ST1 NW). The library default is writable for disks of a game's own set, and write-protected for install media from a shared OS folder (**D9**).
- **Reset disk:** FogCast's existing "reset disk" (discard the record) applies per image.

### BIOS changes

The locked BIOS needs one patch for the floppy. It goes in `firmware/README.md`'s patch list:

- **INT 13h AH=15h** returns AH=02h, "change line supported", for drive types 2–5, as an AT BIOS does. Type 1 (360K) keeps AH=01h.
- **INT 13h AH=16h** reads DIR bit 7 of the drive. If set it returns AH=06h with CF set, otherwise AH=00h with CF clear. On a change it also clears the BDA media state, so the next access senses the medium again.

Without this patch DOS treats the drives as XT-style. It assumes a change after 2 seconds idle and rereads, so swaps still work, only slower and with a window for error. CMOS 10h, 14h bits 0 and 6–7, 2Dh, 38h and 3Dh come from configuration ([machine configuration](#machine-configuration)).

## CD-ROM

### CD-ROM interface

**Recommendation: an ATAPI CD-ROM, master on the secondary IDE channel.** The trade-off is not close:

| | ATAPI, secondary master | Mitsumi / Panasonic (SB) / Sony proprietary |
| --- | --- | --- |
| BIOS | The locked Bochs BIOS finds it and boots El Torito; no firmware work | No BIOS support, no CD boot |
| DOS | Open drivers in FreeDOS 1.4: UDVD2 (`drivers/udvd2`, "Free w/sources"), the SysLinux `ELTORITO.SYS` (GPL v2) for a booted CD, and Jemm's XCDROM32 | Vendor drivers only (`MTMCDAE.SYS`, `SBPCD.SYS`); none open |
| Windows 9x/NT | Built in (ESDI_506 + CDFS) | 95 shipped drivers for some; NT 4 and later dropped most |
| Linux | `ide-cd`/`libata` in every kernel | `mcd`/`sbpcd` and others removed in 2.6 |
| OS/2 Warp | IBM's ATAPI driver | Some, per vendor |
| Games | Call MSCDEX; the interface is invisible | Same |
| RTL | A task file next to the ATA disk's; reuses the 16-bit data-port split | A different register set and IRQ per card |

**Why the secondary channel and not the primary slave.** `pc386_ata` models device 1 as absent with shared registers (`pc386_ata.sv:44-50`). Making it a packet device would put a second state machine behind one task file. The BIOS, Windows and Linux all prefer the CD on its own channel. IRQ 15 is free.

The only reason to reopen this would be a target OS from before ATAPI drivers existed. The sibling early-Windows and early-Linux reports should confirm that none is in scope: Windows 3.1 needs a DOS ATAPI driver, which exists. That confirmation is **D1**.

### Where the command set runs

**Recommendation: the core is a thin ATAPI transport, and the runtime runs the MMC command set (D2).**

**Why.** A CD image is 7–850 MB, and the window has 40 MiB free. The core can never hold a disc, so the runtime must take part in every read whatever the RTL does. Once the runtime is there, the RTL options are:

- **MMC in RTL.** About 25 packet commands: TOC formats, sense state, sub-channel Q, mode pages, raw versus cooked sector layouts, and audio track bounds. All of that would sit in RTL on top of the demand fetch. That is about 1,500–2,000 ALMs more, at 91%+ fill, for no behaviour the HPS cannot give.
- **MMC on the HPS.** It is plain C. A cue sheet's track table, pregaps and MODE1/2048 versus MODE1/2352 versus MODE2 sectors are easy there. The same code serves the simulation.

**Core: `pc386_atapi`.**

- **Registers:** 170h–177h, 376h, IRQ 15. The decoder splits 170h like 1F0h.
- **ATA commands in RTL:**
  - IDENTIFY PACKET DEVICE (A1h), from a 512-byte page the runtime writes into the control page at launch;
  - PACKET (A0h);
  - DEVICE RESET (08h);
  - EXECUTE DEVICE DIAGNOSTIC (90h);
  - SET FEATURES (EFh), accepted;
  - CHECK POWER MODE and the idle group;
  - NOP.
  
  IDENTIFY DEVICE (ECh) aborts and leaves the packet signature (14h/EBh in the cylinder registers), so the BIOS's ATA probe classifies the drive as ATAPI (`rombios.c:2793`).
- **PACKET:**
  1. The core takes the 12-byte packet in one DRQ.
  2. It posts a request record `{sequence, packet, byte-count limit}` to the control page on port 2 and holds BSY.
  3. It polls the response header (one 64-bit read every 4 µs while busy; nothing otherwise) until the runtime has written `{sequence, total length}` for this request.
  4. It streams the response data to the guest in DRQ blocks no larger than the byte-count limit, raising IRQ 15 per block. It reads 64 bits at a time from the data ring into a two-block M10K buffer, as `pc386_ata`'s prefetch does. The ring handshake is below.
  5. Once it has consumed `total length` bytes and the runtime's final record (status and sense) carries the sequence, it ends the command with them.

  **Ring handshake.** The control page holds two byte counters per command, both starting at 0 when the runtime writes the response header:

  - `produced`, written only by the runtime: bytes of the response now in the ring. The ring is circular; byte `n` of the response sits at ring offset `n mod ring size`.
  - `consumed`, written only by the core: bytes it has read out of the ring into its buffer.

  The core starts a DRQ block only when `produced − consumed` covers the whole block (or the rest of the response), so a block never stalls mid-transfer. The runtime writes ring bytes only below `consumed + ring size`, so it never overwrites bytes the core has not read. Each `produced` update follows its data's cache clean and a barrier. A response of any length, up to READ(12)'s, streams through the 4 MiB ring this way, and the first block can go to the guest as soon as it is produced. Each counter is 32 bits, written as one aligned word; READ(12)'s largest response at 2,352 bytes per sector exceeds that, so the runtime refuses a transfer over 4 GiB − 1 with ILLEGAL REQUEST (05h/24h). For a guest write the roles swap: the core produces and the runtime consumes, and the runtime posts its final record after it has read every byte.
- **Writes from the guest** (MODE SELECT parameter lists) go the other way: the core stores the bytes into the ring, then posts.

**Runtime: a CD service thread.**

- It is new, and runs while a CD unit is present.
- It polls the control page through an uncached mapping and runs each packet against the disc's track table and files.
- It writes the response header, then the data in segments under the ring handshake, then the final record, with a cache clean and a barrier before each counter or record update.
- **Stop and core replacement.** The thread belongs to one launch generation. Stop runs, in this order, before the runtime releases the window or programs another core:
  1. Set the thread's cancel flag. The thread checks it between segments and before each window write, and never starts a new write after seeing it.
  2. Join with a deadline (100 ms; a segment write takes far less).
  3. Revoke its grants and unmap the control page.
  
  If the join misses its deadline, Stop fails as a fault cleanup does today. The window stays owned and no core is programmed; it is never handed over with a worker still mapped. A request the core posted but the thread never answered dies with the core, whose reset clears the drive. C2's kit smoke covers Stop during a long READ and during CD-DA playback, then relaunch, as `docs/core-media-evolution.md:92-94` requires.

**Commands the runtime serves (MMC-1 / SFF-8020i subset):**

- TEST UNIT READY, REQUEST SENSE, INQUIRY;
- READ CAPACITY, READ(10), READ(12), READ CD, READ CD MSF, READ HEADER;
- READ TOC/PMA/ATIP formats 0, 1 and 2, READ SUB-CHANNEL formats 1–3;
- PLAY AUDIO(10), PLAY AUDIO(12), PLAY AUDIO MSF, PAUSE/RESUME, STOP PLAY/SCAN, SEEK;
- MODE SENSE(10) and MODE SELECT(10): pages 01h, 0Dh, 0Eh (audio volume) and 2Ah (capabilities);
- START STOP UNIT (eject), PREVENT ALLOW MEDIUM REMOVAL;
- GET EVENT STATUS NOTIFICATION (media class), MECHANISM STATUS, GET CONFIGURATION (minimal).

Anything else gets ILLEGAL REQUEST (05h/20h). The interpreter is one C library in `RT/`, and both the runtime and `M/cores/fes-pc386/sim/machine_tb.cpp` link it.

**Latency.** A real 2× drive takes 100–300 ms to seek and 1–5 ms per command. The service thread's wake-up latency is **not measured yet**. Slice C2 measures it on Kit B and sets the poll interval to keep each command under 2 ms. If polling costs too much ARM time, the fallback is an FPGA-to-HPS interrupt through a fes-window event, a kernel change, recorded as **D3**.

**Speed.** The drive reports a 4× drive (600 KB/s). The core paces its own streaming: a byte-rate limiter in `pc386_atapi` lets a DRQ block start only when the bytes sent since the response header stay within 600 KB/s of elapsed time (a counter against a `pc386_rate` tick). The pace holds however fast the runtime fills the ring, including reads from cache. The runtime adds seek time before it writes the response header: a fixed access time per command and a distance-scaled seek, from a constant table. Games that time the drive then see a period drive. Port 2 load is under 1 MB/s next to its measured 700 MB/s.

### Media change, eject and boot

- **No disc:** TEST UNIT READY gives NOT READY (02h/3Ah).
- **Disc inserted:** the first command after it gives UNIT ATTENTION (06h/28h), then the disc is ready. MSCDEX, Windows and Linux all handle a swap this way.
- **Guest eject.** START STOP UNIT with LoEj ejects the disc in the core's state, unless PREVENT ALLOW MEDIUM REMOVAL locks it. The unit's state tells FogCast, so tenfoot shows the tray empty.
- **Host eject.** An eject from the host while the guest has the medium locked is refused, with the guest's lock named in the error.
- **El Torito.** A CD launch boots CD, then hard disk, then floppy: CMOS 3Dh = 23h and 38h = 10h ([machine configuration](#machine-configuration)). The BIOS boots no-emulation images (isolinux) and 1.44 MB floppy-emulation images (the Windows 98 SE OEM CD), with no patch.

### Images

FogCast gets a `cd` role:

- **`.iso`**: one MODE1/2048 data track.
- **`.cue` plus its `.bin` files**:
  - multiple files allowed;
  - track types `MODE1/2048`, `MODE1/2352`, `MODE2/2352` and `AUDIO`;
  - `PREGAP` and `INDEX 00/01` honoured;
  - FogCast parses the sheet on import into a canonical track table (number, type, start LBA, file, byte offset, sector size) and stores it beside the files.
- **Size:** up to 900 MB in all, the 80-minute ceiling plus raw overhead. This raises the 32 MiB ordinary limit for this role only, under `core-media-evolution.md`'s storage rules.
- **Rejected:** a `.cue` naming a missing file, a `CATALOG`/`CDTEXT`-only quirk the parser does not know, or an audio track not a whole number of 2,352-byte sectors. Each fails with the line number.
- **Staging:** the kit holds the files by SHA-256, as for the hard disk, so relaunching does not move them again. Where they live on the kit is **D6**.

The core never sees the track table. The runtime gives it only data and play state.

### CD-DA

**Playback path:**

1. PLAY AUDIO validates the range against audio tracks; a data track gives 05h/64h.
2. The runtime fills the audio ring: 445 slots of one 2,352-byte sector each (1,046,640 bytes, inside the 1 MiB region; about 5.9 s). Slot `k` holds play sector `k mod 445`, counted from the start of the range.
3. It writes `{start LBA, end LBA}` into the control page, with two monotonic 32-bit sector counters, both 0:
   - `audio_produced`, written only by the runtime after the slot's data, its cache clean and a barrier;
   - `audio_consumed`, written only by the core as it finishes a sector.
   
   The runtime writes a slot only while `audio_produced < audio_consumed + 445`, so it never overwrites a sector the core has not played. It sets *playing* once `audio_produced` reaches 75 sectors (1 s), or the whole range if shorter.
4. `pc386_cdda` reads sector `audio_consumed` from the ring into a two-sector M10K buffer, but only while `audio_consumed < audio_produced`. A sector it has not been given is never read, so a wrapped ring cannot replay stale audio.
5. It plays 16-bit stereo frames at 44,100 Hz from a `pc386_rate` divider of the 61.44 MHz clock (an exact ratio: 61,440,000 / 44,100 = 204,800 / 147).
6. It publishes `audio_consumed` and its frame within the sector in the control page.
7. The runtime refills behind it and answers READ SUB-CHANNEL from that position: absolute and relative MSF, track, index. Play ends when `audio_consumed` reaches the range's length.

PAUSE, RESUME and STOP set a control bit that the player obeys at the next frame.

**Changing the range.** Each range has a 16-bit play generation. The core writes `audio_consumed` and its frame position as one aligned 64-bit word tagged with the generation it is playing, and it publishes an idle word `{generation, idle}`. A PLAY AUDIO that replaces an active range, or a STOP, runs in this order:

1. The runtime clears *playing*.
2. It waits until the core's idle word shows the current generation idle. The core sets it only after its in-flight ring read has returned and its last position write has landed. The wait is bounded by one sector read, and the runtime treats a timeout as a service fault.
3. The runtime resets the counters, writes the new range, increments the generation, refills to the start watermark, and sets *playing*.

The core reads the generation with *playing* and tags everything it writes with it. The runtime ignores any position word whose generation is not current, so a stale write from the old range can never move the new range's counters.

**Underrun.** When the next frame's sector is not yet produced (`audio_consumed == audio_produced` before the range ends), the player outputs silence for that frame, counts it, and resumes at the same sector once it is produced, so position never skips ahead of the data. The runtime is about 6 s ahead, so an underrun means the service thread stalled. The status ring reports the count.

**Mixer.** Page 0Eh's port 0 and port 1 volumes scale left and right (8-bit by 16-bit: two DSP blocks, or shifts and adds). The mixer integrates any held level over each 48 kHz period (`pc386_mixer.sv:1-6`), so a 44.1 kHz source needs no other resampler. The mixer is mono today, so v1 sums left and right at half scale each and the CD sits at the same gain as the DSP. Stereo waits for the SB Pro work in #1441, which needs a stereo mixer anyway. Whether CD-DA should wait for that is **D8**.

**Talkie streaming** (Day of the Tentacle, Sam & Max) is data reads, not CD-DA. It is covered by READ(10) pacing.

### DOS drivers

The games disk builder (`scripts/build_pc386_freedos_image.py --profile games`) gets an optional CD stage:

- **UDVD2** from FreeDOS 1.4 `drivers/udvd2` as the ATAPI driver. Its APPINFO says "Free w/sources". Its source goes on the image beside it, as the builder's licence table requires (`pc386-machine-design.md:1134-1194`).
- **SHSUCDX 3.09** from FreeDOS 1.4 `base/shsucdx` as the MSCDEX replacement. Its APPINFO (`SHSUCDX.LSM`) has **no** copying-policy line. The upstream licence must be checked and recorded before the builder ships it. If it cannot be, the fallback is Jemm's XCDROM32 plus a different extender.
- **`FDCONFIG.SYS`** gets an entry that loads UDVD2. **`AUTOEXEC.BAT`** runs `SHSUCDX /D:UDVD1` on that entry only. The default entry stays as small as it is today, because CD drivers cost conventional memory that Ultima VII and others need.
- **No Microsoft `MSCDEX.EXE`** in anything FES builds or ships. A player's own DOS install may carry it.

## Media delivery

### Window layout

These go in the 40 MiB hole. No existing region moves.

| Window offset | Size | Contents | Port |
| --- | --- | --- | --- |
| `0x01800000` | 4 MiB | Floppy unit 1 (A:), up to 2.88 MB used | 2 (aux) |
| `0x01C00000` | 4 MiB | Floppy unit 2 (B:) | 2 (aux) |
| `0x02000000` | 64 KiB | CD control page: IDENTIFY PACKET page, request record, completion record, play control and position, counters | 2 (aux) |
| `0x02010000` | 4 MiB − 64 KiB | CD data ring | 2 (aux) |
| `0x02400000` | 1 MiB | CD-DA ring | 2 (aux) |
| `0x02500000` | 27 MiB | Unused | — |

### Port 2

`AUX` becomes a small round-robin arbiter over ATA, FDC, ATAPI and CD-DA, behind the display clients as now.

| Client | Need |
| --- | --- |
| CD-DA | 176.4 KB/s; a sector every 13.3 ms against about 6 s of ring |
| FDC | Under 125 KB/s; a sector per 4 ms at 2.88 MB |
| ATAPI | Under 1 MB/s at 4× pacing |
| ATA | Its sector must arrive faster than the guest reads one (92 µs; `pc386-machine-design.md:1048-1051`) |

None of these comes near the port. Round-robin keeps ATA's prefetch bound as long as each of the others issues one burst at a time. Slice C3's bench checks the bound with all four active.

### Bandwidth and latency

- **Floppy insert:** at most 2.88 MB through grants. On the kit this is milliseconds, measured in F3. It happens while the CPU runs, because the drive is empty while it loads.
- **CD:** each command costs the runtime's wake-up time plus a file read, usually from page cache. A cold read costs one SD or USB read, which is still inside a real drive's seek.
- **CD-DA:** a refill is 176 KB/s of sequential reads.

Slice C2 records the measured command latency distribution and the ARM load of the service thread.

### ABI changes

These are owned as `core-media-evolution.md:113-128` says.

**mister-packages** (`MP/packages/abi/fes_computer.yaml`, `MP/docs/computer-io.md`):

- **More than one unit-0 interface.** Lift "at most one unit-0 media interface" to "one interface per unit". pc386 declares `ata-disk` on unit 0, `pc-floppy` on units 1–2 and `atapi-cd` on unit 3.
- **Unit-indexed Disk operations.** DiskInfo (`unit*8+field`) and DiskControl (`unit`) already carry a unit and today refuse any but 0 (`MP/docs/computer-io.md:117-118`, `:444-453`); they accept units 1–3. DiskCommit becomes `unit*8+word`, which fits the 8-bit index for all eight units. **DiskDirty stays unit 0 only:** its index is already the ATA map's word 0–127 (`:121`, `:493`), so a unit field would not fit, and neither new interface has a chunk map (a floppy's dirty state is one DiskInfo flag; a CD has none). Per-interface semantics:
  - `ata-disk` keeps its "held execution only" rules;
  - `pc-floppy` allows them while running, as above;
  - `atapi-cd` has Prepare, Commit (no CRC: the track table is the runtime's) and Eject, and no dirty map.
  
  This is a minor ABI bump, 1.1 to 1.2. Old shells declare no new interfaces and keep working on the new runtime. Old runtimes do **not** accept the new pc386: they admit only minors 0 and 1, and they reject unknown required declarations (`RT/docs/computer-io.md:29-31`, `:47-48`). These media interfaces are required, so a 1.2 pc386 package ships only with a runtime that implements 1.2.
- **New layouts.** `fes.pc386-floppy.image` 1.0 in `media-data.md`. The CD control-page layout is a published constant table, so the core, runtime and `machine_tb` share one definition. It is published in C1, with its first consumers (the RTL and `machine_tb`); C2 adds the runtime as a consumer of the same table.

**libmister-runtime:**

- per-unit library bindings, replacing the single `media_data_file_`;
- unit checks lifted at `protocol.cpp:1446`, `media_data.cpp:136`, `hardware.cpp:1423` and `:1507`;
- `FlushSave` loops over units;
- a `swap_media` operation (save, eject, insert as one);
- the CD service thread and the MMC library;
- stale lines fixed: `RT/docs/computer-io.md:63-64` and `:175-176` still call the PC/XT floppy read-only.

**FogCast:**

- the `floppy` and `cd` roles, with size and format checks;
- **media sets:** a library entry holds an ordered list of media, each with a role and a default drive (disk 1 → A:, disc 1 → CD). This is the entry-schema extension `core-media-evolution.md:111-113` anticipates;
- the live-media API takes an explicit unit, or a drive name for pc386: `A`, `B`, `CD`;
- the tenfoot picker gets a drive row, "A: Disk 2 of 5", and one-press next and previous within the set;
- the launch's boot device (C, A or CD) is a per-entry setting written into the `fes.pc386.machine` record.

### Machine configuration

The drive types and the boot device are per-entry settings. `fes.pc386.machine` 1.0 has no room for them: words 0–6 are settings, word 7 is reserved and must be 0, and words 8–71 are CMOS (`MP/docs/computer-io.md:554-570`). F3 therefore defines **`fes.pc386.machine` 1.1**, with these words added after the CMOS words:

| Word | Content | Values | Default |
| --- | --- | --- | --- |
| 72 | Drive A: type | 0 none, CMOS 10h codes 1–5 | 4 (1.44M) |
| 73 | Drive B: type | as word 72 | 2 (1.2M) |
| 74 | Boot device | 0 hard disk, 1 A:, 2 CD | 0 |

- Word 7 stays reserved; #1555 has a claim on it.
- A 1.1 decoder reads a 1.0 record as 1.1 with these defaults, so existing records keep working. A 1.0 decoder rejects a 1.1 record by its layout version, as `media-data.md` requires.
- Commit rejects values outside each set.
- The golden vectors gain 1.1 cases and keep the 1.0 ones.
- Until F3, the F2 simulation sets these inputs through `machine_tb`'s configuration, not through a record.

The machine turns them into CMOS bytes (`pc386_rtc.sv:146-173`) instead of constants:

| Byte | From |
| --- | --- |
| 10h | Drive types A (high nibble) and B (low nibble) |
| 14h | Today's 04h (bits 5–4 VGA, bit 2 the PS/2 port) ORed with the floppy fields: bit 0 set when a drive exists, bits 7–6 the count minus 1. Two drives give 45h |
| 2Dh bit 5 | Set when booting A: first |
| 3Dh and 38h | Boot order, three devices. The BIOS takes the first device from 3Dh's low nibble, the second from its high nibble and the third from 38h's high nibble (`rombios.c:8549-8550`); codes 1 floppy, 2 hard disk, 3 CD. 38h bit 0 (skip the floppy boot-signature check) stays 0. Hard-disk launch (hard disk, floppy, CD): 3Dh = 12h, 38h = 30h. Floppy launch (floppy, hard disk, CD): 3Dh = 21h, 38h = 30h. CD launch (CD, hard disk, floppy): 3Dh = 23h, 38h = 10h. Every nibble is set, so a failed first device falls back instead of stopping at "No bootable device" |

The checksum over 10h–2Dh covers them as it does today.

## Area and budget

Measured base: the latest seal (`docs/validation/2026-11-11-i386-call-push-first/summary.md:21`) is 36,118 ALMs and 268 of 553 M10K on the 41,910-ALM 5CSEBA6. The Phase 5 plan already projects 91.3–92.8% (`docs/validation/2026-11-08-i386-area-audit/summary.md:40-45`). Nothing past about 92% has been routed.

| Block | ALMs | M10K | DSP | Basis |
| --- | ---: | ---: | ---: | --- |
| `pc386_fdc`: registers, FSM, two drives, timers, sector buffer, window port | 1,000 | 1 | 0 | PC/XT FDC's FSM plus 82077 registers and a second drive |
| Floppy units' state, CRC pass and fence (shared CRC engine with `pc386_disk`) | 200 | 0 | 0 | `pc386_disk` CRC is about 150 |
| `pc386_atapi`: task file, packet, control-page client, PIO streamer | 650 | 2 | 0 | The ATA device's 900-ALM row without CHS, the commands or the dirty map |
| `pc386_cdda`: ring reader, buffer, 44.1 kHz rate, volume | 300 | 4 | 2 | Two 2,352-byte sectors |
| AUX arbiter, 170h split, IRQ and DMA wiring, CMOS rows | 150 | 0 | 0 | |
| **Total** | **about 2,300** | **7** | **2** | |
| After: about 38,400 ALMs (91.6%), 275 M10K, 40 DSP | | | | |

That is inside the ceiling only if Phase 5 lands near its low case. Options, in order of cost (**D7**):

1. Land floppy first. It is about 1,200 ALMs and serves installs and swaps. CD waits for Phase 5's measured area.
2. One floppy drive: about −250.
3. CD-DA in a later slice: −300. CD data and installs work without it.
4. Take the area audit's remaining MLAB wins first: the return stack about −250, the OPL2 memories about −150 (`rtl-frontend-lanes.md`, Summary).

Each slice's record states the measured ALMs against main's seal. A slice that crosses 92% must also show the seal meets timing. Never relax the seal or timing gates to fit.

## Verification

### Device references

Bochs's own device models are the oracle, as `vga_reference.cpp` wraps `vgacore.cc`. The pinned Bochs oracle tree (`M/build/sim/fes-i386-oracle/bochs-*/iodev/`) has `floppy.cc`, `harddrv.cc` and `cdrom.cc` with its image backend. The plan:

- **`fdc_reference`** wraps `floppy.cc` behind a port-and-DMA trace interface. A replay bench (`fdc_replay_tb`) drives the same port writes, DMA acknowledgements and time into both models and compares:
  - port reads;
  - IRQ and DRQ edges, within a tolerance for modelled time;
  - image bytes after writes.
  
  `fdc_divergences.toml` records every difference with its deciding case. The data-rate check starts as one: both ignore it.
- **`atapi_reference`** wraps `harddrv.cc`'s ATAPI half for the task file and packet protocol. Data and sense come from the MMC library on both sides, so the replay checks transport, not the interpreter. The interpreter has its own C unit tests: TOC, sub-channel and mode pages against cue sheets, including multi-file, pregap and MODE2.
- **`cdda_tb`** runs the player alone against a ring model and checks frame rate, position reporting, pause and underrun.

### `machine_tb` and suites

`machine_tb` gains three things:

- floppy units backed by host files (copies, never the originals);
- a CD unit backed by the MMC library with a configurable completion latency, deterministic in simulation;
- a `--swap UNIT:FILE@MARKER` option that swaps at a debug-port marker.

New `sim_fes_pc386.py` suites:

| Suite | What it proves |
| --- | --- |
| `floppy` | FLOPTEST.COM (new, MIT, in `sim/`): INT 13h read, write, verify and format on A: at 1.44M and B: at 1.2M and 360K; a swap mid-run with AH=16h seeing it; DMA across a 64 KiB boundary error; DOS `DISKCOMP` of a written disk |
| `floppy-boot` | Boot FreeDOS from a 1.44 MB FreeDOS floppy built by the repository's FreeDOS tooling (not the LAN set) to the prompt |
| `cdrom` | CDTEST.COM: UDVD2 + SHSUCDX on the games image, `DIR D:` of a generated test ISO (built by `genisoimage`/`xorriso` from repository files), file read CRCs, READ TOC and play/sub-channel through MSCDEX's IOCTL on a generated cue/bin with two short synthetic audio tracks |
| `cd-boot` | El Torito: a generated no-emulation ISO with a tiny boot program that prints to the debug port, and a floppy-emulation ISO around the FreeDOS boot floppy |

Each runs on the oracle CPU first, then with `--cpu rtl`. Every suite above ends with `--cpu rtl`.

### Lockstep

The devices are not CPU state, but they drive the CPU's DMA, I/O and IRQ inputs. The `rtl-stops` and lockstep runs on `floppy` and `cdrom` check that the RTL CPU and the oracle agree across:

- DMA-terminated INT 13h reads;
- IRQ 6 and IRQ 15 arrival;
- `REP INSW` from 170h, the decoder split.

### Kit plan

These run under a kit lease and the [shared kit rules](../../AGENTS.md#shared-kits-and-powerboat-read-first). Every run restores the baseline bundle and image afterwards.

1. **F3 smoke.**
   - Insert the FreeDOS boot floppy in A:, boot it, and read B:.
   - Write files to A: and swap to B: and back, with DIR showing the change.
   - Stop, relaunch, and check the written file is there.
   - Check the record names the base SHA.
2. **C2 smoke.**
   - UDVD2 + SHSUCDX, then `DIR D:` on the generated ISO.
   - Measure the command latency distribution and the service thread's CPU share.
   - Swap discs and see UNIT ATTENTION.
3. **C3 audio.** Play the generated audio tracks, then a game's CD-DA (Screamer). Capture HDMI audio and check pitch against 44.1 kHz and that the track changes.
4. **Acceptance** ([titles](#acceptance-candidates)). Each title runs with its pass criterion recorded, under `docs/validation/`.

### Acceptance candidates

All of these are user-supplied, read from Deano's eXoDOS 6.04 zips on Powerboat or from the OS install media on his LAN share. Records cite paths and SHA-256 only. **Nothing commercial is committed, put in fixtures, or shipped in images.**

**Floppy:**

| Title | Media | Tests |
| --- | --- | --- |
| MS-DOS 6.22 setup (LAN share) | Disk1–3 + Suppdisk, 1.44M | Boot from A:, an install that swaps disks, writes to C: |
| Windows 98 SE boot floppy (LAN share) | 1.44M | Boot, CD-ROM driver from floppy (OAKCDROM.SYS is Microsoft's; it is only run, never shipped) |
| Strike Commander (census `strike`) | Disk1–5+, 1.44M | Install from five disks with prompts; the census title installed the real way |
| Wing Commander II (LAN share, 5.25") | Seven 1.2 MB disks | 1.2M drive on B:, a seven-disk install |
| SimEarth 1.0 (LAN share) | 720K | 720K media in a 1.44M drive |
| Commander Keen 6 | Three .IMA disks | Small multi-disk install |
| Populous | 3.5" and 5.25" .IMA | Both drives at once |
| Karateka, F-15 Strike Eagle, MS Flight Simulator 1.05 | Booter images | INT 13h boot without DOS |

**CD:**

| Title | Image | Tests |
| --- | --- | --- |
| Descent, Hocus Pocus, One Must Fall 2097 (census) | .iso, 7–17 MB | First data CDs, small |
| Screamer (census) | cue/bin, 7 audio tracks | CD-DA music; FPU game |
| U.S. Navy Fighters Gold (census `usnf`) | cue/bin, data | Its CD check and loading from the disc |
| Star Wars X-Wing Collector's CD (census) | cue/bin plus floppies | CD and floppy in one title |
| Strike Commander CD-ROM Edition | cue/img | The CD build of the census title |
| Day of the Tentacle | cue/bin | Talkie streaming |
| Quake | cue/bin, 10 audio tracks | CD-DA under DPMI; waits for x87 for play, but the CD path can be checked from its menu |
| Tomb Raider Gold | cue/bin, 59 audio tracks | Many short tracks, sub-channel seeking |
| Wing Commander III | Four .iso | Four-disc swap |
| 7th Guest | Two cue/img | Disc swap and video streaming |
| Windows 98 SE OEM bootable CD (LAN share) | .iso | El Torito floppy-emulation boot, then setup. Needs the CPU features the Windows report lists |

The census manifest (`M/cores/fes-pc386/sim/game-drives.toml`) copies installed folders today, and no census title mounts a CD or floppy. A media-set field in that manifest lets the census builder attach a title's disc instead of copying it.

Before an OS install is called an acceptance target, it must be checked against the CPU's state. The sibling early-Windows and early-Linux reports list the CPU features each needs, such as paging, V86 and the x87.

## Implementation plan

Each slice is one PR with its own `Closes` issue under the tracker, [#1694](https://github.com/DeanoC/fes/issues/1694). Floppy and CD are independent after F0.

### F0. Design doc (this PR)

### F1. `pc386_fdc` and its reference bench (RTL, sim only)

- **Builds:** the controller, two drives, the sector path to a window model, DMA channel 2, IRQ 6, and the 3F7h DIR/CCR; `fdc_reference`, `fdc_replay_tb` and `fdc_divergences.toml`.
- **Out:** the CMOS stays "no floppy" and the endpoint is not connected yet.
- **Gates:**
  - the replay suite with no unlisted divergences;
  - `devices` and `boot` unchanged;
  - a full Quartus seal with ALMs and M10K against main's;
  - timing met.
- **Estimate:** +1,000 ALMs, +1 M10K.

### F2. Floppy in the machine: CMOS, BIOS patch, `machine_tb` units (RTL + firmware, sim)

- **Builds:** CMOS rows from configuration inputs, driven by `machine_tb` until F3 adds the record words; the INT 13h AH=15h/16h patch; file-backed units and `--swap` in `machine_tb`; the `floppy` and `floppy-boot` suites; FLOPTEST.COM.
- **Gates:**
  - both suites on the oracle and `--cpu rtl`;
  - the lockstep check;
  - firmware lock updated;
  - seal.
- **Estimate:** +150 ALMs.

### F3. Floppy media ABI, runtime and endpoint (mister-packages, runtime, RTL endpoint, FogCast)

- **Builds:**
  - unit-indexed Disk operations and `fes.media.pc-floppy` 1.0 (ABI 1.2);
  - `fes.pc386.machine` 1.1 (drive types, boot device) with its RTL, runtime and FogCast consumers and the 1.0-to-1.1 read;
  - the endpoint's units 1–2 with the CRC pass;
  - per-unit runtime bindings and `swap_media`;
  - `fes.pc386-floppy.image`;
  - FogCast's floppy role, the drive-aware live-media API and the tenfoot drive row.
- **Gates:**
  - ABI generation and the runtime and FogCast tests;
  - the endpoint's golden exchanges in the `storage` suite;
  - Kit smoke 1.
- **Estimate:** +200 ALMs.
- **One PR:** the ABI change and every consumer that implements it (endpoint RTL, runtime, FogCast) land together, since a shared-definition change regenerates its consumers in the same change (`AGENTS.md:182-183`). If it is too large, split by feature (A: alone first, then B: and swap) rather than by repository.

### F4. Floppy acceptance (records)

The floppy titles above, on Kit A or B. The records go under `docs/validation/`.

### C1. `pc386_atapi` transport and the MMC library (RTL + runtime library, sim only)

- **Builds:** the CD control-page layout, published in mister-packages' generated definitions with its consumers regenerated in this PR (`AGENTS.md:182-183`); the secondary channel; the 170h split; the control-page client and PIO streamer; the MMC library with unit tests; `machine_tb`'s CD unit; `atapi_reference`; the `cdrom` and `cd-boot` suites.
- **Out:** the CMOS boot order stays hard-disk-first, and CD boot is a suite option.
- **Gates:**
  - the suites on the oracle and `--cpu rtl`;
  - library unit tests;
  - seal.
- **Estimate:** +700 ALMs, +2 M10K.

### C2. CD media ABI, runtime service and host (ABI, runtime, FogCast)

- **Builds:**
  - `fes.media.atapi-cd` 1.0 (the lifecycle); the runtime becomes a consumer of C1's control-page layout, and any change to that layout regenerates the RTL and `machine_tb` in the same PR;
  - the service thread;
  - the `cd` role with the cue parser, the 900 MB size class and kit staging;
  - media sets in the library;
  - the CD drive in the live-media API and tenfoot.
- **Gates:**
  - the tests in each repository;
  - Kit smoke 2, which records latency and ARM load;
  - the poll interval set from that measurement.
- **Depends on:** **D6** answered.

### C3. CD-DA (RTL + runtime)

- **Builds:** `pc386_cdda`; the audio ring and sub-channel from the play position; volume; the mixer input; the AUX arbiter bench with all four clients.
- **Gates:**
  - `cdda_tb`;
  - the `cdrom` suite's audio cases;
  - seal;
  - Kit smoke 3 with an HDMI audio capture.
- **Estimate:** +300 ALMs, +4 M10K, +2 DSP.

### C4. CD drivers on the games image (builder)

- **Builds:** UDVD2 and SHSUCDX (or the fallback) in the FreeDOS lock with licence rows; the boot-menu entry; the census manifest's media-set field.
- **Gates:** the builder's licence checks and the `cdrom` suite on the built image.
- **Depends on:** the SHSUCDX licence being settled.

### C5. CD acceptance (records)

The CD titles above. Windows 98 SE setup belongs to the early-Windows plan's milestones; C5 records only that the El Torito boot reaches its setup menu.

**Order:** F1 → F2 → F3 → F4, and C1 → C2 → C3 → C4 → C5. C1 can start in parallel with F2 once F1's AUX arbiter shape is merged, since both touch `pc386_port2.sv`'s `AUX` client. C2 depends on F3's unit-indexed operations.

## Open decisions

| | Decision | Recommendation |
| --- | --- | --- |
| **D1** | CD interface: ATAPI on the secondary channel, or a proprietary interface | ATAPI, secondary master. Not close for any target in view. Reopen only if the sibling OS reports name a target that predates ATAPI drivers |
| **D2** | Where the MMC command set runs: in RTL, or in the runtime with a thin ATAPI transport in RTL | Runtime. The runtime must serve every read anyway, since a disc does not fit the window, and the RTL version costs 1,500–2,000 ALMs at 91%+ fill |
| **D3** | How the core gets the runtime's attention: the runtime polls the control page, or a new FPGA-to-HPS interrupt through fes-window | Poll first; C2 measures ARM load and latency. The interrupt is a kernel change, kept for if polling costs too much |
| **D4** | Drive types | A: 3.5" 1.44M, B: 5.25" 1.2M, changeable per entry (2.88M and 720K available). Both drives present by default |
| **D5** | Share one FDC with PC/XT | Write `pc386_fdc` fresh under MIT from the 82077AA datasheet and Bochs's `floppy.cc`, parameterised by clock and geometry. Moving PC/XT onto it (and deleting `pcxt_fdc_card`, per the delete-don't-shim rule) is a separate PR with its own PC/XT reseal and kit check, if Deano wants one FDC. Taking the GPL module into pc386 instead would put GPL RTL into the pc386 bitstream; that licensing call is Deano's |
| **D6** | Where a CD image lives on the kit | Staged to kit storage by SHA-256, as the hard disk is, and read by the runtime from there. Needs Deano's word on kit storage capacity for 400–850 MB discs. Streaming from the host over the network is the alternative, at the cost of a network dependency during play |
| **D7** | Area at 91.6% after both devices, with Phase 5 still landing | Land the floppy first. Start C1 once Phase 5's measured area is known. Drop to one floppy drive or defer CD-DA if the seal will not close. Never relax gates |
| **D8** | CD-DA in mono now, or wait for #1441's stereo mixer | Mono now (L+R at half scale). Stereo when #1441 lands |
| **D9** | Write-protect default | Writable for disks in a game's own media set; write-protected for OS and tool install media from a shared folder; the player can flip it per insert |
