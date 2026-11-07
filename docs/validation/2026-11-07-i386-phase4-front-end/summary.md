# i386 Phase 4: why the front end leaves D empty, 7 November 2026

This record belongs to CPU Phase 4
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). Slices 6 and
6b ([early ALU](../2026-11-06-i386-phase4-early-alu/summary.md),
[load forwarding](../2026-11-06-i386-phase4-load-fwd/summary.md)) found that
in DOOM's bench window, clocks they saved in D came back as `fe_empty`: D
can now issue faster than the front end delivers. This record splits every
clock in which D finds DC empty by its cause in the front end, and plans the
next slices from that split. It changes no CPU behaviour.

**Tree.** Main `74e419cb3`, which already has #1522 (slice 6b), plus this
branch's profiler changes. The runs used that tree.

**Result.**
- **Sequential fetch keeps up.** In DOOM, with no L1I miss involved, Q
  waited on a sequential block for 280 clocks in 50M. Prefixes cost 948.
  The jump-clear bubble cost none, and so did instructions that run past a
  half (all such waits came with an L1I miss).
- **Redirect refill is the largest share.** It is 1.72M of DOOM's 3.11M
  front-end-empty clocks, about 6.2 per redirect. In 3DBench it is 28.5M of
  34.4M. The fix is fewer redirects, not a faster fetch: an indirect CALL in
  DOOM costs 23 clocks, and 3DBench mispredicts 5.0M direct branches
  (accuracy 85.5%).
- **The target's bytes 12–15 are the front end's own limit.** The
  `fe_empty` that soaked up slice 6's saving is mostly one wait: the
  instruction at a predicted target starts at bytes 12–15 of its block. The
  decode of those bytes needs the next block's pass through L1. That wait
  is 350k of DOOM's 654k `fe_empty` clocks, plus 70k more for loops inside
  one block.
- **L1I misses** account for the rest: 1.34M bench clocks in DOOM (0.45M
  kit-equivalent).

Top three slices, by kit-equivalent clocks saved in DOOM's window
(22.726–23.226 s, 47.39M kit-equivalent clocks):

| Rank | Slice | DOOM saving | 3DBench saving | Risk |
| --- | --- | ---: | ---: | --- |
| 1 | Indirect CALL and JMP prediction (tracker slice 5) | 0.62–0.80M (1.30–1.68%) | 0.64–0.82M (0.19–0.25%) | Low |
| 2 | A direction predictor with global history | 0.51–0.85M (1.08–1.79%), not yet sized | 13.6–22.6M (4.1–6.8%), not yet sized | Medium |
| 3 | Decode a target's bytes 12–15 in one pass | 0.20–0.39M (0.41–0.83%) | 1.62–3.25M (0.49–0.98%) | Medium |

At Kit A's 39.8 fps, each 1% of DOOM's clocks is about 0.4 fps.

## The split

`machine_tb --cpu-profile` now charges each clock in `refill`,
`refill_l1i`, `fe_l1i` and `fe_empty` to why Q did not issue an
instruction two clocks earlier
([`cpu_profile.h`](../../../sources/misteross/cores/fes-pc386/sim/cpu_profile.h),
"Front-end split"). Q's issue loads its output register on one edge, and
DC loads from that register on the next. So D is empty at clock t only if
Q, with room in its output register, issued nothing at t − 2, or a
redirect emptied Q and DC.

Q's reasons, read from `fes_i386_qalign` and `fes_i386_fetchbuf`, are:
- **prefix:** Q consumed a prefix. Every prefix but the last costs a cycle.
- **jclr:** a taken jump that started in the half before its last byte's
  half cleared `avail`, and Q's bytes are otherwise present.
- **redirect:** a redirect at WB. The clock after the redirect is charged
  to it too.
- **half:** Q's half has not been filled.
- **tail:** Q is at bytes 12–15 of its block, and their decode waits for
  the next block to pass through L1.
