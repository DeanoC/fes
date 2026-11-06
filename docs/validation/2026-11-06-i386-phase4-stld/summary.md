# i386 Phase 4 slice 4: the store-to-load wait, 6 November 2026

This record covers CPU Phase 4's slice 4
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). Until now, D held
every load for one cycle behind a store in AG. It now holds a load only when
a byte of each may be the same byte.

**Tree.** Every result here comes from main `b12419644` plus this slice, at
`a421fb060`. That tree has slice 2's BTB (#1511) but not slice 3's return
stack (#1512, open). Every "before" figure is from main or from
[slice 2's record](../2026-11-06-i386-phase4-btb/summary.md).

**Result:**
- **Kit A, max.**
  - DOOM v1.9 `-timedemo demo3`: 37.9 fps, up from 37.5 (+1.2%). The plan
    projected 1–2%.
  - The DOS Benchmark Pack's DOOM: 34.1 fps at maximum detail, up from 33.7,
    and 93.0 at minimum, up from 92.8.
  - 3DBench: 71.4, unchanged. PCPBench: 13.0, up from 12.8. TopBench: 307, up
    from 303.
- **Wolfenstein 3D gains nothing, as expected.** The loop that is 60% of its
  bench window is the game's wait for the next timer tick, and both loads
  that waited there read the bytes the store just before wrote. The plan's
  6–11% came from that loop
  ([below](#wolfenstein-3d)).
- **Microbenchmarks.** A store, then a load from another line, costs 2.0
  clocks a pair, down from 3.0. A load that shares a byte with the store, or
  is 4 KiB from it, still costs 3.0.
- **Lockstep.** It passes as main does, on the RTL and on the Quartus
  build's Yosys netlist. The one failure is
  [#1510](https://github.com/DeanoC/fes/issues/1510), as on main.
- **Seal.** Quartus 17.0.2 meets every analysis. Setup slack is +1.025 ns,
  where slice 2's seal had +0.800, and hold slack is +0.111 ns. D's
  registers lost about 4 ns of their slack
  ([below](#timing)).

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
- **Unchanged.** Uncached loads still read only as the oldest uop, and M2's
  merge still takes only bytes that write RAM. I/O still runs at commit.
  MOVS's st2 stores were never held behind. Forwarding is not added. The
  self-modifying-code check is made at a store's commit, whatever loads do.

## Latency microbenchmarks

`scripts/test_fes_i386_latency.py`, clocks for 1,000 iterations of eight
pairs (before: main `b12419644`):

| Loop | Before | After |
| --- | ---: | ---: |
| `MOV [DI], AX` then `MOV CX, [SI]`, another line (`store_load_other`) | 26,059 | 18,059 |
| ... then `MOV CX, [DI + 4]`, the next dword (`store_load_line`, new) | 26,050 | 18,057 |
| ... then `MOV CL, [DI + 1]`, a byte it wrote (`store_load_part`, new) | 26,030 | 26,030 |
| ... then `MOV CX, [DI + 1000h]`, 4 KiB on (`store_load_alias`, new) | 26,059 | 26,059 |
| `MOV [DI], AX` then `MOV CX, [DI]` (`store_load_same`) | 26,059 | 26,059 |

Every other loop takes main's clocks exactly
([latency-report.json](latency-report.json)).
- **Placement.** The new loops run after the subroutines, behind a JMP that
  takes the three bytes of the old OUT and HLT. A 64-byte zero pad follows
  RETF.
- **Why it matters.** Without this, `near_return` and `far_return` move, and
  so do the wrong-path bytes fetched after RETF. CALL loops' clocks then
  change by up to 8,000 for no reason in the CPU.

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus) at
`a421fb060`:

| Part | RTL | Netlist (Yosys `238eac9a`) |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 31,999 of 32,000 | 31,999 of 32,000 |
| Protected-mode programs | 8,000 of 8,000 | 8,000 of 8,000 |
| Directed cases | 50 of 50 | 50 of 50 |
| Unreal, protected-mode, no-387 and memory cases | 7, 92, 1,933 and 27, all pass | same |
| Holds without latency | pass | pass |

The one failure is `random#7#1936`, a CALL rel past CS's limit
([#1510](https://github.com/DeanoC/fes/issues/1510)). It fails the same way
on main.

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
a store (`store_other`) are counted, not checked.

| Loads after a store, no latency | Count | Gap |
| --- | ---: | --- |
| Meets it in bits 11:0 (`store_load`) | 1,040 | 99.8% two cycles apart |
| Other (`store_other`) | 1,363 | 7.6% one cycle apart |

Most random loads after a store take their address register from the
instruction just before. That value is late at D, so they still wait.

## Timing

`make build-fes-pc386-quartus SEED=1` from a clean checkout of `a421fb060`
built package `a587e709…`, build ID `1ea83012…`
([seal-summary.json](seal-summary.json)):
- **Worst slack.** Setup +1.025 ns and hold +0.111 ns. Every analysis is met.
- **Size.** 31,556 ALMs, 262 M10K blocks and 38 DSP blocks. Slice 2's seal
  used 31,502 ALMs.

**D's slack.** D's `issue` now waits on the 12-bit sum of the load's address
operands, so the registers it enables have lost most of their margin. The
table is TimeQuest's worst setup slack into each group at the −40 °C corner
([stage-slack.json](stage-slack.json)). Slice 3's seal is the only one
before this with the same per-stage script.

| Registers | Slice 3 seal `0a16a224` | This seal |
| --- | ---: | ---: |
| D's EIP (`d_eip`) | +5.57 ns | +1.46 ns |
| The string copies (`r_src`, `r_dst`) | +5.3–5.4 ns | +1.6 ns |
| D's address forwarding (`dfwd`) | +5.86 ns | +1.91 ns |
| D's first-uop flag (`d_first`) | +8.27 ns | +2.59 ns |
| D's operand forwarding (`dops`) | +6.91 ns | +2.74 ns |

The worst path into `d_eip` starts at `dfwd`'s ready bits. The plan put the
early ALU (slice 6) in AG on the 4.7–5.4 ns D and AG had. That room is now
about 1.5 ns. One way back is to keep the 12-bit sum off `issue`'s path, for
example by registering part of it a cycle earlier; this slice does not try
that.

## Kit

Run `s1` used Kit A, core-only, on 6 November from 15:30 to 15:43
([kit-scores.json](kit-scores.json)).
- **Package.** `a587e709…`, the seal above.
- **Kit.** Baseline software `a8cd30c4` on image `f904961e`, with no install,
  no image change and no reboot. Kit A was restored, checked against its
  snapshot and logged FREE. The run's five FogCast data trees were removed
  after, and Kit A was logged FREE again at 15:43.
- **Disk.** The baseline's bench disk, `fe81c9c4…`, staged from Powerboat. It
  is not committed.
- **Commands.** Each was the baseline's, at max (preset 0), after a SCRUBX
  launch.

| Benchmark | Before (slice 2, k1) | After (s1) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,608 realtics, 37.5 fps | 3,863 in 3,566, 37.9 fps | +1.2% |
| 3DBench | 71.4 | 71.4 | 0 |
| PCPBench | 12.8 | 13.0 | +1.6% |
| TopBench | 303 | 307 | +1.3% |
| Pack DOOM, maximum detail | 2,134 in 2,218, 33.7 fps | 2,134 in 2,188, 34.1 fps | +1.4% |
| Pack DOOM, minimum detail | 2,134 in 805, 92.8 fps | 2,134 in 803, 93.0 fps | +0.2% |

The kit runner has no Wolfenstein 3D timing.

## Wolfenstein 3D

The profile record's Wolfenstein 3D window spends 59.7% of its clocks in one
seven-instruction loop at linear 1F9C6h. Two of its loads waited behind
stores, 6.3% of the window each, and the plan's 6–11% gain came from them.

A Bochs run of the same disk to 17 s gives the loop's bytes. It is the game's
`CalcTics` wait for the next timer tick, at CS:0CF6h:

```
mov eax, [TimeCount]
mov [bp-4], eax
mov ax, [bp-4]          ; reads the bytes just stored: waits
sub ax, [lasttimecount]
mov [tics], ax
cmp word [tics], 0      ; reads the bytes just stored: waits
jz  0CF6h
```

- **Both loads still wait.** Each reads exactly the bytes the store before
  it wrote, so they keep their hold.
- **Nothing to gain anyway.** The loop spins until the timer ticks, so a
  faster loop only spins more often.
- **The page-flip check.** The bench's Wolfenstein 3D window flips 70 pages a
  second, one a tick, which is the game's limit. So its page flips a second
  cannot show a CPU gain.

The bench windows (16.5–22 s, main's RTL and this slice's) are recorded below
as busy clocks a frame: the window's clocks less the loop's, over its frames.

<!-- WOLF -->

## Profile

<!-- DOOM -->
