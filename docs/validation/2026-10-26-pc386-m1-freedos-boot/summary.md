# pc386 M1: Bochs BIOS POST and FreeDOS boot in simulation, 26 October 2026

**The pc386 AT core boots the Bochs BIOS, the LGPL VGABios and FreeDOS 1.4 to
its prompt in simulation, and a command typed at that prompt runs.** The CPU is
the Bochs level-3 oracle; every I/O port access, interrupt acknowledge, A20
change and CPU reset goes through the Verilated AT-core RTL. `dir`, typed as set
2 scan codes through the 8042, lists the disk and FreeDOS returns to `C:\>`.
Every CPU I/O completed at exactly its timing class. The 62 directed device
checks pass. This is a **host-only check**: no synthesis, routed result,
package or kit is involved. It meets the exit of phase M1 in the
[machine design](../../../sources/misteross/docs/pc386-machine-design.md#phases).

## Inputs

- misteross tree as on the PR branch after the first review round (on FES base
  `28d4bd2e9f`); the boot result is identical to the first run at `4315c9befb`.
  The AT core is in [`cores/fes-pc386/`](../../../sources/misteross/cores/fes-pc386/README.md).
- Firmware from `scripts/build_pc386_firmware.py`, matching `firmware.lock`:
  `pc386-bios.bin` (Bochs `rombios.c` at `REL_3_1_FINAL`, legacy build, PCI
  BIOS, APM and PnP off, INT 16h AH=03h added) SHA-256 `8caa3f2f…01eeda`;
  `pc386-vgabios.bin` (VGABios 0.9d without VBE) `69401dc8…b343c`. dev86
  `v1.0.1`, gcc 14.2.0 as the preprocessor.
- Disk from `scripts/build_pc386_freedos_image.py`: 65 × 16 × 63 sectors
  (32.0 MiB), one FAT16 partition, FreeDOS 1.4 kernel 2043 (`KERNL386.SYS`),
  FreeCOM 0.86a, Free FDISK 1.4.3's MBR code, the kernel's FAT16 boot sector
  assembled with NASM 2.16.03, mtools 4.0.48. SHA-256 `b9d36549…f0ccc5f`,
  reproducible. The image stays in `build/` and is not distributed.
- Bochs oracle `30cfd566` (CPU level 3), Verilator 5.032, model C++ at `-O3`.
- Machine model: 100 MHz system clock; four clocks per retired instruction
  (about 25 MIPS); 640 KiB + 15 MiB RAM; the text VGA stand-in described in
  the core README.

## Boot

`python3 scripts/sim_fes_pc386.py --suite boot` ([`boot-report.json`](boot-report.json)):

| | |
| --- | --- |
| Result | `END prompt`, no BIOS panic port written, no CPU reset |
| Instructions retired | 104,108,642 |
| Simulated time | 4.30 s (430,009,677 clocks); first prompt at 2.40 s |
| Wall time | 2 min 20 s |
| INTA | 86, each 146 clocks |

POST codes on port 80h: `00`, then `21`, `31`, `41`, `71`, `77` from the
keyboard initialisation's wait loops. Debug-port lines:

```
402 BIOS BUILD DATE: 10/26/26
500 VGABios ID: vgabios.c 2026-08-07
402 ata0-0: PCHS=65/16/63 translation=none LCHS=65/16/63
402 IDE time out
402 Booting from 0000:7c00
```

The "IDE time out" is the BIOS waiting for BSY on the absent slave after SRST:
with device 1 selected the status reads 00h, as in Bochs's own drive model,
and the BIOS lists that slave as "Unknown device". The final screen:

```
FreeCom version 0.86 - WATCOMC - XMS_Swap [Dec 30 2024 22:10:51]
C:\>dir
 Volume in drive C is FREEDOS
 Volume Serial Number is 1386-0026
 Directory of C:\
KERNEL   SYS        46,256  10-26-26 12:00p
COMMAND  COM        87,772  10-26-26 12:00p
FDCONFIG SYS            75  10-26-26 12:00p
AUTOEXEC BAT            39  10-26-26 12:00p
         4 file(s)        134,142 bytes
         0 dir(s)      33,101,312 bytes free
C:\>
```

(`AUTOEXEC.BAT` runs `VER`, which prints the FreeCom line.) The listing can
only appear if the typed keys reached DOS: set 2 bytes into the keyboard
model, PS/2 pacing, set 1 translation in the 8042, IRQ1 through the PIC, the
BIOS INT 09h handler and INT 16h.

## I/O timing

### Measured in the simulation

Request clock to done clock, every CPU I/O of the boot:

| Class | Access | Clocks | Count | Composition |
| --- | --- | ---: | ---: | --- |
| `isa8` | byte | 74 | 75,597 | one 72-clock (720 ns) cycle + 2 |
| `isa16` | word (1F0h) | 38 | 209,152 | one 36-clock (360 ns) cycle + 2 |
| `vga` | byte | 3 | 829 | posted to the stand-in |
| `vga` | word | 5 | 3,381 | two posted bytes |
| INTA | | 146 | 86 | two `isa8` acknowledge cycles + 2 |

The two extra clocks are the decoder taking the request and reporting
completion. Word and dword accesses to 8-bit ports (146 and 290 clocks) and the
bus-sized dword at 1F0h (182 clocks: the word cycle, then bytes at 1F2h and
1F3h) are checked by the device bench; the boot issues none of them once the
PCI BIOS is off. A full sector is 256 `isa16` words, 97 us
of guest I/O.

### Against published figures

| Source | Figure | Implies | Verdict |
| --- | --- | --- | --- |
| ISA bus reference ([isa.txt](https://www.ixbt.com/mainboard/download/isa.txt)): "The default for 8 bit transfers is 4 wait states", "The default for 16 bit transfers is 1 wait state"; BCLK 4.77–8 MHz typical, 8.3 MHz maximum | 2 bus clocks + 4 or 1 wait states | 6 and 3 BCLK: 720 and 360 ns at 8.33 MHz, 750 and 375 ns at 8 MHz | The classes are these cycles at the 8.33 MHz maximum |
| [Linux kernel list, 2007](https://lkml.iu.edu/hypermail/linux/kernel/0712.1/0671.html) | 8-bit ISA slave access six bus cycles with four wait states, 16-bit three with one: 0.75 and 0.375 us at 8 MHz | as above | Agrees |
| [Parallel-port list, 2000](https://cyberelk.net/tim/parport/archive/current/2940.html), citing *Parallel Port Complete* and a measurement | An ISA I/O read is 6 bus cycles; 1,385 ns measured at 4.33 MHz | 6 BCLK | Agrees: a period measurement of the same six cycles |
| AdLib programming guide, as quoted ([SWAG FAQ](https://swag.bttr-software.de/FAQ/0052.PAS.html), [VOGONS](https://www.vogons.org/viewtopic.php?p=1357920)) | Read the status port 6 times after a register write (3.3 us) and 35 times after a data write (23 us) | at least 550 and 660 ns per status read | 720 ns gives 4.3 and 25.2 us: both waits are met at any CPU speed |
| Linux `io_delay` ([source](https://elixir.bootlin.com/linux/v2.6.26-rc9/source/arch/x86/kernel/io_delay.c), [Torvalds, 2002](https://lkml.iu.edu/0203.1/0823.html)) | A port 80h write is used as a delay of about 1 us on ISA | one 8-bit ISA write | 720 ns is one such cycle at 8.33 MHz; "about 1 us" includes slower buses |

The 720 ns and 360 ns figures stand; the design table is unchanged and its
note now records this check. The sources are secondary: the IBM PC/AT
Technical Reference itself was not consulted. Real chipsets let the BIOS
change the wait states and the bus clock, so period machines spread around
these figures; the machine fixes the AT defaults.

## Device bench

`python3 scripts/sim_fes_pc386.py --suite devices` ([`devices.log`](devices.log)),
62 checks, all pass, about 12 s. Beyond what the boot exercises it checks:
the timing classes for words and dwords; IRQ8 and IRQ14 arriving through
master IR2 as vectors 70h and 76h, and EOI; PIT channel 0 at divisor 65536
interrupting every 5,492,541 ± 90 clocks (54.925 ms); the 8254 read-back
status, null count and count latch; port 61h bit 4 toggling every 15.09 us
with channel 1 at 18; a channel 1 DMA transfer through the CPU port at
123400h with auto-init; the second DMA controller and all page registers;
8042 self-test and command byte; A20 off through D1 visible before IBF clears,
on through port 92h, DF and DD; CPU reset by FE and by port 92h, each only
after FLUSH completes; keyboard reset pacing and BAT; set 2 to set 1
translation with breaks and the translated F2 identify; the aux mouse's ACK,
packet with dy negated, movement over 255 carried, quarter counts carried
toward zero at resolution 0, 100 ms between packets at 10 per second, and a
command reply that fits behind a full scan-code queue; every CMOS
configuration byte and the checksum; the wall-clock seed into BCD with the
century; UIP before the update, the year rollover, binary and 12-hour forms;
IDENTIFY; WRITE and READ SECTORS with the dirty chunk; READ MULTIPLE with an
IRQ per block; CHS through the IDENTIFY geometry; IDNF, ABRT, the absent
device 1 and the SRST signature.

## Recorded behaviour

- CMOS 14h = 04h, 1Bh-23h (disk geometry) and 3Fh bit 0 (no F12 boot menu) are
  written besides the bytes the design listed, as the Bochs emulator does; the
  design now lists them.
- ATA writes complete into the sector buffer and are stored behind it: the
  BIOS requires DRDY with BSY clear right after `outsw`. The design's disk
  section now says so.
- READ MULTIPLE prefetch needs the image port to deliver a sector faster than
  the guest reads one (about 92 us).
- KF8259 holds INT until acknowledged, so a request masked before INTA gives a
  spurious IR7.
- The keyboard model takes set 2 bytes; the HID row model and typematic repeat
  are not built.

## Not covered

The HID keyboard model, `fes.mouse.relative` and the `fes.computer` mailbox
(disk commit with CRC, freeze, dirty read-out, persistence, wall clock), the
VGA, audio, shell and producer. No synthesis has been run on the AT core.