- **cross:** the instruction runs past its half, and the next half has not
  arrived.

Each wait is split by how Q entered the half it is in:
- `seq`: in sequence;
- `seq1`: the first half in sequence after a jump or redirect;
- `tk`: a predicted taken jump;
- `tkself`: a predicted jump whose target is in the branch's own block;
- `rd`: a redirect.

A wait is also marked `l1i` if F2 waited on the L1I since Q last issued.
The `latent` column counts every clock Q did not issue, whatever D was
doing. These are the bubbles D's own waits hide today.

`tkself` comes from one bench-only wire, `fes_i386_qalign prof_tself`. It
is an `assign` that nothing in the CPU reads, so synthesis removes it.
Everything else is read through `cpu_profile.vlt`.

**Checks.**
- **Inert.** The three windows' buckets are identical, clock for clock, to
  #1522's record. DOOM's `fe_empty` is 653,999 in both.
- **Offset.** In DOOM's second half-second, every front-end-empty clock has
  a reason. Two clocks in the first half-second have none: the handover's.
  The `cpu-profile` suite now fails if more than 0.1% have none. Its first
  run found 61,632, one per redirect: the clock after a redirect, when Q's
  output from before the redirect is gone. That clock is now charged to the
  redirect, and the suite then had 2 of 338,904 unattributed.
- **Tests.** `tests/test_pc386_cpu_profile.py` drives each reason, the
  two-clock offset, the redirect case and the L1I mark through the header.
  The Python side must find each bucket's split equal to the bucket.

## DOOM, 22.726–23.226 s

50,000,000 clocks, 47,388,153 kit-equivalent (each L1 miss costs 7 fewer
clocks on the kit), 276,312 redirects, 127,482 L1I misses, 2,710,794
predicted jumps taken at Q (195,945 inside one block).
[doom-window-h2.json](doom-window-h2.json).

| Reason | Clocks | refill | refill_l1i | fe_l1i | fe_empty | of which l1i | latent |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| half_rd | 1,088,137 | 719,424 | 368,713 | 0 | 0 | 341,178 | 1,088,479 |
| redirect | 552,624 | 532,074 | 20,550 | 0 | 0 | 0 | 276,312 |
| half_seq1 | 493,538 | 0 | 0 | 371,366 | 122,172 | 493,534 | 499,684 |
| tail_tk | 454,316 | 0 | 0 | 104,381 | 349,935 | 132,661 | 502,315 |
| tail_rd | 121,132 | 44,900 | 28,030 | 39,391 | 8,811 | 74,882 | 149,479 |
| tail_seq | 111,959 | 0 | 0 | 82,750 | 29,209 | 111,797 | 133,307 |
| cross_seq | 89,924 | 0 | 0 | 65,911 | 24,013 | 89,915 | 361,211 |
| tail_tkself | 72,045 | 0 | 0 | 2,158 | 69,887 | 2,577 | 75,056 |
| half_tk | 53,405 | 0 | 0 | 36,941 | 16,464 | 41,231 | 119,039 |
| tail_seq1 | 34,187 | 0 | 0 | 28,656 | 5,531 | 34,145 | 61,512 |
| half_tkself | 25,303 | 0 | 0 | 73 | 25,230 | 30 | 25,574 |
| half_seq | 7,413 | 0 | 0 | 6,334 | 1,079 | 7,350 | 12,012 |
| cross_rd | 6,691 | 0 | 3,454 | 2,824 | 413 | 6,691 | 8,514 |
| cross_seq1 | 1,299 | 0 | 0 | 1,027 | 272 | 1,299 | 2,265 |
| cross_tk | 1,088 | 0 | 0 | 830 | 258 | 1,088 | 2,232 |
| prefix | 948 | 30 | 7 | 186 | 725 | 0 | 1,520 |
| Total | 3,114,009 | 1,296,428 | 420,754 | 742,828 | 653,999 | 1,338,378 | |

