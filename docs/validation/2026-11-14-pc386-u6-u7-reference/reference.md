# The reference machines, as configured

86Box 6.0, build 9001 (the v6.0 release's Linux x86-64 AppImage), with the
86Box ROM set. Neither is committed; both live in `~/fes-scratch/u6ref` on
Powerboat with the game drives.

| | Ultima VI | Ultima VII: The Black Gate |
| --- | --- | --- |
| `machine` | `award286` — a SCAT 286 board, Award 286 Modular BIOS B3.11.03 | `ecs386` — ECS 386/32, a C&T 386/AT board, AMI 386 BIOS (03/03/89) |
| `cpu_family`, `cpu_speed` | `286`, 12,500,000 Hz | `i386dx`, 25,000,000 Hz |
| `cpu_use_dynarec` | 0 (interpreter) | 0 (interpreter) |
| `cpu_waitstates` | 1 (the board's own default) | board default |
| FPU | none | none |
| `mem_size` | 1,024 KiB | 8,192 KiB |
| `gfxcard` | `vga` (IBM VGA) | `vga` (IBM VGA) |
| `sndcard` | `adlib`, plus the PC speaker | `sb2.0` (Sound Blaster 2.0) |
| `mouse_type` | none | `msserial` on COM1 |
| `hdc_1` | `xtide_at` (XT-IDE Universal BIOS r631) | `xtide_at` |
| Shadow RAM | disabled | disabled (`Shadow F0000H,64K : Disabled`) |

**Why 12.5 MHz.** 86Box's 286 family offers 12.5 MHz, not 12.0: a request for
12,000,000 Hz is written back as 12,500,000. The reference therefore runs 4.2%
above the preset's nominal clock, which makes the reference faster, not slower.

**Why XT-IDE.** Neither 1988 board auto-detects an IDE disk, and the drive
images use a 65/16/63 geometry no CMOS drive type matches. The XT-IDE
Universal BIOS detects the disk itself, as a period IDE controller card with
its own BIOS would.

## The reference against real hardware, in TopBench's own database

TopBench 0.38a ran its continuous Realtime benchmark on each reference, from
the same benchmark drive the kit's presets were measured on
([#1486's record](../2026-11-04-pc386-m5-game-checks/summary.md)). Its database
(`DATABASE.INI` on that drive) holds submissions from real machines.

| Machine | TopBench |
| --- | --- |
| **86Box 286 at 12.5 MHz (the Ultima VI reference)** | **23** |
| Real 286s at 12–12.5 MHz in TopBench's database | 21, 22, 22, 23, 25, 27 |
| pc386 at the 286-12 preset (#1486) | 33 |
| **86Box 386DX at 25 MHz (the Black Gate reference)** | **40** |
| Real 386DX/SX machines at 20–25 MHz in the database | 25, 26, 39, 40, 41, 46, 46, 47, 47, 49, 60 |
| pc386 at the 386DX-25 preset (#1486) | 68 |

The 286 reference sits in the middle of the real 286s of its clock, so its
board defaults — `cpu_waitstates = 1` among them — are period-typical. The 386
reference sits inside the real range but on the slow side of it, which is what
a 1988 chipset with no cache and shadow RAM off should do; a cached 386DX-25
of 1990 would score higher.

CHKCPU 1.25 reports the 386 reference as a "classic 386" at 26.3 MHz, against
26.4 MHz for the kit's 386DX-25 preset. It refuses to run on the 286 reference
("requires a 386 or higher CPU"), which is itself the difference these checks
are about: the kit's 286-class preset is a 386 core paced to a table, not a
286.
