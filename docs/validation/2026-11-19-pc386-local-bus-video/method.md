# pc386 local-bus video: method

19 November 2026. Issue [#2005](https://github.com/DeanoC/fes/issues/2005),
part of the i486 plan (tracker [#1962](https://github.com/DeanoC/fes/issues/1962)),
following slice 3 ([#1999](https://github.com/DeanoC/fes/issues/1999), PR #2000).

This file is committed before any kit run. Its rules do not change after the
runs. A run that cannot meet them is reported as failed or "not measurable".
It is not re-ruled.

Deano's request: "what be good to be an option if possible, if not charge
local-bus video".

## 1. Option or fallback

**Decided: an option.** The governor's VGA memory charge becomes a machine
setting, `fes.pc386.machine` word 7 bits 3:2:

| Value | Video bus |
| --- | --- |
| 0 | The preset's own. ISA for presets 1–5 (286 and 386). Local bus for presets 6 and 7 (486). |
| 1 | ISA |
| 2 | Local bus (VL-Bus or PCI) |

Where the setting comes from and how it reaches the governor:
- **Before this change.** The governor (`fes_i386_governor`, RTL) charged ISA
  time for every VGA memory access under every preset, from `governor.toml`
  `[isa]`.
- **How settings reach it.** The settings travel from FogCast, to the kit
  runtime, into the record, then to the core's `fes_computer_storage`
  (checked at Commit). From there they reach `pc386_system`, then the CPU's
  `gov_*` ports, which the governor samples at reset.
- **Why word 7 has room.** Word 7 was reserved and zero, but the EMS board's
  size had already claimed it ([#1555](https://github.com/DeanoC/fes/issues/1555),
  design doc "Size and setting", values 0/1/2). This change leaves bits 1:0
  to #1555, still reserved and zero. The video bus goes above them. #1555's
  documented values therefore still read the same as whole words while the
  video field is 0.

What the option touches, and why that is in proportion:

- **RTL (pc386 only):**
  - `fes_computer_storage` accepts word 7's video field.
  - `pc386_system` passes it to the CPU.
  - The governor picks the cycle.
  - `governor.toml` gains `[local_bus]` and a per-preset `video`.
  - The core is resealed. The RTL change needs that seal anyway.
- **No ABI constants.** `packages/abi/fes_computer.yaml` gets a comment and
  no new constants, and the layout stays `fes.pc386.machine` 1.0. The reasons:
  - New constants regenerate `fes_computer.vh`, which eight cores' producers
    compile. That would move each of those cores' `BUILD_ID`, and D12 would
    then apply to each of them.
  - A minor version bump would make every runtime that predates it refuse
    the new package.
  - So the field's values are written out in `docs/computer-io.md` and in
    each consumer.
- **Runtime and FogCast.** An optional `video_bus` field, sent only when it
  is non-zero, on these paths:
  - the JSON between host, agent and runtime;
  - an eighth value in `X-FogCast-Machine-Settings`;
  - the runtime's record check.
- **Version skew.**
  - A peer that predates the field never sees it while it is 0.
  - A non-zero value fails closed: the old runtime, FogCast or core refuses
    it. The old core's storage refuses word 7 ≠ 0 at Commit.
  - No old record changes meaning except a 486 preset's, as decided below.
- **No menu change.** No menu surfaces any machine setting today. FogCast's
  `machine` object is where the settings are chosen, so that is where this
  one goes.

## 2. The local-bus cycle

**The charge on the local bus.** A VL-Bus or PCI VGA is on the CPU's 32-bit
bus. An MMIO access therefore takes one local-bus VGA cycle for each 32-bit
dword it touches. That replaces one ISA VGA cycle for each 16-bit half. As on
ISA, the access is charged that cycle less what is already charged for it:
the data sheet's 2 clocks, the wait states, and a dword's second cycle on a
16-bit bus.

**The cycle is fixed time,** converted at the governor's frequency as the ISA
cycles are. Two consequences:
- Preset 7 and preset 6 at 66.7 MHz charge alike, which `reach.py`'s repeat
  rule relies on.
- The charge stays in the form T = A/f + B.

**The figure: 5 clocks of 30 ns, which is 150 ns per dword.** It comes from
[`derive_cycle.py`](derive_cycle.py), output in [cycle.json](cycle.json),
which is fixed before any kit run and fitted to no benchmark:
- **Rows.** Every "VLB DX2/66" row of the Video Card FAQ's Vidspeed table,
  9 rows. The source is Boogyman, 31 January 1995, "DOS Performance", Low Res
  Modes, 320x200 32-bit writes.
- **Why DX2/66 rows.** A DX2/66's bus runs at 33.3 MHz. Its REP STOS issues
  a write every 2 bus clocks, so the card and the bus set the time, not the
  CPU. The FAQ itself explains why a non-doubled 486's rows are CPU-bound.
- **Clocks per write.** For each row, 4 bytes at 33.3 MHz divided by its rate
  gives 3.0, 4.0, 4.0, 4.0, 5.0, 5.9, 11.1, 14.0 and 14.8 bus clocks.
- **Result.** The median is 5.01, which rounds to 5.
- **Hash.** The script checks the FAQ's SHA-256 (`8d0d6f33…`). It also checks
  that the nine rows are every VLB DX2/66 row in that table.

Agreeing sources:
- **The same FAQ:** "With a good card VLB can sustain 4 cycle 32-bit writes
  at 33 MHz", and on PCI without burst mode "Typical rates are 5 or 6 cycles
  per write".
- **The VESA VL-Bus 2.0 standard (November 1993):**
  - its minimum is "1 wait states for reads and 0 or 1 wait states for
    write", so 2 or 3 clocks;
  - the target "may add additional wait states beyond these minimums";
  - "The VL-Bus clock operates at the same frequency and in phase with the
    CPU clock".

What the model leaves out:
- **Reads** are charged as writes. The FAQ's Mode X byte reads run at 9 to
  17 clocks. The governor's MMIO input does not tell reads from writes, and
  the ISA cycle is the same both ways.
- **I/O,** including the VGA ports, keeps the ISA charge. No source here
  times a local-bus VGA's port cycles.
- **The bus clock.** A 25 MHz board's slower bus and a DX-50's faster one are
  not modelled. The governor models no bus clock, as slice 3 says.

**Rounding.** The charge is whole reference clocks: round(150 ns × f), less
2 clocks, never below zero. Each access then takes these times in total:

| Frequency | 20 MHz | 25 MHz | 33.3 MHz | 50 MHz | 66.7 MHz |
| --- | ---: | ---: | ---: | ---: | ---: |
| Time per access | 150 ns | 160 ns | 150 ns | 160 ns | 150 ns |

So the local-bus share of B moves by at most 6.7% between frequencies. On any
benchmark that moves T by at most 6.7% of the local-bus VGA time, against a
2% tolerance and a VGA share that is now small. The record reports that share.

## 3. The 486 presets' default

**Local bus, from the evidence.** In the slice 3 references (PR #2000,
`references.json` and `comparison.json`), the full-screen DOOM rows are:

| 486 class | Local bus | ISA |
| --- | ---: | ---: |
| 486-25 | 3 | 0 |
| 486-33 | 5 | 5 |
| 486-50 | 2 | 1 |
| 486-66 | 25 | 2 |
| All | 35 | 8 |

- Ertl's DOOM list has 11 local-bus 486-66 rows and 1 ISA row.
- PCPBench's rows split the same way.
- The 486DX2-66, preset 7, was the VL-Bus era's machine.

The 386 presets keep ISA, so everything that ships today on presets 1–5 is
unchanged. A 486 preset's existing record (word 7 = 0) changes from ISA to
local-bus pacing. That change is what Deano asked for, and word 7 = 1 gives
the old pacing back.

## 4. Host gates

Each of these must pass. A failure stops the PR and is reported.

1. **Generated files and unit tests.**
   - `scripts/fes_i386_governor.py --check`.
   - `tests/test_fes_i386_governor.py`.
   - The runtime's `make test`.
   - FogCast's `protocol` and `misterruntime` tests.
   - mister-packages `make test` and `check-fixtures`.
2. **The governor bench** (`test_fes_i386_governor.py`), on the RTL and on
   the locked-Yosys netlist (`--netlist`). Its rules are unchanged, and both
   bus choices are covered:
   - every loop is charged exactly the table's clocks;
   - presets 1–5 reach every loop;
   - the preset runs use word 7 = 0, so 486DX-33 and 486DX2-66 are charged
     the local bus;
   - two more runs set word 7: 386DX-25 on the local bus, and 486DX-33 on
     ISA. Each loop must be charged exactly, and paced within 5% where the
     CPU reaches it.
3. **ISA presets unchanged.** Main's bench (`origin/main`, the merge base) is
   compared with this one, both on the RTL:
   - For `max` and presets 1–5, every loop's `charged` must be equal, except
     `vram`.
   - The `vram` loop gains one store, `mov [es:di + 3], ax`, which touches
     two halves and two dwords. Its charge must differ by exactly 200 times
     that store's ISA charge.
   - [`compare_main.py`](compare_main.py) computes this from the two
     `report.json`s.
4. **Lockstep** (`test_fes_i386_lockstep.py`), on the RTL and on the netlist,
   for these runs:
   - `max`;
   - `--preset 6 --same-as` max, which is local bus by default;
   - `--preset 6 --same-as` with `FES_I386_VIDEO_BUS=1`;
   - `--preset 3 --same-as` with `FES_I386_VIDEO_BUS=2`.

   A governor only holds D, so each run must match max's architectural
   report.
5. **test386** to POST FFh (`test_fes_i386_test386.py`).
6. **The storage endpoint** (`sim_fes_pc386.py --suite storage`) replays the
   regenerated `fes.computer` 1.1 exchanges. Those now include word 7's video
   field accepted at 8, and refused at 0Ch and 10h.
7. **The seal.**
   - Quartus 17.0.2 of this head, on chip-30x0-ai-dev if Powerboat is
     loaded, after a netlist lockstep run, as the sealer requires.
   - It must pass every sealer gate. A miss is reported and stops the work.
     No check is relaxed.
   - No commit is made while it runs.

## 5. Kit run

Slice 3's kit method (PR #2000, `method.md` section 4) applies unchanged,
with this record's package. Its parts:
- the claim, baseline checks, core-only run, snapshot check and release;
- the benchmark disk `f82cc8ba…`;
- the six benchmarks in order;
- the readings from kept HDMI grabs;
- the rerun rule;
- the clean-up of the run's FogCast data, inside a lease.

The driver is slice 3's (`~/fes-scratch/i486s3/kit`: `run-kit.sh`,
`hil_games.py`, `bench.py`), copied to `~/fes-scratch/lbv/kit` with only its
messages and paths changed.

Runs and record words:
- **Rows.** `max`, then preset 6 at word 1 = 200, 250, 0 and 500, then
  preset 7, then preset 6 at word 1 = 667.
- **Settings.** Each launch writes the seven settings (`P,F,0,0,544,7,1`), as
  slice 3's driver does, so word 7 is 0. Presets 6 and 7 are therefore on the
  local bus.
- **Why no ISA row.** The baseline runtime refuses a non-zero word 7, and a
  core-only run installs nothing. So this run has no ISA row on the new
  package.
- **The ISA side.** The before figures are slice 3's run `k2`, the same
  presets on ISA. That seal's governor is this one's apart from this change,
  which the record shows by diffing the two seals' pinned inputs.
- **Control.** The `max` row has no governor. Its six scores are compared
  with slice 3's `max` row, to show the package is otherwise the same speed.

The kit run passes if every row's benchmarks are read under those rules.
That includes a "not run" after the one allowed rerun, which is reported.
The scores themselves are reported, not gated.

## 6. Reach and references

Two scripts from PR #2000 (head `9ad260ac2`) run unchanged on this record's
`kit-scores.json`:
- `reach.py` writes `reach.json`.
- `compare.py`, with slice 3's `references.json`, writes `comparison.json`.

Their blob hashes are recorded. A summary sentence may not override either
output:
- `reach.py`'s verdicts say whether 50 MHz and 66.7 MHz are reached on the
  local bus, under slice 3's unchanged rule: its fit at 25 and 33.3 MHz, its
  20 MHz hold-out, its repeat check and its tolerance.
- The before and after ratios are slice 3's `comparison.json` and this one,
  side by side, at each preset.