How to read the table:
- **Refill.** A WB redirect costs about 6.2 empty clocks:
  - 2 while the redirect empties Q and DC;
  - about 2.7 while the target block passes F1, F2, L1 and L2 into the
    buffer;
  - about 1.5 for L1I misses on the way and a target's bytes 12–15.
- **`fe_empty`.** Of the 654k clocks Q left D empty with no L1I miss
  involved, 350k + 70k are `tail` waits at a predicted target, 25k are
  `half` waits in a loop inside one block, and the 122k `half_seq1` clocks
  are the drain after an L1I miss. `tail_tk` costs 0.17 clocks per predicted
  jump.
- **Fetch keeps up.**
  - `half_tk` (53k in all, 12k without an L1I miss) shows the predicted
    target's block reaches the buffer in time.
  - The sequential waits (`half_seq`, `tail_seq`, `cross_seq` and the
    `seq1` rows) are 738k clocks, and all but 280 of them come with an L1I
    miss.
- **Latent bubbles.** The latent count is close to the realised count for
  every row but `cross_seq` (361k latent, 90k realised) and `half_tk` (119k
  against 53k): D's own waits still hide those two. The `tail_tk` bubbles
  that slices 6 and 6b uncovered are now almost all realised (502k latent,
  454k realised).

The first half-second (22.226–22.726 s,
[doom-window-h1.json](doom-window-h1.json)) has the same shape. It has
fewer redirects (164,275), more predicted jumps (9.0M), and `tail_tk` at
590,350 clocks (514k of them in `fe_empty`).

## 3DBench, 4.5–8.0 s

350,000,000 clocks, 330,895,565 kit-equivalent, 5,247,292 redirects.
[3dbench-window.json](3dbench-window.json).

| Reason | Clocks | refill + refill_l1i | fe_l1i + fe_empty | of which l1i | latent |
| --- | ---: | ---: | ---: | ---: | ---: |
| half_rd | 16,651,318 | 16,651,313 | 5 | 1,367,011 | 16,662,215 |
| redirect | 10,494,390 | 10,494,390 | 0 | 0 | 5,247,292 |
| tail_tk | 2,273,981 | 0 | 2,273,981 | 247,531 | 2,757,644 |
| tail_rd | 1,808,206 | 1,327,546 | 480,660 | 391,533 | 1,825,870 |
| tail_tkself | 1,175,943 | 0 | 1,175,943 | 45,938 | 1,286,716 |
| half_tk | 881,807 | 0 | 881,807 | 304,177 | 1,148,845 |
| half_tkself | 618,010 | 0 | 618,010 | 1,456 | 625,438 |
| tail_seq1 | 272,842 | 0 | 272,842 | 271,598 | 369,452 |
| tail_seq | 251,057 | 0 | 251,057 | 249,691 | 317,564 |
| other | 2,987 | 0 | 2,987 | 2,987 | 17,010 |

- **Refill** is 8.6% of the kit-equivalent clocks, because 3DBench's
  direct branches predict at only 85.5%: 5,018,590 mispredicted of
  34,676,108. A Jcc's taken target costs 6.93 clocks on average, against
  2.38 for a CALL rel.
- **Loops inside one block** are 10.5M of the 26.4M predicted jumps.
  Their `tail` and `half` waits are 1.79M clocks.
- **No prefix cycles.** 3DBench has none, and no `cross` waits outside an
  L1I miss.

## Causes the task listed that do not cost clocks here

- **A uop sequence.** While DC issues an instruction's uops, Q fills
  `dc_skid` and its own output register and then holds (`held`), so a
  sequence never empties D. Its own waits are the `microcode` cause.
- **Self-modifying code and replay.** These restart through a WB redirect,
  so they are in `refill`. There were no SMC restarts or fetch-check
  replays in these windows.
- **An instruction longer than the fetch width.** The buffer is 32 bytes
  and an instruction at most 15, so this is the `cross` wait. Without an
  L1I miss it cost DOOM 9 clocks.
