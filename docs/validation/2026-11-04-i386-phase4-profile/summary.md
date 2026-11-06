# i386 Phase 4 profile, 4 November 2026

This record measures where `fes_i386_cpu`'s clocks go on the game code
Phase 4 is meant to speed up. It supports the plan in
[`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed).
It is host simulation of main `d646bcd7e` plus a Quartus timing rebuild. It
changes no RTL, and it is not a kit result.

**Result:** taken branches are the CPU's largest cost.
- **Cost per branch.** Branches resolve at EX and redirect from WB. In DOOM,
  the target of a taken Jcc, JMP rel, CALL rel or RET retires 11–12 clocks
  after it; an indirect CALL's 22 clocks after it, and those of far
  transfers, interrupts and deliveries 17–39.
- **DOOM.** In the `-timedemo demo3` window, redirects are 34% of all clocks
  (CPI 2.54, 0.87 of it redirects).
- **Other workloads.** Redirects are 37% in Wolfenstein 3D's play window and
  43% in 3DBench.
- **What prediction would remove.** About 29–33% of DOOM's clocks, taking
  Kit B's 26.7 fps to about 38–40.
- **Next-largest costs.** The store-to-load wait (11% in Wolfenstein 3D),
  L1D misses to DDR (10% of DOOM's bench clocks, which is 6–7% at the kit's
  clock) and the AGI waits (6% in DOOM).

## Method

- **Bench.** `machine_tb --cpu rtl` (the pc386 machine with `fes_i386_cpu`),
  governor `max`, built from main `d646bcd7e` with the profile hook in
  [profile-hook.patch](profile-hook.patch). Plan slice 1 landed the hook as
  `machine_tb --cpu-profile` ([baseline record](../2026-11-05-i386-phase4-baseline/summary.md));
  the patch stays as the record of what this profile ran.
  - **Signal access.** The hook reads about 40 of `fes_i386_cpu`'s internal
    signals through a Verilator config file (`public_flat_rd`), so the RTL is
    unchanged.
  - **Clock buckets.** Every system clock goes in one bucket at D, in
    priority order: issued (an instruction's first uop or a later one),
    halted, frozen (L1D wait, MMIO, multiply or divide, I/O, INTA, store
    buffer full), front end empty (refill after a redirect, L1I miss,
    other), then the hazard that held D (AGI, segment, store-to-load, flags
    after a shift, ESP, temp register, microcode ROM, string, I/O
    serialisation).
  - **Wrong path and branch costs.** Wrong-path uops are issued uops less WB
    commits. Each retirement's clocks since the previous one go to its
    instruction class. A taken instruction's redirect is the gap to its
    target's retirement ("next").
  - **Frames.** Frames count byte writes to CRTC start-address register 0Ch at
    3D5h, including byte lanes from multi-byte I/O (a word OUT at 3D4h selects
    the register and writes its data), as both games page-flip that way.
- **Handover.** The bench is slow, about 12 wall minutes per simulated
  second on the RTL. So Bochs runs each workload up to its window and the
  RTL takes over: DOOM at its 40th page flip, the others at a set time.
  - **Cold caches.** The RTL starts with empty caches.
  - **Window start.** Each window starts at the profile dump after the
    handover. The hook charges the first retirement after a handover its
    whole gap from time 0, so the window must not include it.
- **Kit-equivalent clocks.** The bench clocks the CPU at 100 MHz, and port
  0's first data takes 18 of those clocks (`pc386_ddr_model.h`). At the
  kit's 61.44 MHz it takes 11. So kit-equivalent clocks charge each L1D and
  L1I miss 7 clocks fewer.
- **Disks.** Built outside the repo; no game or benchmark bytes are in this
  record.
  - DOOM: the games image `b23fba2e…` plus `DOOM (1993).zip` `06ee36b4…`, as
    in the games smoke record; disk `7d98bffd…`.
  - Wolfenstein 3D: the game suite's disk (`sim_fes_pc386.game_disk`),
    `1d2c9314…`, zip `f451d853…`.
  - Benchmarks: the games image plus PhilsComputerLab's DOS Benchmark Pack
    v1.5's 3DBENCH, 3DBENCH2, PCPBENCH, TOPBENCH and DOOMS folders, from
    Deano's share, with one batch file each; disk `5c585ebc…`. The pack is
    not committed. Executable SHA-256s:
    - 3DBENCH.EXE `ae644264…`
    - PCPBENCH.EXE `b68200dd…`
    - TOPBENCH.EXE `4428fc27…`
    - DOOMS DOOM.EXE `b8020523…`
    - DOOM1.WAD `1d7d43be…`
    - MAX.CFG `6e0678ef…`

## Windows

| Window | Simulated seconds | Retired | Frames | CPI | Bench clocks a frame | Kit-equivalent fps |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| DOOM v1.9 `demo3` | 22.726–32.226 | 373,411,347 | 433 | 2.544 | 2,193,995 | 29.1 |
| DOOM shareware, Pack MAX.CFG | 11.752–21.252 | 366,179,500 | 315 | 2.594 | 3,015,873 | 21.2 |
| Wolfenstein 3D play | 17.0–30.0 | 410,163,921 | 912 | 3.169 | 1,425,439 | 43.8 |
| 3DBench | 4.5–8.0 | 107,438,785 | — | 3.258 | — | — |
| PCPBench, before its first frame | 4.5–14.5 | 279,488,121 | — | 3.578 | — | — |
| TopBench `-l` | 4.512–8.512 | 16,606,653 | — | 24.1 | — | — |

- **DOOM against the kit.** Kit B's whole `demo3` averaged 2.30M clocks a
  frame (3863 gametics in 5062 realtics, 26.7 fps). The window's 433 frames
  are about a ninth of the demo and cost 2.11M kit-equivalent clocks each,
  8% fewer.
- **Wolfenstein 3D.** The window stands in E1L1 facing a door and fires; a
  Bochs-only run with frames shows that view at 15 s and 20.5 s. 912 frames
  in 13 simulated seconds are 70 a second at the bench's clock, or 44 at the
  kit's; Kit B's capture tops out at 30.
- **3DBench.** It waits for a key at its title. The run presses Enter at
  3.8 s; the window is its fly-through.
- **PCPBench.** It ran 10 simulated seconds on the RTL without drawing a
  frame, in set-up code that is 42% CALL and RET redirects and 11% multiply
  and divide. Under Bochs, which runs faster in simulated time, it draws
  frames from 6 s.
- **TopBench.** It halts for 66% of its window and waits for VGA memory for
  14%. Its score of 461 is at the bench's 100 MHz clock and mostly measures
  timer waits and video memory. Its database lists 486DX2-66 machines at
  170–190.

[profile.json](profile.json) has every window's counters, buckets, classes
and twelve hottest sites (linear address, class, retirements, clocks).

## Where the clocks go

| Share of clocks | DOOM `demo3` | DOOM shareware | Wolfenstein 3D | 3DBench | PCPBench set-up |
| --- | ---: | ---: | ---: | ---: | ---: |
| Uops that retire | 40.6% | 39.7% | 32.9% | 37.0% | 30.8% |
| Redirect refill | 18.6% | 18.1% | 20.6% | 23.3% | 29.0% |
| Wrong-path uops | 15.6% | 15.6% | 16.0% | 19.7% | 26.2% |
| L1D miss to port 0 (bench clocks) | 10.4% | 9.3% | 2.1% | 5.0% | 0.0% |
| AGI | 5.9% | 6.5% | 0.9% | 3.5% | 0.7% |
| Multiply and divide | 2.5% | 2.6% | 1.3% | 3.2% | 10.7% |
| Store-to-load wait | 2.1% | 2.1% | 11.1% | 1.3% | 0.7% |
| Store buffer full | 0.1% | 0.1% | 9.1% | 1.6% | 0.0% |
| I/O | 1.2% | 0.7% | 3.8% | 0.2% | 0.0% |

**Taken branches by class (DOOM `demo3`):**

| Class | Retired | Taken | Clocks to the target | Clocks per taken |
| --- | ---: | ---: | ---: | ---: |
| Jcc | 35,824,313 | 25,605,538 | 310,646,203 | 12.13 |
| RET | 2,634,888 | 2,634,888 | 31,668,078 | 12.02 |
| CALL rel | 2,178,539 | 2,178,539 | 27,090,790 | 12.44 |
| JMP rel | 1,846,406 | 1,836,499 | 22,720,279 | 12.37 |
| CALL r/m | 465,534 | 465,534 | 10,282,041 | 22.09 |
| Far CALL, far JMP, IRET, RETF, INT and deliveries | 42,584 | 40,744 | 1,147,600 | 17–39 |

- **DOOM.** The column drawer's loop-back branch is the hottest site: 10.1%
  of the window's clocks, at 11 clocks each.
- **Wolfenstein 3D.**
  - One seven-instruction loop is 59.7% of the window. Its loop-back branch
    is 34.5%; two of its loads wait behind stores, 6.3% each.
  - Redirects after far CALL and RETF are 2.2% of the window, at 19.8 and
    11.8 clocks per taken.
  - The store buffer fills behind VGA writes, a machine bandwidth limit.
- **3DBench.** LOOP is 3.1M of 12.1M taken branches.

## Estimates

The [design](../../../sources/misteross/docs/i386-cpu-design.md#estimated-gains)
gives each feature's gain on each window, with these assumptions:
- **Predicted branch.** A correctly predicted taken branch costs its
  target's average clocks plus one bubble: 1.60 in DOOM, 1.97 in
  Wolfenstein 3D.
- **Accuracy.** Jcc, LOOP and JCXZ are 85–95% right, direct JMP and CALL and
  RET 95–99%, indirect 70–90% and far 90–98%. 5% of not-taken conditionals
  are predicted taken and pay a full redirect plus the predicted bubble over
  a fall-through: the class's clocks to the target plus one.
- **Store-to-load.** The overlap check removes 50–100% of that wait.
- **Early ALU.** It removes 30–80% of AGI. The profile does not yet split
  AGI between load producers, which the early ALU cannot help, and ALU
  producers.
- **L2.** It hits 50–80% of L1D misses at 7 clocks.
- **Divider.** A faster divider removes 30–60% of DIV's clocks beyond one
  each. The DSP multiplier stays as it is, so multiply waits are not counted.
- **Frame rates.** These scale Kit B's 26.7 fps by the window's
  kit-equivalent clocks.
- **Far branches.** The classes count both modes, as does the estimator in
  [profile-hook.patch](profile-hook.patch). Wolfenstein 3D and 3DBench
  retire nothing with PE set; DOOM's far branches are DOS/4GW's in
  protected mode, so none of them is real-mode far prediction's.

| Feature | DOOM `demo3` | DOOM fps |
| --- | ---: | ---: |
| BTB: Jcc, LOOP and JCXZ | 22.0–24.6% | 34.2–35.4 |
| BTB: JMP and CALL rel | 4.1–4.3% | 27.8–27.9 |
| Return stack | 2.6–2.7% | 27.4 |
| BTB: indirect | 0.7–0.9% | 26.9 |
| Real-mode far | 0% (its far branches, 0.1%, are protected mode) | 26.7 |
| All prediction (real-mode far excluded) | 29.3–32.5% | 37.8–39.6 |
| Store-to-load wait | 1.1–2.1% | 27.0–27.3 |
| Early ALU | 1.8–4.9% | 27.2–28.1 |
| L2 | 3.1–5.0% | 27.6–28.1 |
| Divider | 0.6–1.2% | 26.9–27.0 |

## Timing

A Quartus 17.0.2 rebuild (`make build-fes-pc386-quartus SEED=1`) of main
`d646bcd7e` passes every analysis. Its 109 sealed inputs, toolchain and seed
match Kit B's package `499f4805`, but it is not that package.
- **Not bit-stable.** The build ID is `c899e920…` against `61d5f899…`, and
  the worst slacks differ.
- **Worst slacks.** Setup +0.854 ns, on the pixel clock; hold +0.081 ns. The
  system clock's worst is +1.221 ns at the −40 °C corner, in the
  governor's ISA charge.
- **Size.** 29,119 ALMs, 259 M10K, 36 DSP.

TimeQuest's worst setup slack into each CPU stage's registers, at the −40 °C
corner and a 16.276 ns period:

| Registers | Slack | From |
| --- | ---: | --- |
| `cpu_decode_by` (DC) | +2.402 ns | Q's `align_start` |
| `cpu_l1i_valid` | +3.068 ns | `cpu_l1i_blk` |
| `cpu_frontend_align_acc` (Q) | +3.202 ns | Q's `align_start` |
| `cpu_seg_lim` | +3.310 ns | |
| `cpu_biu_i_line` | +3.437 ns | `cpu_l1i_blk` |
| `cpu_l1d_stall` | +3.736 ns | |
| `cpu_dc0` | +3.924 ns | `cpu_decode_by` |
| `cpu_wb_s` | +4.322 ns | `cpu_smc_lo` |
| `cpu_m2_hit_w` | +4.561 ns | |
| `cpu_ag_row` | +4.731 ns | |
| `cpu_muldiv_q1_over` | +4.943 ns | |
| `cpu_r_src`, `cpu_r_dst`, `cpu_d_eip` | +5.036–5.418 ns | |
| `cpu_dfwd_src`, `cpu_dops_in_ag` | +5.411–6.420 ns | |
| `cpu_ex_md_flags` | +6.594 ns | |
| `cpu_m1_s` | +6.921 ns | |

## Not done

- **Full benchmark scores.** 3DBench's and PCPBench's whole-run scores on
  the RTL take more than a day of bench time each. Plan slice 1 records them
  on Kit B instead, with TopBench and the Pack's DOOM timedemos.
- **Kit runs.** None were needed; DOOM's kit figure is the games smoke
  record's.
- **AGI by producer.** The profile does not split AGI by producer. Plan
  slice 6 starts with that split.

## Corrections

- **6 November: the false-taken charge.** The estimator in
  [profile-hook.patch](profile-hook.patch) charged each falsely predicted
  not-taken conditional `per - 1` clocks, where `per` is the class's clocks to
  the target. A full redirect after the predicted bubble costs `per + 1` over a
  fall-through, and the patch now charges that (#1491). Rerun on
  [profile.json](profile.json)'s windows, the Jcc row moves 0.1–0.2 points:
  DOOM `demo3` 22.1–24.7% to 22.0–24.6% (34.3–35.5 fps to 34.2–35.4), all
  prediction 29.5–32.6% to 29.3–32.5% (37.8–39.6 fps unchanged), 3DBench's
  Jcc row 32.4–36.4% to 32.2–36.2%. Wolfenstein 3D's rows are unchanged at
  this precision. Slice 2's BTB measured 37.5 fps on Kit A against its
  projected 36–38 ([BTB record](../2026-11-06-i386-phase4-btb/summary.md)).
  Slice 3's return stack, in open PR #1512's record, takes Kit A's `demo3`
  from 37.5 to 38.7 fps (+3.3%). Its 2.6–2.7% of the baseline's clocks is
  about 3.6–3.8% of the clocks left after the BTB, so that is in line with the
  estimate too.
