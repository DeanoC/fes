# i386 Phase 4 slice 4: the store-to-load wait, 6 November 2026

This record covers CPU Phase 4's slice 4
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). Until now, D held
every load for one cycle behind a store in AG. It now holds a load only when
a byte of each may be the same byte.

**Two trees.** The slice was built and measured first on slice 2's tree, then
again after slice 3 merged. Every result says which tree it comes from:

| Tree | What it is | Results |
| --- | --- | --- |
| `317310c81` (merged) | main `cdde5f6c2` with slice 3's return stack (#1512), merged into this branch | latency, lockstep (RTL and netlist), seal `254b2bf5`, Kit A run `s2` |
| `a421fb060` (pre-merge) | main `b12419644` (slice 2, no return stack) plus this slice | the same, with seal `a587e709` and run `s1`, plus the DOOM and Wolfenstein 3D bench windows |

The merge changed nothing in this slice's RTL; it brought in slice 3's
return stack beside it. The bench windows were not rerun on the merged tree.

**Result (merged tree, against [slice 3's record](../2026-11-06-i386-phase4-ras/summary.md)):**
- **Kit A, max.**
  - DOOM v1.9 `-timedemo demo3`: 39.2 fps, up from 38.7 (+1.3%). The plan
    projected 1–2%.
  - The DOS Benchmark Pack's DOOM: 35.4 fps at maximum detail, up from 34.9,
    and 97.5 at minimum, up from 96.9.
  - 3DBench: 76.9, unchanged. PCPBench: 13.2, up from 13.0. TopBench: 309,
    up from 307.
- **Microbenchmarks.** A store, then a load from another line, costs 2.0
  clocks a pair, down from 3.0. A load that shares a byte with the store, or
  is 4 KiB from it, still costs 3.0.
- **Lockstep.** It passes as main does, on the RTL and on the Quartus build's
  Yosys netlist. The one failure is
  [#1510](https://github.com/DeanoC/fes/issues/1510), as on main.
- **Seal.** Quartus 17.0.2 meets every analysis. The worst setup slack is
  +0.410 ns, on the pixel clock. The system clock's worst is +0.911 ns, and
  hold slack is +0.102 ns.
- **Timing cost.** D's registers have lost about 4 ns of slack: D's EIP has
  +1.13 ns, where slice 3's seal had +5.57 ([below](#timing)).
- **Profile (pre-merge tree).** In the DOOM `demo3` bench window, the
  store-to-load wait falls from 2.65% of clocks to 0.10%. About 45% of what
  it saves comes back as AGI waits, and CPI falls from 1.811 to 1.781.
- **Wolfenstein 3D gains no frames, as expected (pre-merge tree).** Its bench
  window still flips 70 pages a second, the game's limit, and the clocks it
  needs a frame fall by 1.2%. The loop that is 69% of the window is the
  game's wait for the next timer tick, and both loads that waited there read
  the bytes the store just before wrote ([below](#wolfenstein-3d)). The
  plan's 6–11% came from that loop.

## Design as built

[The store-to-load check](../../../sources/misteross/docs/i386-cpu-design.md#the-store-to-load-check)
has the full description. In short:
- **What D compares.** Bits 11:0 of each byte's linear address, modulo 4096.
  Two bytes can be the same physical byte only if these bits agree. A20M
  masks bit 20, the reset alias moves bits 31:16, and a page keeps its
  offset.
- **The test.** With the load's bytes at L and the store's at S, and nl and
  ns bytes long, D holds the load when e = L − S + nl − 1 (mod 4096) is at
  most nl + ns − 2.
- **Where the bits come from.** D works out the load's from the operands it
  hands AG: the segment term, the row, the last operand and the
  displacement (`d_lo`). The store keeps its own value in AG (`ag_slo`).
  When a late value from M1, EX or WB replaces an address operand, the bits
  are not known at D, and the load waits as before. That covers a load
  through ESP right after a push.
- **Unchanged.**
  - Uncached loads still read only as the oldest uop.
  - M2's merge still takes only bytes that write RAM.
  - I/O still runs at commit.
  - MOVS's st2 stores were never held behind.
  - Forwarding is not added.
  - The self-modifying-code check is made at a store's commit, whatever
    loads do.

## Latency microbenchmarks

`scripts/test_fes_i386_latency.py`, clocks for 1,000 iterations of eight
pairs. Before is main's RTL; both trees give the same figures for these
loops:

| Loop | Before | After |
| --- | ---: | ---: |
| `MOV [DI], AX` then `MOV CX, [SI]`, another line (`store_load_other`) | 26,059 | 18,059 |
| ... then `MOV CX, [DI + 4]`, the next dword (`store_load_line`, new) | 26,050 | 18,057 |
| ... then `MOV CL, [DI + 1]`, a byte it wrote (`store_load_part`, new) | 26,030 | 26,030 |
| ... then `MOV CX, [DI + 1000h]`, 4 KiB on (`store_load_alias`, new) | 26,059 | 26,059 |
| `MOV [DI], AX` then `MOV CX, [DI]` (`store_load_same`) | 26,059 | 26,059 |

Every other loop takes main's clocks exactly, slice 3's included.
[latency-report.json](latency-report.json) is the merged tree's report.
- **Placement.** The new loops run after the subroutines, behind a JMP that
  takes the three bytes of the old OUT and HLT. A 64-byte zero pad follows
  RETF.
- **Why it matters.** Without this, `near_return` and `far_return` move, and
  so do the wrong-path bytes fetched after RETF. The CALL loops' clocks then
  change by up to 8,000 for no reason in the CPU.

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus).
The RTL and the netlist (Yosys `238eac9a`) give the same results, on each
tree:

| Part | Merged tree | Pre-merge tree |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 31,999 of 32,000 | 31,999 of 32,000 |
| Protected-mode programs | 8,000 of 8,000 | 8,000 of 8,000 |
| Directed cases | 57 of 57 | 50 of 50 |
| Unreal, protected-mode, no-387 and memory cases | 7, 92, 1,933 and 27, all pass | same |
| Holds without latency | pass | pass |

The one failure is `random#7#1936`, a CALL rel past CS's limit
([#1510](https://github.com/DeanoC/fes/issues/1510)). It fails the same way
on main. The merged tree's directed cases add slice 3's seven to this
slice's seven.

**New directed cases.** Seven `stld-*` groups run against Bochs. Each group
counts as one case. Each run is `MOV CX, 2; L: NOT EDX; store; load; LOOP L`:
- The second pass runs from the L1I with the load right behind the store.
- It stores the complement of the first pass's bytes, so a load that misses
  the store reads the first pass's.
- The store is DL, DX or EDX to `[BX]`. The load is AL, AX or EAX from
  `[SI]`, at offsets −8 to 8 from the store, with every pair of sizes.

The groups vary where the store is:

| Group | Store |
| --- | --- |
| `stld-mid` | mid-line |
| `stld-line` | across a line |
| `stld-page` | across a page |
| `stld-wrap` | at FFFCh, the load past the 64 KiB wrap |
| `stld-alias` | load 4 KiB on |
| `stld-late` | `INC BX` before the store, so its address is late |
| `stld-push` | a push then a load from `[ESP + d]`, so the load's base is late |

The memory cases add `fwd-mmio-near`: a store to B8010h, then a load from
B8014h, which follows it directly and must read the device.

**The hold check.** The bench used to require every load right after a store
to retire at least two cycles after it. It now requires that only where
Bochs's one read meets the store's one write in address bits 11:0
(`store_load`). The Bochs adapter now logs reads for this. Other loads after
a store (`store_other`) are counted, not checked. On the merged tree:

| Loads after a store, no latency | Count | Gap |
| --- | ---: | --- |
| Meets it in bits 11:0 (`store_load`) | 1,037 | 99.8% two cycles apart |
| Other (`store_other`) | 1,361 | 7.6% one cycle apart |

Most random loads after a store take their address register from the
instruction just before. That value is late at D, so they still wait.

## Timing

`make build-fes-pc386-quartus SEED=1` ran from a clean checkout of each tree
([seal-summary.json](seal-summary.json) is the merged tree's;
[seal-summary-a421fb060.json](seal-summary-a421fb060.json) the pre-merge
one's):

| | Merged `317310c81` | Pre-merge `a421fb060` |
| --- | --- | --- |
| Package | `254b2bf5…` | `a587e709…` |
| Build ID | `68e4b90f…` | `1ea83012…` |
| Worst setup slack | +0.410 ns (pixel clock) | +1.025 ns |
| System clock's worst setup slack | +0.911 ns | +1.407 ns |
| Hold slack | +0.102 ns | +0.111 ns |
| ALMs | 32,662 | 31,556 |
| M10K and DSP blocks | 263 and 38 | 262 and 38 |

Slice 3's seal had +1.020 ns of setup slack and used 32,544 ALMs. Slice 2's
had +0.800 ns and 31,502.

**D's slack.** D's `issue` now waits on the 12-bit sum of the load's address
operands, so the registers it enables have lost most of their margin. The
table is TimeQuest's worst setup slack into each group at the −40 °C corner
([stage-slack.json](stage-slack.json)):

| Registers | Slice 3 `0a16a224` | Pre-merge `a587e709` | Merged `254b2bf5` |
| --- | ---: | ---: | ---: |
| Q's start loop (`frontend`) | +1.28 ns | +1.83 ns | +0.91 ns |
| D's EIP (`d_eip`) | +5.57 ns | +1.46 ns | +1.13 ns |
| The string copies (`r_src`, `r_dst`, `r_cnt`) | +5.3–6.6 ns | +1.6–2.0 ns | +1.1–1.3 ns |
| D's address forwarding (`dfwd`) | +5.86 ns | +1.91 ns | +1.84 ns |

- **Path.** The worst path into `d_eip` starts at `dfwd`'s ready bits.
- **Q's start loop.** It keeps slice 3's cost, and moves ±0.5 ns from fit to
  fit.
- **Effect on later slices.** The plan put the early ALU (slice 6) in AG,
  where D and AG had 4.7–5.4 ns. That room is now about 1.1 ns.
- **A way back.** Keep the 12-bit sum off `issue`'s path, for example by
  registering part of it a cycle earlier. This slice does not try that.

## Kit

Kit A ran core-only at max, on baseline software `a8cd30c4` and image
`f904961e`, with no install, no image change and no reboot
([kit-scores.json](kit-scores.json)).
- **Runs.** `s2` ran on 6 November 16:55–17:07 with the merged seal
  `254b2bf5…`. `s1` ran 15:30–15:43 with the pre-merge seal `a587e709…`.
- **After each run.** Kit A was checked against its snapshot and logged
  FREE. The run's five FogCast data trees were removed, and the kit was
  logged FREE again.
- **Disk.** The baseline's bench disk, `fe81c9c4…`, staged from Powerboat. It
  is not committed.
- **Commands.** Each was the baseline's, after a SCRUBX launch.

**Merged tree, against slice 3's `r1`:**

| Benchmark | Before (r1) | After (s2) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,492 realtics, 38.7 fps | 3,863 in 3,448, 39.2 fps | +1.3% |
| 3DBench | 76.9 | 76.9 | 0 |
| PCPBench | 13.0 | 13.2 | +1.5% |
| TopBench | 307 | 309 | +0.7% |
| Pack DOOM, maximum detail | 2,134 in 2,141, 34.9 fps | 2,134 in 2,111, 35.4 fps | +1.4% |
| Pack DOOM, minimum detail | 2,134 in 771, 96.9 fps | 2,134 in 766, 97.5 fps | +0.7% |

**Pre-merge tree, against slice 2's `k1`:**

| Benchmark | Before (k1) | After (s1) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 in 3,608, 37.5 fps | 3,863 in 3,566, 37.9 fps | +1.2% |
| 3DBench | 71.4 | 71.4 | 0 |
| PCPBench | 12.8 | 13.0 | +1.6% |
| TopBench | 303 | 307 | +1.3% |
| Pack DOOM, maximum detail | 2,134 in 2,218, 33.7 fps | 2,134 in 2,188, 34.1 fps | +1.4% |
| Pack DOOM, minimum detail | 2,134 in 805, 92.8 fps | 2,134 in 803, 93.0 fps | +0.2% |

The kit runner has no Wolfenstein 3D timing; the bench has it below.

## Profile

The machine bench reran DOOM v1.9's `-timedemo demo3` as slices 1 to 3 did,
on the pre-merge tree:
- the profile's disk `7d98bffd…`;
- the oracle to page flip 40, then the RTL from cycle 2,222,604,910;
- the window 0.5–1.0 s after the handover, 50,000,000 clocks
  ([doom-window.json](doom-window.json)).

| | Slice 2 | This slice |
| --- | ---: | ---: |
| Instructions retired | 27,605,211 | 28,067,637 (+1.7%) |
| CPI | 1.811 | 1.781 |
| Store-to-load wait | 1,327,251 clocks (2.65%) | 52,008 (0.10%) |
| AGI wait | 3,757,159 clocks | 4,331,317 |

A load that no longer waits behind a store may now wait one cycle for its
address register instead, so AGI takes back about 45% of the saving.

## Wolfenstein 3D

The profile record's Wolfenstein 3D window spends 59.7% of its clocks in one
seven-instruction loop at linear 1F9C6h. Two of its loads waited behind
stores, 6.3% of the window each, and the plan's 6–11% gain came from them.

A Bochs run of the same disk to 17 s gives the loop's bytes, at CS:0CF6h.
It is the game's wait for the next timer tick:
- **The loop.** It copies the tick count into a local, reads the local back,
  subtracts the last frame's count, stores the difference, then compares
  that stored word with zero and loops while it is.
- **The two waiting loads.** They are the read-back of the local and the
  compare of the stored difference.

- **Both loads still wait.** Each reads exactly the bytes the store before
  it wrote, so they keep their hold.
- **Nothing to gain anyway.** The loop spins until the timer ticks, so a
  faster loop only spins more often.
- **Measurement.** The bench's window flips 70 pages a second, one a tick,
  which is the game's limit. Page flips therefore cannot show a CPU gain, so
  this record uses busy clocks a frame instead: the window's clocks less the
  loop's, over its frames.

**Bench windows.** Disk `1d2c9314…`, Bochs to 16.5 s, then the RTL, keys as
in the profile record. The window is 17.0–20.5 s
([wolf3d-window.json](wolf3d-window.json)):

| | Main `b12419644` | Pre-merge `a421fb060` |
| --- | ---: | ---: |
| Frames | 246 | 246 |
| `CalcTics` loop | 68.7% of clocks | 69.1% |
| Busy clocks a frame | 445,460 | 439,932 (−1.2%) |
| Store-to-load wait | 59,947,576 clocks | 53,959,926 |

The store-to-load wait that remains is almost all the loop's two loads.