- **Prefix and ModRM/SIB length decode.** Prefixes cost 948 clocks in
  DOOM and none in 3DBench. ModRM and SIB are decoded in L1 with no cycle
  of their own. The decoders' four-byte look-ahead is the `tail` wait.

## Plan

How the savings are worked out:
- Clocks are kit-equivalent, against the window's kit-equivalent total.
- Bubbles not involving an L1I miss convert 1:1.
- Only the L1I candidate takes the 7-clock-per-miss correction.
- Ranges are a low and high recovery of the measured clocks, and each row
  states its basis.
- DOOM is the second half-second unless noted.

### 1. Indirect CALL and JMP prediction (tracker slice 5)

- **Measured.**
  - DOOM: 42,841 `CALL r/m` at 23.05 clocks each (987k clocks), against
    2.44 for a predicted `CALL rel`.
  - 3DBench: 73,655 at 14.81.
- **Basis.** Six call sites carry 96% of DOOM's indirect calls in the
  window. The 70–90% below assumes each mostly repeats its last target. The
  profile does not measure that, so the slice's first step should count
  target repeats per site on the bench.
- **Saving.** 70–90% of them predicted at the CALL rel cost:
  - DOOM: 0.62–0.80M (1.30–1.68%). The first half-second gives 0.36–0.47M
    (0.75–0.97%).
  - 3DBench: 0.64–0.82M (0.19–0.25%).
- **RTL.**
  - The BTB already keeps a target per entry. WB allocates `CALL r/m` and
    `JMP r/m`, and F2 predicts their last target as it does a `CALL rel`
    (pushing the return stack).
  - D gives the next instruction the target's EIP as it does after a RET
    (`rpt`).
  - EX compares it with the computed target, as for a RET, and redirects on
    a difference.
- **Risk: low.** The compare is the RET compare's twin at EX, which has
  +5.5 ns slack. The BTB entry needs one more type bit. The ALM cost is
  well under 1%.

### 2. A direction predictor with global history

- **Measured.**
  - 3DBench: 5,018,590 mispredicted direct branches (85.5% accurate).
  - DOOM: 188,974 (94.1%).
  - A mispredict redirects from WB and costs about 10 clocks (design,
    "Branch prediction"). This analysis counts 9 saved for each one avoided,
    since a predicted taken branch still costs about 1.
- **Saving, not yet sized.** If a global-history predictor removes 30–50%
  of the mispredicts:
  - DOOM: 0.51–0.85M (1.08–1.79%).
  - 3DBench: 13.6–22.6M (4.1–6.8%).

  The 30–50% is an assumption, not a measurement. The slice's first step
  should model a gshare table on the retired branch stream in
  `cpu_profile.h`, bench-only like this split, to replace it with a number.
- **RTL.** A pattern table of 2-bit counters in an M10K, indexed by fetch
  block and a global history register, read in F1 beside the BTB. The BTB
  keeps targets and the table gives the direction. On a redirect, WB
  restores the history as it restores the return stack's pointer.
- **Risk: medium.**
  - The read sits beside the BTB's, whose slack is +3.71 ns into
    `pred_block` and +4.29 ns into `btb_enter`.
  - It adds one M10K and a few hundred ALMs.
  - The history repair on a redirect follows the return stack's pattern.

### 3. Decode a target's bytes 12–15 in one pass

- **Measured.** Waits at bytes 12–15 of a block that Q entered by a
  predicted jump or a redirect, with no L1I miss:
  - DOOM: 391k clocks (0.83%), and 528k (1.10%) in the first half-second;
  - 3DBench: 3.25M (0.98%).

  These are most of the `fe_empty` slices 6 and 6b uncovered.
- **Saving.** Half to all of those clocks:
  - DOOM: 0.20–0.39M (0.41–0.83%);
  - 3DBench: 1.62–3.25M (0.49–0.98%).

  Half is the low end because the simplest form, the first option below,
  fixes only a block in the first half of its 32-byte L1I line.
