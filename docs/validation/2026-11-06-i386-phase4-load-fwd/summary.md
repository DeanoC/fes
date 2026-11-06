# i386 Phase 4 slice 6b: load forwarding into addresses, 6 November 2026

This record covers a follow-up to CPU Phase 4's slice 6
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). Slice 6's
profile ([record](../2026-11-06-i386-phase4-early-alu/summary.md#loads))
found that `fes_i386_dfwd` has a `LOAD` kind of producer that
`fes_i386_cpu` never drove. Every load into an address register therefore
waited until it reached EX, one clock longer than the design intended. This
slice drives the `LOAD` kind
([design](../../../sources/misteross/docs/i386-cpu-design.md#registers-address-generation-and-the-early-alu)).

**Tree.** The branch started from PR #1517's head `d2ac32e00` before #1517
merged. The seal, lockstep, profile and kit runs used `905f50635`, which is
that head plus this slice. Main has since taken #1517 as `21cdb60a1` and was
merged in. The merge changes only other records, so `sources/` is identical
to `905f50635`.

**Result.**
- **Kit A, max** (against slice 6's run `t1`, package `c214820c`):
  - DOOM v1.9 `-timedemo demo3`: 39.8 fps, up from 39.5 (+0.8%).
  - The DOS Benchmark Pack's DOOM: 36.0 fps at maximum detail, up from
    35.6 (+0.9%), and 99.1 at minimum, up from 98.4 (+0.7%).
  - 3DBench 76.9 and TopBench 309, unchanged. PCPBench 13.3, from 13.2.
- **Bench windows.**
  - DOOM, the second half-second after the handover (the window slices 1
    to 4 used): a load's AGI falls from 1,905,207 clocks to 1,364,287
    (−540,920, 1.08% of the window). The window retires 1.25% more
    instructions, CPI 1.680 from 1.701.
  - DOOM, the first half-second: a load's AGI falls by 286,730 clocks
    (0.57%), and the window retires 0.45% more.
  - In both windows, fetch and L1I clocks stay flat. Unlike slice 6's
    saving, this one is not taken back by an empty front end.
  - 3DBench: no change, clock for clock. It is 16-bit code, and this slice
    forwards only 32-bit loads.
- **Lockstep.** It passes as main does, on the RTL and on the Quartus
  build's Yosys netlist. The one failure is
  [#1510](https://github.com/DeanoC/fes/issues/1510), as on main. The 68
  directed cases include 4 new groups for load forwarding.
- **Seal.** Quartus 17.0.2 meets every analysis
  ([seal-summary.json](seal-summary.json)): worst setup +0.853 ns (slice 6
  +0.802), hold +0.117 ns (+0.104), 33,118 ALMs (32,629).
- **D-stage slack.** D's EIP has +1.95 ns, from slice 6's +2.61, and the
  forwarding tables +2.19, from +2.46. Both are still above slice 4's
  +1.13 ([below](#timing)). The worst path into D is slice 4's
  store-to-load sum, as before, and the new select is not on it.

## As built

- **Which loads.** `pipe_t ldfwd`, set at D, marks a load whose result is
  its load data, whole (`pipe_t ld`). These are MOV r32 and POP r32 from
  memory, and MOVZX r32, whose load data is zero above its size. A 16-bit
  load keeps its register's high half, and MOVSX and a load-op change the
  value in EX, so those stay late.
- **The producer kind.** `fes_i386_cpu` gives `fes_i386_dfwd` the `LOAD`
  kind for these.
- **Late into AG.** D issues the younger uop while the load is in M2. AG's
  late operand from EX is now the EX register's load data, `ex_s.ld`, and
  not its result. Only the `LOAD` kind ever selects that source.
- **From EX.** While the load is in EX, D takes the value from that load
  data too. The table keeps a bit per GPR (`ld_ex`) saying whether its EX
  value is load data or EX's result. It is the same idea as `sp_wb`, which
  says whether a WB value is ESP from AG. POP r32 needs the bit, since its
  register is in `ld` and its ESP is in `result` in the same cycle.
- **Unchanged.** A scaled index still waits one cycle in D when its value
  arrives late, as it does for any producer. Data operands
  (`fes_i386_dops`) are unchanged.
- **Unit bench.** `dag_tb` now feeds the EX load data separately, with
  noise in the EX result register for a load. Its directed waits match
  `fes_i386_dfwd`'s design: a load costs 2, 2 and 3 cycles behind a base,
  an unscaled index and a scaled index. Slice 6 had added the
  `fes_i386_dfwd base_hi` port without connecting it in `dag_tb_top.sv`
  and `spike_dag.sv`, so Verilator refused to build them. This slice
  connects it.

## Profile

`machine_tb --cpu-profile` was run as in slice 6, with the same disks,
commands and handover (page flip 40, 22.226 s). The "before" is slice 6's
run at the same RTL as #1517's head.

AGI clocks charged to a load producer, by the producer's stage while D
waits:

| DOOM window | Stage | Before | After |
| --- | --- | ---: | ---: |
| 22.726–23.226 s | AG | 148,550 | 150,442 |
| | M1 | 405,669 | 412,601 |
| | M2 | 1,026,673 | 800,459 |
| | EX | 324,315 | 785 |
| | Total | 1,905,207 | 1,364,287 |
| 22.226–22.726 s | M2 | 534,419 | 415,886 |
| | EX | 172,828 | 408 |
| | Total | 1,009,059 | 722,329 |

- **EX.** Waits with the load in EX nearly vanish. They were scaled
  indexes and two late operands: such a uop now issues from the EX load
  data as soon as the load reaches EX, rather than one clock later from WB.
- **M2.** The M2 bucket falls by the unscaled 32-bit cases, which now
  issue while the load is in M2.
- **What remains in M2.** The 800,459 clocks left there are loads this
  slice does not forward and the cases it still makes wait:
  - MOVSX, and 8- and 16-bit loads (the profile's `load` class counts all
    three);
  - a scaled index;
  - two late operands.

  The profile does not split these. The `w32` column, which counts 32-bit
  load producers at every stage, falls from 1,250,342 to 690,930.
- **AG and M1.** Clocks with the load in AG or M1 are the load-use latency
  itself, which the design keeps.

Slice 6 estimated 1,023,380 clocks for this window (2.17% of DOOM's
kit-equivalent clocks). That estimate counted every load class. The 32-bit
forms built here recover about half of it, which matches the kit's +0.8%.

Window totals are in [doom-window-h2.json](doom-window-h2.json),
[doom-window-h1.json](doom-window-h1.json) (with
[doom-window-h1-before.json](doom-window-h1-before.json)), and
[3dbench-window.json](3dbench-window.json). The h2 "before" is slice 6's
[doom-window-after.json](../2026-11-06-i386-phase4-early-alu/doom-window-after.json).

### Latency

`scripts/test_fes_i386_latency.py` ([latency-report.json](latency-report.json)):
every loop takes exactly the clocks it took on slice 6. The bench has no
loop of a load into an address.

## Lockstep

`scripts/test_fes_i386_lockstep.py`, on the RTL and with `--yosys` on the
locked Yosys's netlist (slot `5a66161a`), matches main's results:
- the corpus: 1,758,699 tests pass;
- 32,000 random programs: one failure, `random#7#1936`, which is #1510, as
  on main;
- 8,000 protected-mode programs, the unbuilt encodings, the unreal-mode,
  protected-mode, no-387 and memory cases: all pass;
- 68 directed cases pass, 64 before this slice.

The new groups (`lockstep_tb.cpp`, `run_ldfwd_cases`) store the value
first, so the load also takes it from the store:
- `ldfwd-32`: MOV ESI, [BX], MOVZX ESI from a byte and a word, and PUSH
  then POP ESI. Each is 0 to 3 NOPs from [ESI], [ESI + EBX], [EBX + ESI * 4],
  [SI] or [ESP + ESI].
- `ldfwd-ealu`: the same loads of 00AB21FEh, then ADD SI, 2 or ADD ESI, 2
  through the early ALU, which takes the loaded high half, then MOV EAX,
  ESI.
- `ldfwd-esp`:
  - POP ESP, then [ESP];
  - POP ESI, then [ESI] and [ESP];
  - two POPs, then [ESI + EDI];
  - a chain of loads, each the next one's address.
- `ldfwd-other`: loads that still wait. These are MOV SI, [BX], with ESI's
  high half set, both into an address and into the early ALU; MOVSX; ADD
  ESI, [BX]; and LODSD.

Each forwarding path was checked to be covered. Pointing AG's late operand
back at `ex_s.result` fails 3 directed groups. Pointing `fes_i386_dfwd`'s EX
select back at the result register fails `ldfwd-32` at distance 3.

## Timing

`make build-fes-pc386-quartus SEED=1` ran from a clean checkout of
`905f50635`. Package `c89d6b0c…` passes with every analysis met. The
system clock is 61.44 MHz.

| Analysis | Slice 6 `c214820c` | This slice `c89d6b0c` |
| --- | ---: | ---: |
| Setup, worst | +0.802 ns | +0.853 ns |
| Hold, worst | +0.104 ns | +0.117 ns |
| ALMs | 32,629 | 33,118 of 41,910 |

[stage-slack.json](stage-slack.json) has the worst setup slack into each
register group at the −40 °C corner:

| Group | Slice 4 `254b2bf5` | Slice 6 | This slice |
| --- | ---: | ---: | ---: |
| D's EIP (`d_eip`) | +1.13 | +2.61 | +1.95 |
| Forwarding table (`dfwd_`) | +1.84 | +2.46 | +2.19 |
| Data operands (`dops_`) | +2.71 | +3.18 | +2.45 |
| AG (`ag_`) | +2.61 | +2.85 | +2.80 |
| DC (`dc_`) | +2.45 | +2.15 | +2.24 |

- **D's paths.** The worst path into D's EIP starts at a register that
  feeds the descriptor displacement (`desc_t0`). It runs through the segment term and slice 4's
  12-bit store-to-load sum (`store_hazard`) to `issue`. It does not pass
  through the load data or the new `ld_ex` select.
- **The forwarding tables and data operands.** Their worst paths start in
  the same register group.
- **Placement.** The D slack moved without any change on these paths, so
  the change is placement.
- **Elsewhere.** EX's worst rose from +3.70 to +5.51 ns, and WB's from
  +2.66 to +3.41 ns.

## Kit

Kit A ran core-only at max on baseline software `a8cd30c4` and image
`f904961e`, with no install, no image change and no reboot
([kit-scores.json](kit-scores.json)):
- **Run.** `u1`, 6 November 22:27–22:38, with package `c89d6b0c…`.
- **After the run.** The kit was checked against its pre-run snapshot and
  logged FREE.
- **Housekeeping.** The run's five FogCast data trees were removed at
  22:39, and the kit was logged FREE again.
- **Disk and commands.** As in slice 6's run.

| Benchmark | Before (slice 6, `t1`) | After (`u1`) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,425 realtics, 39.5 fps | 3,863 in 3,399, 39.8 fps | +0.8% |
| 3DBench | 76.9 | 76.9 | 0 |
| PCPBench | 13.2 | 13.3 | +0.8% |
| TopBench | 309 | 309 | 0 |
| Pack DOOM, maximum detail | 2,134 in 2,096, 35.6 fps | 2,134 in 2,077, 36.0 fps | +0.9% |
| Pack DOOM, minimum detail | 2,134 in 759, 98.4 fps | 2,134 in 754, 99.1 fps | +0.7% |

## Next

- **More load kinds.** Forwarding MOVSX and 16-bit loads would recover more
  of the load AGI left in M2:
  - MOVSX would need its sign extension at M2;
  - a 16-bit load could forward to a 16-bit address, which uses only the
    low half.

  The profile cannot yet say how the remaining clocks split among these
  and scaled indexes. Splitting it first would size the gain.
- **The front end.** It still limits the code slice 6 sped up.

## Commands

```bash
python3 scripts/test_fes_i386_frontend.py --bench dag
python3 scripts/test_fes_i386_lockstep.py --download
python3 scripts/test_fes_i386_lockstep.py --yosys /abs/path/to/i386-locked/yosys
python3 scripts/test_fes_i386_latency.py --download
QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus make build-fes-pc386-quartus SEED=1 \
  CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains
machine_tb --bios pc386-bios.bin --vgabios pc386-vgabios.bin --disk doom.img --cpu rtl --cpu-profile cpu.prof \
  --limit-seconds 300 --type 'cd doom|doom -timedemo demo3|' --rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5
machine_tb --bios pc386-bios.bin --vgabios pc386-vgabios.bin --disk bench.img --cpu rtl --cpu-profile cpu.prof \
  --limit-seconds 8.0 --type 'b3d|' --keys '3.8:|' --rtl-from 4 --cpu-profile-every 0.5
python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.726049 --to 23.226049 --json doom-window-h2.json
python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.226049 --to 22.726049 --json doom-window-h1.json
python3 scripts/pc386_cpu_profile.py cpu.prof --from 4.5 --to 8.0 --json 3dbench-window.json
```

The disks hold commercial software and are not committed.
