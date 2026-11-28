# Quake textured spans

In the published i386 `D_DrawSpans8` routine, the candidate second slot can issue **12 pairs** in the first eight-pixel segment of each of two specified spans. One pair issues the texture load `mov (%esi),%bl` alongside the independent pixel-count update `sub $8,%ecx`. The span code mixes texture accesses and register arithmetic with carry-dependent updates and x87 operations. [id Software’s renderer](https://github.com/id-Software/Quake/blob/bf4ac424ce754894ac8f1dae6a3981954bc9852d/WinQuake/d_draw.s#L313-L514)

| Pixels at segment entry | Instructions through final JA | x87 instructions | Greedy nonoverlapping pairs | Ideal issue slots saved | Branch at segment end |
| ---: | ---: | ---: | ---: | ---: | --- |
| 17 | 114 | 15 | 12 | 10.53% | Taken, for the next eight-pixel segment |
| 9 | 108 | 7 | 12 | 11.11% | Falls through, for the last pixel |

The 17-pixel path starts a divide, then executes 30 integer or branch instructions before the next segment starts with an x87 instruction. This counts the distance between the divide and its next consumer. A cycle-level trace would be needed to tell how much latency an independent x87 unit could hide. The percentages divide pairs by instructions, assuming an ideal one-instruction-per-clock baseline.

The fixture has one scanline at `u=v=0`, with constant `1/z=1`, texture row 1, and one texture column per screen pixel. Both coordinates stay in bounds. The vertical step is zero, selecting `LZero`. Subtracting 8 from the width leaves 9 or 1, which selects the continuing or final path. The measured region runs from `LNotLastSegment` through its final conditional branch inclusive. It omits setup before the segment and the last-pixel handler.

The script assembles the two unmodified Quake sources as 32-bit x86 and applies the shared classifier to consecutive instruction bytes. **Unicorn executed both complete calls.** All 17 and 9 pixels and the following guard byte matched. The instruction hook observed every listed program counter and each final branch target. These runs measure two renderer paths. Timedemo frequencies, stall cycles, decoder bandwidth and FPGA clock remain unmeasured.

## Reproduction

Use the `WinQuake` directory from id Software’s commit `bf4ac424ce754894ac8f1dae6a3981954bc9852d`. The script checks SHA-256 for the two assembly sources and four upstream headers. The renderer source hash is `6471ec615e7a619e8f8d9447736672c0eb5c4985b9bc3e4710a0e127dd1c62e2`. The build used GCC 14.2 and GNU binutils 2.44, without a 32-bit C library. [Upstream sources and license](https://github.com/id-Software/Quake/tree/bf4ac424ce754894ac8f1dae6a3981954bc9852d)

```sh
python3 measure.py /path/to/Quake/WinQuake --unicorn --list
```

`--unicorn` uses Unicorn 2's Python bindings to verify the branch trace and output. The recorded run used the upstream Linux x64 library, SHA-256 `59d4110d3abed9403b2fe2469215aadc9cd13e4eb9e7d800e2567d142a30448f`. `--list` prints each instruction's bytes and marks the first instruction of every selected pair. On Linux with i386 executable support, `--check-pixels` runs the pixel check natively. [Upstream emulator build](https://github.com/unicorn-engine/unicorn/actions/runs/35098084807/artifacts/10446957991)

### DOS executable cross-check

The DOS Benchmark Pack's QUAKE.EXE contains the same 538-byte span region at file offset `0xfffb`. Comparing it to the assembled upstream region accounts for 54 absolute data-address relocations and different displacements for four branches that both fixture paths skip. All other bytes match, including the final loop-back at file offset `0x1020f`. Applying the classifier to the DOS bytes gives 12 pairs for each path. The executable SHA-256 is `7cdb244122cc607cc6f162524bfcac60f6d11d8ca2ff07473d6cd962311161fb`. The FES census identifies its Quake test drive by disk digest. The identity of the executable inside that disk and the frequency of either span path during the timedemo remain unverified. [DOS Benchmark Pack executable](https://github.com/SaxonRah/blitzBUS/blob/58dc068b0e76aac47c3bb6cef2c877f6473b446c/dos_bench/MARKS/QUAKES/QUAKE.EXE) · [FES census drive provenance](https://github.com/DeanoC/fes/blob/ebe9d522eee31beb1ddc25be5ea9a6662e08e060/docs/validation/2026-11-12-i386-x87-plan/summary.md)

```sh
python3 measure.py /path/to/Quake/WinQuake --dos-exe /path/to/QUAKE.EXE
```
