# i386 Phase 4 baseline, 5 November 2026

This record is CPU Phase 4's slice 1, the measurement baseline
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). Every later
slice reports its before and after against these numbers. It changes no RTL,
so it needs no seal.

**Result:**
- **Profile hook.** `machine_tb --cpu-profile` has landed, with the
  Bochs-to-RTL handover. On DOOM's `demo3` window it gives the same counts
  as the scratch hook behind the
  [Phase 4 profile](../2026-11-04-i386-phase4-profile/summary.md).
- **Microbenchmarks.** On the RTL, a taken Jcc costs 11.5 clocks, CALL and
  RET together 23.4, and an indirect CALL and its RET 29.3.
- **Kit scores at `max` (Kit A, package `01e1eebb…`).**
  - 3DBench: 50.0 frames a second.
  - PCPBench: 10.7.
  - TopBench: 297.
  - The DOS Benchmark Pack's DOOM timedemo: 24.3 fps at maximum detail and
    69.7 at minimum.
  - DOOM v1.9 `-timedemo demo3`: 26.7 fps, the same as Kit B's games smoke
    run.
- **Presets.** The table below gives every preset's scores.

## Profile hook

[`cpu_profile.h`](../../../sources/misteross/cores/fes-pc386/sim/cpu_profile.h)
puts every system clock the RTL runs into one cause, and each retirement's
clocks into its instruction class. It reads D's issue and wait terms through
[`cpu_profile.vlt`](../../../sources/misteross/cores/fes-pc386/sim/cpu_profile.vlt)
(`public_flat_rd`), so the CPU's RTL is unchanged.
[`pc386_cpu_profile.py`](../../../sources/misteross/scripts/pc386_cpu_profile.py)
reports a window: clocks by cause, redirects by branch class, instruction
classes and the hottest sites. The pc386 README's
[CPU profile](../../../sources/misteross/cores/fes-pc386/README.md#cpu-profile)
section gives the flags.

These are the changes from the scratch
[profile-hook.patch](../2026-11-04-i386-phase4-profile/profile-hook.patch):
- **Handover.** The accounting starts at RTL-BEGIN. The scratch hook charged
  the first retirement after the handover its whole gap from time 0, so its
  first window after the handover was unusable.
- **Oracle steps.** Oracle steps before the handover no longer count as the
  RTL's.
- **End of run.** `--rtl-for` ends the run as `rtl-window`, a pass.
- **Option checks.** The profile flags need `--cpu rtl`.
- **Removed.** The scratch build wrapper, the gain estimator and the frame
  dumps were left out.

**Load-use.** A data load never holds D. The load is at M1–M2, ahead of the
ALU at EX, so the only load-use interlock is a load's result used as an
address. That interlock is the AGI cause.

### Equivalence with the profile

The landed bench reran DOOM v1.9's `-timedemo demo3` (the profile's disk
`7d98bffd…`) on the oracle to page flip 40, then on the RTL for 1.1 s, with a
snapshot every 0.5 s. The CPU's RTL has not changed since the profile's
`d646bcd7e`; only `pc386_system.sv` has, and the machine bench does not build
it. The two runs compare as follows
([handover-equivalence.json](handover-equivalence.json)):

| Window (RTL from cycle 2,222,604,910) | Buckets | Classes | Sites |
| --- | --- | --- | --- |
| 0.5–1.0 s after the handover: 19,694,266 retired, CPI 2.539 | identical | identical | identical |
| 0–0.5 s after the handover | identical | the landed hook's first `mem_op` gap is 15.6M clocks; the scratch hook's was 2,238M (its gap from time 0) | differ by that one gap |

### Short window in CI

`sim_fes_pc386.py --suite cpu-profile` (`make sim-fes-pc386-cpu-profile`, in CI's
pc386 full tier) boots FreeDOS on the oracle and types CPUPROF.COM
([`cpu_profile_test.asm`](../../../sources/misteross/cores/fes-pc386/sim/cpu_profile_test.asm)).
The RTL takes over at its fourth page flip and runs 20 ms. The suite fails
in any of these cases:
- a clock is unsorted;
- the RTL stops or hands work to the oracle;
- an SMC restart happens;
- a taken branch class or a cause the program exercises is missing.

It takes about 8 minutes on Powerboat. The suite passed with:
- 2,000,000 clocks, all sorted, and 213,827 retired (CPI 9.35);
- 174 page flips and no stops ([cpu-profile-window.json](cpu-profile-window.json)).

## Latency microbenchmarks

[`latency_bench.asm`](../../../sources/misteross/cores/fes-common/sim/i386/latency_bench.asm)
is in the governor bench's form. It has one loop per row of the latency
table that Phase 4 changes.
[`test_fes_i386_latency.py`](../../../sources/misteross/scripts/test_fes_i386_latency.py)
runs it on the RTL alone under `max`, as `lockstep_tb`'s governor mode does,
with port 0 at its fixed latency. The RTL alone is deterministic, so each loop
must take exactly its expected clocks. A slice that changes them updates
those values and records the before and after. The run takes about
20 seconds.

The baseline on main `c01c878cd` ([latency-report.json](latency-report.json)):

