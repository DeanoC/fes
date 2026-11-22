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

Pending full producer and explicit multithreaded `OPT_CLEAN` replay. Results
will be appended after the build completes. No DE10 kit or HIL was used;
`fes.pc386` still requires DE10 kit HIL.