- **RTL.** Bytes 12–15 are decoded with the next block because their
  decoders read four bytes ahead
  ([Length decode and alignment](../../../sources/misteross/docs/i386-cpu-design.md#length-decode-and-alignment)).
  Two options:
  - F2 also returns bytes 16–19 of the block's 32-byte line (the line's
    other 128-bit word) when the block is the line's first half. L1 then
    decodes all 16 bytes in one pass.
  - When F2 passes a predicted target, it also loads the next block on the
    next edge. This removes the one-cycle `pred_go` drop for that block, so
    the tail's decode lands with the target's bytes 0–11.

  Either way, `fes_i386_lendec` grows from 16 to 20 decoders and the
  buffer's slot layout for bytes 12–15 changes in `fes_i386_fetchbuf` and
  `fes_i386_lstage`.
- **Risk: medium.**
  - L1 and L2 are not the front end's tightest paths. The fetch group has
    +2.60 ns (`align start` into `fetch tail_ok`), and decode +2.09 ns.
  - Q's loop (+1.64 ns, `align start` to `start`) is not touched.
  - The extra decoders and wider L1I read cost about 1–2k ALMs.

### Further candidates, ranked

4. **Resolve a mispredicted branch from EX, not WB.**
   - **Saving.** One clock per redirect: DOOM 276k (0.58%), 3DBench 5.25M
     (1.59%).
   - **RTL.** EX drives F1's redirect for a branch when nothing older can
     fault. WB still restores the return stack and history.
   - **Risk: medium–high.** It adds a new EX-to-F1 path into the L1I's
     address registers, and an ordering rule against older faults.
5. **L1I next-line prefetch on a miss.**
   - **Measured.** The L1I-marked waits are 1.34M bench clocks in DOOM, and
     0.45M kit-equivalent after the 7-clock correction per miss.
   - **Saving.** 30–60% of that: 0.13–0.27M (0.28–0.56%). 3DBench: 0.2–0.4M
     (0.06–0.12%).
   - **Risk: medium.** The bus interface's port 0 is shared with the L1D,
     and the L1I fills a second line.
6. **Run a loop inside one block from the buffer.**
   - **Measured.** Waits after a jump to the branch's own block, with no
     L1I miss: DOOM 95k (0.20%), 3DBench 1.75M (0.53%). These overlap
     candidate 3's `tail_tkself` share.
   - **RTL.** Q stays in the half and the fetch buffer neither refetches nor
     refills it.
   - **Risk: high.** It changes Q's loop, the front end's tightest path.

Not worth a slice on these windows: prefix cycles, the jump-clear bubble,
cross-half waits without an L1I miss, and sequential fetch bandwidth.

## Commands

```bash
python3 scripts/sim_fes_pc386.py --suite cpu-profile --download
python3 -m unittest tests.test_pc386_cpu_profile
machine_tb --bios pc386-bios.bin --vgabios pc386-vgabios.bin --disk doom.img --cpu rtl --cpu-profile cpu.prof \
  --limit-seconds 300 --type 'cd doom|doom -timedemo demo3|' --rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5
machine_tb --bios pc386-bios.bin --vgabios pc386-vgabios.bin --disk bench.img --cpu rtl --cpu-profile cpu.prof \
  --limit-seconds 8.0 --type 'b3d|' --keys '3.8:|' --rtl-from 4 --cpu-profile-every 0.5
python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.726049 --to 23.226049 --json doom-window-h2.json
python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.226049 --to 22.726049 --json doom-window-h1.json
python3 scripts/pc386_cpu_profile.py cpu.prof --from 4.5 --to 8.0 --json 3dbench-window.json
```

The `unittest` and `cpu-profile` lines run from `sources/misteross`.
The disks hold commercial software and are not committed. No kit run: this
slice changes no CPU behaviour, and the bench buckets match #1522's.