| Loop | Iterations | Clocks | Per iteration | Per case |
| --- | ---: | ---: | ---: | ---: |
| DEC BP and JNZ, taken (`jcc_loop`) | 4,000 | 47,999 | 12.00 | — |
| 8 taken JE, each over a NOP (`jcc_taken`) | 1,000 | 103,999 | 104.00 | 11.50 |
| 8 CALL rel and RET (`call_ret`) | 1,000 | 199,118 | 199.12 | 23.39 |
| 8 CALL BX and RET (`call_reg`) | 1,000 | 245,999 | 246.00 | 29.25 |
| 8 CALL [mem] and RET (`call_mem`) | 1,000 | 246,029 | 246.03 | 29.25 |
| 8 real-mode far CALL and RETF (`far_call`) | 1,000 | 269,029 | 269.03 | 32.13 |
| MOV CX, 8 and LOOP (`loop`) | 1,000 | 90,999 | 91.00 | 9.88 |
| 8 MOV BX, [BX] (`agi_load`) | 1,000 | 44,999 | 45.00 | 4.13 |
| 8 ADD SI, 2 and MOV AX, [SI] (`agi_alu`) | 1,000 | 52,999 | 53.00 | 5.13 |
| 8 stores, each with a load from the same address (`store_load_same`) | 1,000 | 36,029 | 36.03 | 3.00 |
| 8 stores, each with a load from another line (`store_load_other`) | 1,000 | 36,029 | 36.03 | 3.00 |

"Per case" takes the bare loop's 12 clocks out of each iteration and divides
the rest by the eight cases. The loops' set-up moves are left in, and `loop`
counts seven taken LOOPs and one that falls through.
- **Branches.** Each taken branch costs 11–12 clocks, as the profile found.
- **Store and load.** A load waits behind a store whether or not it
  overlaps. Slice 4's change shows as a drop in `store_load_other` alone.

## Kit scores

Run `a1` used Kit A, core-only, on 5 and 6 November from 22:28 to 00:56
(kit-runs data in [kit-scores.json](kit-scores.json)):
- **Package.** `01e1eebb…` is main's current seal: Quartus 17.0.2 at
  `b4558d4cd`, from the [CPU status record](../2026-11-04-pc386-cpu-status/summary.md).
  - Its 110 sealed inputs hash identically on main `c01c878cd`.
  - Its `core.rbf` matches that seal summary.
- **Kit.** Baseline software `a8cd30c4` on image `f904961e`, with no
  install, no image change and no reboot. Deano gave a standing OK for
  either free kit.
- **Disk.** `fe81c9c4…` was staged from Powerboat and is not committed:
  - main's FreeDOS games image (`36f50d52…`);
  - DOOM v1.9 from Deano's eXoDOS zip (`06ee36b4…`);
  - the DOS Benchmark Pack v1.5's 3DBENCH, PCPBENCH, TOPBENCH and DOOMS
    folders, under `C:\MARKS`.

  The JSON gives the executables' SHA-256s.
- **Memory scrub.** Each preset's launch followed a SCRUBX launch, since
  #1499 (zero guest memory on load) has not merged.
- **Sound Blaster.** Every launch set 220h, IRQ 7, DMA 1.
- **Reading scores.** Each score was read from an HDMI grab once its finished
  screen had appeared. PCPBench was read from two grabs at least 30 s apart
  showing the same value.

| Preset | 3DBench (fps) | PCPBench | TopBench | Pack DOOM, max detail (realtics, fps) | Pack DOOM, min detail (realtics, fps) |
| --- | ---: | ---: | ---: | ---: | ---: |
| `max` | 50.0 | 10.7 | 297 | 3,072, 24.3 | 1,072, 69.7 |
| 486DX2-66 | 26.3 | 7.4 | 133 | 5,973, 12.5 | 1,378, 54.2 |
| 486DX-33 | 20.4 | 5.5 | 102 | 7,364, 10.1 | 1,650, 45.3 |
| 386DX-40 | 14.9 | 4.3 | 90 | 10,326, 7.2 | 2,380, 31.4 |
| 386DX-33 | 12.9 | 3.6 | 81 | 11,692, 6.4 | 2,756, 27.1 |
| 386DX-25 | 10.4 | 2.8 | 68 | 14,458, 5.2 | 3,516, 21.2 |
| 386SX-16 | 6.4 | 1.3 | 42 | not run | not run |
| 286-12 | 5.0 | 0.9 | 33 | not run | not run |

- **DOOM timedemos.** Each ran 2,134 gametics. The Pack's figure is
  74,690 / realtics, which is 35 × 2,134 / realtics.
  - The 386SX-16 and 286-12 timedemos were left out to stay within the run's
    lease limit.
  - The registered DOOM v1.9 `-timedemo demo3` was run at `max` only:
    3,863 gametics in 5,064 realtics, 26.7 fps. Kit B's games smoke run gave
    5,061–5,062 realtics on seal `499f4805`.
- **TopBench at `max`.** The components are MemTest 20, MemEA 17, Opcodes 19,
  VidMem 98 and 3DGames 21 µs (175 in all). The 286-12 score (33) and the
  `max` score (297, against 298) match the
  [M5 game checks](../2026-11-04-pc386-m5-game-checks/summary.md).
- **Esc misses.** At 386SX-16 and 286-12, the Esc that leaves 3DBench was
  missed, so the first PCPBench reading was of the 3DBench screen and was
  discarded:
  - 286-12's PCPBench and TopBench were rerun by hand in the same launch;
  - 386SX-16's PCPBench was rerun in a new launch, after a scrub.

  `max`'s PCPBench was also rerun in a fresh launch and gave 10.7 again.
- **Clean-up.** The run's 23 FogCast data trees (11 game records, 11 media
  trees and the staged disk) were removed afterwards. The shared SCRUBX
  scrub image (`8538495a…`) stays on Kit A, as on Kit B, for other sessions'
  scrubs. The kit was logged FREE at 01:07.

## What this record does not cover

- No seal: the CPU's RTL is unchanged.
- No new profile windows: the CPU's RTL is the one the
  [Phase 4 profile](../2026-11-04-i386-phase4-profile/summary.md) measured,
  and its windows stand as the baseline. Each later slice reruns them with
  `--cpu-profile`.
- Wolfenstein 3D's page flips per second on the bench stand as that
  profile's: 912 frames in 13 simulated seconds.
