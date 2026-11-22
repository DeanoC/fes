# pc386 VGA area: results

The method, with its amendment, is [method.md](method.md), committed before the
board fits.

**This PR changes only behaviour-preserving parts:**
- `pc386_vga.sv` and `pc386_vga_scanout.sv`;
- the capture bench's instance of the scanout, whose palette ports changed.

The VGA is cycle-exact at its ports against main, every output in every clock;
[diff/](diff/) is the proof. Cuts that would change what a guest or the host
sees are listed [below](#cuts-that-change-behaviour-not-made) with their
bounds, and none of them is made.

## Result

**DE25 board pair (387 off, #2036 board, four seeds each): −242.75 ALMs, measured.**

| | Median ALMs | Spread | Registers (median) | M20K | MLAB memory ALMs | DSP |
|---|---:|---:|---:|---:|---:|---:|
| Baseline (`3401bb193`) | 41,971.4 | 30.1 | 48,027 | 167 | 390 | 24 |
| Arm (with this PR's VGA) | 41,728.65 | 75.3 | 47,752 | 167 | 410 | 24 |
| **Arm − baseline** | **−242.75** | | −275 | 0 | +20 | 0 |

- **Measured:** the saving, 242.75, is larger than the larger spread, 75.3.
- **Main at the time:** both arms contain main at `f3140cb33`. That is **before** #2075, the OPL2 audio area cut, which has merged since. The pair measures the VGA change alone, and #2075 changes no VGA file.
- **Timing:** every seed of both arms closes at 61.437908 MHz, with no "not met" in any STA report. The details are [below](#de25-timing-per-seed).
- **Figures:** per seed in [de25/pair.json](de25/pair.json), from [collect.py](collect.py). The run scripts and logs are in [de25/](de25/).

**DE10:**
- The registered producer resealed it at `459381f3d`, with a fresh lockstep;
  every analysis closes at 61.44 MHz.
- Kit B ran FreeDOS and Wolfenstein 3D on that package and passed.

## What changed

The VGA-only Agilex 5 fit is a virtual-pin fit of `pc386_vga` alone, Quartus Pro
26.3, seed 1, with the netlist from the seal's Yosys recipe. Its figures are
per-cut estimates; the board pair is the measurement.

| Cut | VGA-only ALMs | Δ | Registers | RAM blocks | MLAB memory LABs |
|---|---:|---:|---:|---:|---:|
| main (`f3140cb33`) | 2,271 | — | 1,756 | 14 | 0 |
| 1: MMIO read path and one translation per dword | 2,111 | −160 | 1,596 | 14 | 0 |
| 2: palette and CRTC readback copy in MLAB (with 1) | 1,988 | −123 | 1,552 | 14 | 2 |
| **Total** | | **−283** | −204 | 0 | +2 |

Fit records are in [fits/](fits/). fmax is still far above 61.44 MHz: the slow
100 °C corner went from 171.2 MHz on main to 164.7 MHz with both cuts.

### Cut 1: the MMIO read path

**Before:**
- A read held up to four 64-bit beats in registers (`read_beats`).
- At the answer it picked each byte's 32-bit word out of them, through a 4:1
  beat select, a 15-bit subtractor per byte, and a 2:1 half select.

**After:** each byte's result is formed as its beat arrives, and the latches
load from the highest byte read, as before.

**Why this is exact:** I enumerated every dword address under every memory
map, chain-4 and odd/even setting, 1,048,576 cases, with no violations. In
every case the four bytes of a dword share:
- whether they are inside the map;
- offset bits 15:2.

From that:
- **Translation:** one translation per dword serves all four bytes. Bits 1:0
  are byte 0's in chain-4, `{b[1], 0}` in odd/even, and `b` in planar mode.
- **Read length:** a read is one beat, or two when its enabled bytes straddle
  offset bit 1. The 3-bit beat count and the subtractors can never exceed
  that.
- **Write side:** the group is all live bytes in chain-4, the first byte's
  pair in odd/even, and the first byte alone in planar mode. The four 16-bit
  offset comparators go.
- **Registers:** the registers the result uses cannot change while a read is
  out, because the engine applies no FIFO entry until it answers.

### Cut 2: palette and CRTC readback in MLAB

**What changed:**
- The 16-entry palette was 128 registers, read through a 16:1 mux by the pixel
  path and another by attribute register reads. It is now two 16 × 8 MLAB
  copies, read asynchronously.
- CRTC register reads went through a 25:1 mux over the CRTC registers. They now
  read a 32 × 8 MLAB copy.
- The scanout still reads its CRTC registers from `crtc`. The six CRTC
  registers only readback used (2–5, 21h, 22h) now have no readers, so
  synthesis drops them.

**Reset:**
- A RAM is not cleared by reset, so each entry carries a "written since reset"
  bit (16 + 25 flip-flops).
- An entry reads as zero, its reset value, until it is written.
- CRTC protection writes the copy with the same value as the register: CRTC 7
  keeps bits 7:5 and 3:0 and takes bit 4.

## Proof of behaviour

**Differential bench ([diff/](diff/), [results.txt](diff/results.txt)):**
- Main's VGA, renamed, runs beside this one on random traffic. The bench fails
  on the first clock where any output differs, including `io_rdata` and
  `mem_rdata` between strobes.
- **RTL:** 32 seeds × 2,000,000 clocks, 64M clocks in all: 2.16M port bytes,
  4.18M MMIO writes, 2.25M MMIO reads, 5.75M port-1 read beats, 833k capture
  beats and 312 random mid-run resets. **All pass.**
- **Netlist:** this branch's netlist from the seal's locked Yosys recipe,
  against main's RTL, 16 seeds × 2,000,000 clocks. **All pass.**
- **Mutation check:** the bench fails on each of these planted bugs:
  - latch from the lowest byte;
  - beat match without the first beat's bit;
  - odd/even group swapped;
  - odd/even offset bits swapped;
  - each of the three reset masks removed;
  - the CRTC 7 protection value.

  The reset masks are caught only by seeds that reset mid-run.

**Existing suites on this head:**
- `sim_fes_pc386.py --suite vga`: **PASS**.
  - Every marked 720p frame differs from the presenter model in 0 pixels.
  - The Bochs replay matches over 13,481,172 events, with 0 VRAM, read and
    frame mismatches.
- `--suite system`: **PASS**. FreeDOS reaches the prompt at cycle 191,533,231;
  every check passes, and the status ring has 0 late records.
- The same suite on main (`f3140cb33`) reaches the prompt at the **same
  cycle**, 191,533,231. Its CHECK lines are identical, apart from wall-clock
  seconds.

**DE10 seal ([de10/seal-summary.json](de10/seal-summary.json)):**
- Producer: `build_pc386_product.py --with-lockstep --seed 1` from a clean
  worktree of `459381f3d`, Quartus 17.0.2 Lite on Powerboat under its Lite
  lease.
- Results:
  - BUILD_ID `8de03af62c45b5074d9ff87505a3e8fe`;
  - RBF `2175204a…`, package `73a4873d…`;
  - 36,308 / 41,910 ALMs and 282 RAM blocks;
  - the fresh netlist lockstep passed.
- Worst slack at 61.44 MHz:
  - setup +0.248;
  - hold +0.098;
  - recovery +6.127;
  - removal +0.289;
  - minimum pulse width +1.122.

### DE25 timing per seed

Worst slack in ns over every clock and corner:

| Seed | Setup | Hold | Recovery | Removal | Min pulse width |
|---|---:|---:|---:|---:|---:|
| base s1 | 1.424 | 0.000 | 7.743 | 0.032 | 0.220 |
| base s2 | 1.551 | 0.000 | 7.479 | 0.102 | 0.220 |
| base s3 | 1.299 | 0.000 | 7.427 | 0.095 | 0.220 |
| base s4 | 1.505 | 0.000 | 7.618 | 0.082 | 0.220 |
| arm s1 | 1.592 | 0.000 | 7.161 | 0.083 | 0.220 |
| arm s2 | 1.599 | 0.000 | 7.061 | 0.019 | 0.220 |
| arm s3 | 1.540 | 0.000 | 7.175 | 0.000 | 0.220 |
| arm s4 | 1.505 | 0.000 | 7.625 | 0.013 | 0.220 |

- **Zero slacks:** every slack is ≥ 0. The 0.000 entries are exact zeros, not rounding of a negative value:
  - Hold is 0.000 on every seed of both arms, on PLL output clocks.
  - Arm s3's removal is 0.000 on `adv7513_config`, the HDMI transmitter's configuration clock, with a TNS of 0.000. It is not a VGA path.

### DE25 arm seed 1: why it was rerun

**The first build was not counted.**
- The first arm board build, seed 1, compiled fully, with fit and STA complete. Its producer then exited 1 with `de25-pc386: source changed during the board build`.
- That check is the producer's last `identity()`, a `git status` of the build worktree. It fired at 22:06:46Z, 00:06:46 local.
- That is inside the window, about 00:05–00:38 local on 22 November, when a Lander job had written its own `core.worktree` into the shared repository config. Every linked worktree's `git status` then compared that job's files.
- The elaboration had finished about 75 minutes earlier, from `git show` of the revision, and the post-compile topology check passed.

**What counts instead:** seed 1 reran the same way as seeds 2–4 ([de25/vga-area-s1copy.sh](de25/vga-area-s1copy.sh), [log](de25/board-arm-s1.log)).
- It is a copy of the first build's generated project, with SEED 1.
- It compiled the identical netlist (`aef0daf9…`) and ran no Yosys.
- Its fit equals the uncounted run's: 41,732.8 ALMs and the same slacks.

**Two earlier reruns stopped before any fit** ([a](de25/board-arm1r-a.log), [b](de25/board-arm1r-b.log)). Each was a full board build, and each hit a locked-Yosys assertion in `OPT_CLEAN`. They were not used.
- That is a thread-count race in the pc386 lock's Yosys, reported on [DeanoC/yosys#77](https://github.com/DeanoC/yosys/issues/77#issuecomment-7079444384).
- With more than one thread, that `OPT_CLEAN` either:
  - asserts;
  - hangs;
  - or finishes with netlist bytes that vary from run to run. The variation is a consistent renaming of function-result wires; the logic is the same.
- With one thread it is clean and stable.
- A rebuilt netlist would therefore not have matched seeds 2–4's bytes. That is why seed 1 reran from the project copy.

**Kit B ([kit/](kit/)):**
- Core-only run on the baseline: image `179affbb…`, bundle `c9ae79e2…`. Nothing
  was installed and nothing rebooted.
- The kit was claimed and restored in `kitB-STATUS.md`
  ([kit/kitB-status-lines.txt](kit/kitB-status-lines.txt)).
- **k4: pass.**
  - The FreeDOS prompt was seen in 2.2 s.
  - Wolfenstein 3D ran in graphics mode, lit 93.8%, with 56.2% of the frame
    changed after walking.
- k2 and k3 failed before launch with a broken pipe. My runner named the
  `.fcore` file by a label, and the driver sends the file's stem as the
  package ID. Each run was restored and logged FREE, and k4 used the
  package-ID name.

## Cuts that change behaviour (not made)

Each needs a decision. The figures are bounds from the locked-Yosys bench,
where an ablation removes the logic outright: Cyclone V ALUT and FF deltas
against main's VGA at 3,862 ALUTs and 2,257 FFs, not ALMs. The DE25 figure for
a real cut would be smaller.

| Candidate | Bound (ALUT / FF) | What changes |
|---|---:|---|
| Register reads take one more clock: a registered readback mux | up to −453 / −230 (all readback; most of the CRTC and palette part is already gone with cut 2) | `io_done` for a port read comes a clock later. The guest sees the timing, lockstep cycle counts move, and governor tables would need checking. |
| Host counters `scanout_underflows` and `capture_drops` from 32 to 16 bits | −75 / −31 | The status block's counters wrap at 65,536. Host-visible, not guest-visible. |
| Drop the CGA interleaved shift mode (graphics controller 5 bit 5) | −32 ALUT | Modes 4, 5 and 6 (CGA graphics) would show wrong pixels. |
| Drop the text cursor | −20 ALUT | No hardware cursor. |
| One dot-clock rate generator, switched instead of two free-running ones | about 10 ALMs (fit rows rate0 10, rate1 11) | The dot clock's phase after a misc-output clock switch. |
| One DAC RAM read by both the pixel path and the CPU, time-shared | 0 ALMs, −1 M20K | `3C9h` reads wait for a free slot: a clock more on some reads. |

**Considered and not built (behaviour-preserving, but not worth it):**
- **Capture address computed incrementally:** 400 × line kept in a register
  instead of three adders. −4 ALUT, +19 FF in the bench.
- **`VRAM_BASE | offset` instead of `+`** (the base's low 20 bits are zero):
  −4 ALUT.
- **The coalescer's 4 × 32-bit byte-enable mask** (bench bound −237 ALUT /
  −128 FF, the largest remaining front-end item):
  - It could live in the line RAM: 72-bit words with 9-bit lanes, plus a
    "fresh" bit per line beat in place of the clear.
  - That depends on how the locked Quartus Lite and Pro infer 9-bit byte
    lanes, and Cyclone V M10K may not take them.
  - A possible follow-up, measured on its own.

## Found on the way (pre-existing, not changed)

A DAC write to the index the pixel path reads in the same clock is a
read-during-write on `dac_scan`. With `PC386_RAM_COLLISION` the RTL poisons
that one pixel's colour, which is what an M10K or M20K leaves undefined. It
showed up because the differential bench compared poisoned RTL with the
netlist; main's own netlist differs from main's RTL at the same clock. It
affects one pixel of one frame per such write, and nothing persists.
