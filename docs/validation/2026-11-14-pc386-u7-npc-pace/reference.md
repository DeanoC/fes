# The 386DX-25 reference, as configured

86Box 6.0, build 9001 (the v6.0 release's Linux x86-64 AppImage), with the
86Box ROM set. Neither is committed; both live in `~/fes-scratch/u6ref` on
Powerboat with the game drives. This is the same installation
[the 14 November Ultima VI record](https://github.com/DeanoC/fes/pull/1838)
used for its 286 reference.

| | value |
| --- | --- |
| `machine` | `asus3863364k` — [SiS 310] ASUS 386/33-64K, Award 386 Modular BIOS v4.00 (#401A0), 64 KB cache, POST prints `Cache controller ... Enabled` |
| `cpu_family`, `cpu_speed` | `i386dx`, 25,000,000 Hz |
| `cpu_use_dynarec` | 0 (interpreter) |
| FPU | none |
| `mem_size` | 8,192 KiB |
| `gfxcard` | `vga` (IBM VGA) |
| `sndcard` | `sb2.0` (Sound Blaster 2.0), plus the PC speaker |
| `mouse_type` | `msserial` on COM1 |
| `hdc_1` | **`ide_isa`** — [ISA16] PC/AT IDE Controller, no option ROM |
| CMOS drive C | **user type 47**, 65 cylinders / 16 heads / 63 sectors / precomp 0 / landing zone 65 |
| ISA memory expansion card 1 | **`genericat`** — Generic PC/AT Memory Expansion, `size = 96`, `start = 800` (96 KiB of RAM at C8000–DFFFF) |
| Shadow RAM | board default (`Shadow : BIOS`) |

The last three rows are the changes this work made, and
[`summary.md`](summary.md) gives the evidence for each. The CMOS drive type
lives in the VM's `nvr/asus3863364k.nvr`, so that file is part of the
reference: a VM directory without it will not find the disk.

## Why those three changes

The game would not start on any earlier configuration. The cause, read from
inside the guest rather than inferred, is in `summary.md`; in short, the games
image's UMB provider needs read/write RAM at C8000, and on the reference that
address held the XT-IDE Universal BIOS option ROM, with open bus above it.
Replacing the XT-IDE card with a plain AT IDE controller frees C8000–C9FFF,
and the memory card supplies C8000–DFFFF.
