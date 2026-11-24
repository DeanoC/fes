# i486DX cuts, round 2: the measurement method, committed before any fit or simulation

Committed 23 November 2026, before any fit, lockstep run or profile window of
this round was queued. The only run before this commit is an oracle-only
3DBench calibration (section C2), which runs no variant and measures no CPI. Later amendments are dated and say
what had been seen by then. No band, window or rule is changed after a
result exists.

This record measures; it changes no RTL. The patches under
[`patches/`](patches/) are scratch ablations, written by
[`patches/make_patches.py`](patches/make_patches.py) against main
`dafabb6db`. They are never merged, and nothing here is a seal.

## The question

The plan record, `docs/validation/2026-11-22-i486dx-core-plan/` (PR #2146,
tracker #2135), left the DX about 900 ALMs over the Agilex 5 device after the
measured shell cuts and dropping load forwarding. Deano, 23 November 2026
(relayed): the DX passes when it fits the device and closes timing on four
seeds ("Device + 4-seed timing"), and the next round starts now ("Start
now"): 8 KiB L1s (and a unified 8 KiB), a narrower front end, the x87
candidates and a slower divider, measured in ALMs and DOOM, Quake and 3DBench
CPI, with no RTL merged.

## What each approved candidate became

Each candidate was read in the RTL at `dafabb6db` before this method was
written. Only some have a small, functionally correct ablation. The others
need a design, and this round does not invent one: whether to write scratch
redesigns is Deano's call.

| Candidate | This round | Why (RTL at `dafabb6db`) |
| --- | --- | --- |
| L1I at 8 KiB | **`l1i2`**: 2-way, 128 sets, 32-byte lines | `fes_i386_l1i.sv` is 16 KiB 4-way with no size parameter. Fills choose ways 0–1 only (the free-way scan at lines 232–236 limited to ways 1..0, round robin `{0, rr[0]}`), and ways 2–3's valid bits are held at 0 (line 267), so their arrays, tag compares and hit terms have no reader. |
| L1D at 8 KiB | **`l1d2`**: the same | `fes_i386_l1d.sv` is 16 KiB 4-way, write-through, no write-allocate, no dirty bits: the same two edits (lines 372–376, 653). The walker, DMA and FLUSH clear whole sets, never a way. |
| Both L1s at 8 KiB | **`l12`** = `l1i2` + `l1d2` | |
| Unified 8 KiB L1 | **No ablation: needs a design.** | One array shared by fetch and data needs an arbiter and a merged fill and snoop path; no edit of the two caches gives it. `l12` bounds its storage side from below: two 8 KiB caches hold 16 KiB, a unified cache 8. |
| Narrower front end | **No area ablation: needs a design.** `qhalf` measures CPI only. | Q (`fes_i386_qalign.sv`) can start at any of 32 byte positions, so `fes_i386_lendec`/`lstage` precompute a length for every byte under four prefix variants; that per-byte decode is the design, not a width setting. Fewer decoders or variants give wrong lengths; one buffer half breaks instructions that cross a block. Fetch already uses one port, and Q already issues one instruction a clock. **`qhalf`** lets Q advance at most once every two clocks (lines 111–112): a bound on what front-end throughput is worth in CPI. It saves no area and is never fitted. |
| Slower divider | **No candidate.** | `fes_i386_muldiv.sv`'s divide is already radix-2 restoring, one quotient bit a clock (DIV r/m32 36 clocks in the unit), after the 386EX microcode. The only spare logic, a 32-bit compare at step 2 (lines 307–309), carries the 386EX's #DE and flags behaviour. The area lever in that unit is its multiplier (four 18×18 DSP products, five clocks), and an iterative one is a design, because the flags depend on its stages. The x87 does not share this divider. |
| x87: de-pipeline the slow lane | **No candidate.** `xdisp` instead. | `fes_x87_slow` is already one iterative op at a time (DIV and SQRT on one subtractor), and the fast lane is already one stage (#2059, `PIPELINED=0`). The slow lane is not a superset of the fast lane. The core's only remaining pipeline is its three-register front end. **`xdisp`** removes the first register (`dispatch`), `fes_x87_core.sv`. |
| x87: iterative multiply on fewer DSPs | **`xshift`** for the envelope's multiplier; the fast lane's FMUL needs a design. | The envelope's `fes_i386_x87_mul` (nine 22×22 DSP products) has one caller, S_SHIFT, whose `b` is `64'd1 << sh_pow` (`fes_i386_x87.sv:1617`, the only assignment of `ub_mul`): it is a shifter. **`xshift`** makes it a serial shifter (a byte or a bit a clock, at most 14 clocks against 4). The fast lane's FMUL (`fes_x87_fast.sv`) made iterative is a design. |
| x87: serial conversions through the engine | **No ablation: needs a design.** | FILD, FIST, FBSTP and the memory-format conversions run through the envelope's `clz64`, rounding select and BCD adjust; moving them onto the engine needs microcode the engine's 512×40 ROM does not hold (#2059). |
| x87: state into memory | **Already done.** | The register stack `rf_a`/`rf_b` is in M20K with no ALMs (`fes_i386_x87.sv:245–261`; the plan record's hierarchy fit). There is nothing for an ALM-first fit. |

**All measurable cuts together: `cuts`** = `l12` + `xshift` + `xdisp` +
round 1's `lfwd` (accepted by decision 3). This is the figure set against the
budget.

## A. Area: ablation fits on Agilex 5

Everything as round 1's section A (plan record `method.md`), except as listed:

- **Source and baseline:** main `dafabb6db` + `dx.patch` (copied unchanged from the plan record). The i386 RTL, `fit_i386_device.py` and `yosys_netlist.py` are unchanged on main since (`git log dafabb6db..65859fe18` on those paths is empty).
- **`dx` is not refitted.** Its three seeds are the plan record's `fits/dx-s{1,2,3}` (34,606, 34,651 and 34,706 ALMs), copied into this record.
- **Variants:** `l1i2`, `l1d2`, `l12`, `xshift`, `xdisp`, `x87` (`xshift` + `xdisp`) and `cuts`, each `dx` plus its patch, **each at seeds 1, 2 and 3** of one netlist (seeds 2 and 3 through `--netlist`).
- **Netlists:** the i386 lock's Yosys through the script's own `cpu_netlist`, with `YOSYS_MAX_THREADS=1`. The Yosys fix (DeanoC/yosys#81, PR #2130) is still not on main. **These are exploration fits**, as round 1's were.
- **The toolchain cache** is checked ready before each lane starts; if an install is pruned mid-run, round 1's amendment 1 repair (`make toolchain-fes-pc386`) is applied and the failed fit is re-run once, with its failure kept beside it.

**Rules, fixed now:**

1. **Saving:** `dx`'s median of three seeds minus the variant's median of three seeds. The seed-1 difference is printed beside it.
2. **Noise band, per variant:** N = max(100, `dx`'s spread (100), the variant's own spread). A saving of N or less is "under noise", with the figure shown.
3. **The combined fits.** `x87` is reported against `xshift` + `xdisp`, `l12` against `l1i2` + `l1d2`, and `cuts` against the sum of its parts (with round 1's `lfwd` saving, 360, for `lfwd`). The difference is reported; nothing is inferred from single deltas adding up.
4. **M20K and DSP** deltas are reported for every variant beside ALMs. They are not converted into ALMs.
5. **The budget rule.** The plan's budget (summary.md, "The budget") stood at about +1,221 ALMs over the device after the measured shell cuts and before load forwarding. `cuts` includes load forwarding. **`cuts` closes the device gap, first-order, when its saving is at least 1,221.** Short of that, the remaining gap is reported as the difference. This is a sum of a CPU-envelope saving and machine figures, which is first-order (round 1's caveat), and it is not a fit of the DX.
6. **Timing is reported, not judged.** Each fit's worst-corner Fmax estimate is reported; the DX's pass rule (four-seed timing) applies to a fit of the DX machine, which this round does not make. `xdisp` moves the x87 front end's operand muxes ahead of the normalising shift, on the side of the path that bounds the clock in the plan record's hierarchy fit, so its estimate is reported against `dx`'s three seeds and called out if it falls below `dx`'s lowest (63.4 MHz).
7. **A fit that fails** to compile or hits the 90-minute cap is reported so; nothing is estimated in its place.
8. **No figure is compared with a Cyclone V figure.**

## B. Correctness of each functional ablation

- **Lockstep:** round 1's subset, unchanged: `scripts/test_fes_i386_lockstep.py --random-programs 500 --random-seeds 8 --limit-per-file 20`, on main + the variant's patch (the `I486 = 0` default, as round 1). This run includes the x87 suites with the 387 fitted: the x87 cases (125), the BCD cases (28), the transcendental cases (181) and the built-form comparison (2,048 opcodes). Variants: `l1i2`, `l1d2`, `l12`, `xshift`, `xdisp`, `x87`, `cuts` and `qhalf`.
- **The x87 unit bench** for `xshift`, `xdisp`, `x87` and `cuts`: `scripts/test_fes_i386_x87_unit.py --download` at its defaults, which drives the loads, stores and conversions that use the shifter, against the host's x87. `base` (main, no patch) is run the same way.
- **The rule:** a variant passes when each report has the same failures as `base` on the same settings and no new ones. `base`'s lockstep report is the plan record's (same commit, same settings).
- **A fit counts only if its variant passes.** Otherwise its row is reported as not valid.

## C. CPI

The bench and its build are round 1's (`machine_tb`, `Vpc386_machine`, built
by `scripts/sim_fes_pc386.py --suite cpu-profile`, system Verilator, the
machine's CPU at `I486 = 0`). **Memory model:** the bench charges port 0's
first data 18 clocks (its 100 MHz model) against the kit's 11 at 61.44 MHz.
A smaller L1 misses more, so the L1 variants' CPI losses here **overstate**
the kit's; they are upper bounds.

### C1. DOOM

Round 1's window, unchanged (DOOM v1.9 `-timedemo demo3`, census `pmode`
drive, `--rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5`, primary
window h2). `base` is the plan record's base profile. Variants: `l1i2`,
`l1d2`, `l12`, `qhalf` and `cuts`. **The x87 variants are not run on DOOM:**
DOOM issues no x87 instruction and the bench's RTL CPU has no 387, so their
ratio is 1.000 by construction; their cells read "not applicable". The
governor bound of round 1 (2,062 × r against DX2-66 2,912 and DX-33 4,682) is
reported for each.

### C2. 3DBench

- **The disk:** the census `bench` drive, `e69a4cb6…` (`~/fes-drives/pc386/by-name/bench.img`), which carries 3DBench 1.0 at `MARKS\3DBENCH` (`ed1e63c1…`). The Phase 4 records' `bench.img` (`5c585ebc…`) was built outside the repo and is not available, so **this base's CPI is not comparable with the Phase 4 figures** (3.258, 2.221); only ratios within this round are.
- **The window:** fixed by a dated amendment, from oracle-only calibration runs (`--cpu oracle`, frames every 0.5 s, no RTL), **before any 3DBench window of any variant or of `base` runs**. The rule for it, fixed now: Enter is pressed 0.3 s after the title screen is first seen; the RTL takes over 0.2 s after Enter (`--rtl-from`); the window is the first 3.5 s of the fly-through that the calibration shows, starting no earlier than 0.5 s after the handover (the first dump after a handover carries the gap from time 0). The runner reads the values from `t3d.env`, written from that amendment, and waits for it.
- **The comparison:** as DOOM's. Every variant hands over at the same cycle from the same oracle state; a variant whose handover cycle differs is reported as not comparable.
- **Variants:** `base`, `l1i2`, `l1d2`, `l12`, `qhalf` and `cuts`. The x87 variants are not applicable, as for DOOM (3DBench is fixed-point).

### C3. Quake: a bring-up, not yet a measurement

No record has run Quake on the RTL, and the bench cannot today:

- `--x87 1` sets only the machine's CMOS bit; the RTL CPU's `x87_present` is never driven (`machine_tb.cpp`, the `RtlCpu` constructor), so after a handover the RTL has no 387.
- No x87 state crosses a handover (the CPU has no `init_x87*` ports), so the handover must come before Quake's FNINIT.
- The handover needs real mode or CPL 0 with paging off, which Quake's play never meets.

**The attempt, fixed now:**

- A scratch bench edit, [`patches/bench-x87.patch`](patches/bench-x87.patch), drives the RTL CPU's `x87_present` from the machine's. IRQ 13 and ports F0h/F1h stay unwired on the RTL side.
- **Disk:** the census `bench` drive, title `quakes` (`MARKS\QUAKES`, `8506ad26…`), launched as the census does: `cd \marks\quakes|quake +timedemo demo1 +exec mode0.cfg -nosound -nocdaudio|`, with `--x87 1`.
- **Handover:** `--rtl-at-prompt 1`, so the RTL runs Quake from the DOS prompt, before its FNINIT. `--rtl-for 20`, `--cpu-profile-every 0.5`, `--frames` every 1.0 s, a wall cap of 10 hours ([`runners/i486dxr2_quake.sh`](runners/i486dxr2_quake.sh)).
- **The window:** the last 3.0 s of the RTL's 20 (handover + 17.0 s to + 20.0 s).
- **One `base` attempt first.** It is a bring-up when the run reaches its end with the bench's demo playing at handover + 17.0 s (the frame written with `--frames` nearest handover + 17.0 s shows the demo's 3D view, not the console or a loading plaque) and no CPU stop. Then, and only then, `xshift`, `xdisp`, `x87` and `cuts` run the same way.
- **If the attempt fails** for any reason, every Quake cell reads "not measurable", with the failure recorded. No other Quake window or gate is put in its place.
- **What the comparison is.** Each variant runs Quake's load on its own RTL, so the window is the same phase of the demo, not the same instructions. Rows are labelled so.

### C4. Latency bench

`scripts/test_fes_i386_latency.py` per variant, as round 1, for every
functional variant.

## Where it runs

- **Host:** powerboat. Quartus Pro 26.3 under the shared lease the fit script takes, one slot per fit, the script's 90-minute cap. Workspace `/data/projects/i486dx-plan/r2`, outside `/tmp`.
- **Guards:** every job refuses to start under 30 GiB free and writes a heartbeat; every simulation has a `timeout` cap (lockstep and unit benches 4 h, DOOM and 3DBench 8 h, Quake 10 h).
- **No GPU** (Quartus places and routes on the CPU) and **no kit**.

## Amendments

1. **23 November 2026: the 3DBench window (C2), fixed from the oracle calibration.** By then, area fits of `cuts`, `l12`, `l1i2` and two seeds each of `l1d2` and `x87` and one of `xshift` had finished, and `l12` and `cuts` had passed lockstep and their latency benches. No 3DBench, DOOM or Quake profile window of any variant or of `base` had run. C2's rule is unchanged; this fills in its values.
   - **The calibration runs.** Both used `machine_tb --cpu oracle` from `base`'s build (main `dafabb6db`), the census `bench` drive (`e69a4cb6…`, a fresh copy each), `--type 'cd \marks\3dbench|3dbench|'` and `--frames` every 0.5 s. Run a had no keys and ran to 20 s. Run b added `--keys '7.3:|'` and ran to 16 s. Both ended at their `--limit-seconds` (`END limit`), which `machine_tb` exits 1 for by design; neither log has a stop or an error. The frames' pixel hashes are in [`calibration/3dbench-frames.tsv`](calibration/3dbench-frames.tsv), and the end lines are in [`calibration/3dbench-end-lines.txt`](calibration/3dbench-end-lines.txt).
   - **The title screen** (Superscape "VGA Benchmark v1.0") is first seen in the 7.0 s frame of both runs. At 6.5 s the DOS prompt is still typing `3dbench`. So **Enter is at 7.3 s** (`T3D_KEYS='7.3:|'`) and **the RTL takes over at 7.5 s** (`T3D_FROM=7.5`).
   - **The fly-through.** Without a key (run a), the title stays up to 11.5 s and the fly-through ("Now calculating Superscape benchmark") is first seen at 12.0 s. With Enter at 7.3 s (run b), it is first seen at 7.5 s. Its first frame is the same frame as run a's at 12.0 s (pixels `bdbc504edc10…` in both), and the title frame before it is the same in both runs (`535dbe369150…`), so Enter skips the title's wait and nothing else.
   - **The window.** The fly-through starts at 7.5 s, the handover time, so C2's floor (handover + 0.5 s) applies: **the window is 8.0 s to 11.5 s**, the profile dumps at those times (`scripts/pc386_cpu_profile.py --from --to`), and the run ends at `T3D_LIMIT=11.5`.
   - **The runner** reads these from `$R2/t3d.env`, which is a copy of [`calibration/t3d.env`](calibration/t3d.env).
