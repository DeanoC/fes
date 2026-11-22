# i386 and pc386 Yosys #81 lock bump

Date: 2026-11-22  
Candidate Yosys: `d273c1cb774f70d58c890745ac1b7dd106d32a26`  
Previous Yosys: `e240a4653bb6578e0b267f730128f8a5dac28483`

This record compares functional behavior; generated netlist bytes and names
may differ because #81 changes hashmap iteration order.

## i386 functional checks

The full lockstep suite passed on both pins using the same source and corpus:

- 941 corpus files, 1,758,699 SingleStepTests passed.
- Random 16x2000 (32,000 programs), protected mode (8,000), tasks (4,000),
  debug (4,000), undefined instructions (4,000), and directed/x87 cases passed.
- The old and new lockstep reports match in every functional field. Their
  netlist SHA-256 values differ: old `dd80a7ff2e3cf510636188424cb205e4eae993ccebacee74a39a6f980923ce17`,
  new `a1127633b7a17537183817d1ba64cd895af2b7cd8fb7fcd6929e54daf2dba649`.
- The exhaustive frontend suite passed on both pins. Its functional reports
  match after excluding tool and netlist provenance.

Reproduce the report comparison with:

```sh
python3 sources/misteross/docs/validation/2026-11-22-yosys81-pin-bump/compare_reports.py
```

The four source reports are in `reports/`. The comparison ignores only tool
identity and generated netlist provenance, plus lockstep source tracking
metadata; it compares the remaining report content exactly.

## Generated assignment review

A text diff initially listed these three assignments as old-only:

```verilog
assign _07350_ = decode_pm ? (* full_case = 32'd1 *) 2'h0 : 2'h1;
assign _07767_ = decode_pm ? (* full_case = 32'd1 *) 3'h0 : 3'h1;
assign _07675_ = decode_pm ? (* full_case = 32'd1 *) 1'h0 : 1'h1;
```

They are not removed. Each occurs in both generated netlists with the same
right-hand side (old lines 51805-51807; new lines 54762-54764). The generated
assignment block moved during Yosys's changed hash iteration order, making
the positional diff misleading. These are live decode signals; their uses
remain in both netlists. This is not dead-logic removal and no behavior change
was found.

## Formal equivalence

The direct `equiv_make` attempt refused the raw generated Verilog because it
contains processes. Adding `proc; memory_map; opt` expanded the design to over
1.7 million cells and roughly 195,000 unused cells/wires before a proof could
be completed. The run was stopped to avoid consuming the host. Formal
equivalence is therefore not completed; the old/new lockstep and exhaustive
frontend comparisons are the functional equivalence evidence for this bump.

## Full misteross test suite

`python3 -m unittest discover -s tests -v` from `sources/misteross` completed:
2,828 tests ran, 88 skipped, zero failures. The skips require unavailable
vendor archives, prepared hardware projects, or optional tool pins.

## pc386 producer, multithreaded cleanup regression, and timing

The full producer completed on source revision
`cbf76585481475df656f6398f6a5edad569796ce` with the new pin. It first ran a
fresh full i386 lockstep (all counts above passed), then elaborated the full
machine and ran Quartus Prime Lite 17.0.2. The producer used the authenticated
HIP toolchain slot; its CLI states that this Quartus producer uses no GPU
routing. Quartus finished with zero errors. Its fitter reported an Auto Fit
compilation and skipped optimizations to reduce compilation time; this is the
recorded Quartus run and passed the existing timing gate.

- New package `BUILD_ID`: `62305375764cda287bef28b9ef7f08e4`.
- New `core.rbf` SHA-256:
  `9beed9343c236be97b5abc92d5155e028a50060f8df12b82551e5d9d243c0e27`.
- Old-pin `BUILD_ID`, calculated from the same source revision with the
  previous Yosys locks and authenticated old Yosys tool identity:
  `62305375764cda287bef28b9ef7f08e4`. This is a design identity; the schema
  records toolchain identity separately. The old-pin Quartus package was not
  rebuilt.
- TimeQuest worst setup / hold slack: slow 100°C `+0.350 / +0.264 ns`, slow
  −40°C `+0.146 / +0.187 ns`; fast 100°C `+7.494 / +0.151 ns`, fast −40°C
  `+8.196 / +0.109 ns`. The producer's checked summary reports overall
  worst-case setup `+0.146 ns`, hold `+0.109 ns`, and `status: pass`.
- Quartus elapsed time: 19m40s (the producer captured exit 0).

The exact `build/fes-pc386-quartus/machine.ys` script was replayed three times
with `YOSYS_MAX_THREADS=4`, using the authenticated d273c1cb Yosys executable.
All three exited 0, ended normally without an assertion/crash, and produced
the same Yosys logfile hash `1a24cfb0ba`; elapsed times were 66.46s, 50.64s,
and 49.36s. This script includes both `opt -purge` passes around `memory_map`,
the #77 crash path. Compact replay evidence and log SHA-256 values are in
`reports/mt-opt-clean.txt`; the full 22 MiB logs remain in the ignored build
directory `build/validation/yosys81-mt-opt-clean/`.

The full misteross suite completed: 2,828 tests, 88 skipped, zero failures.
No DE10 kit or HIL was used; `fes.pc386` still requires DE10 kit HIL.
