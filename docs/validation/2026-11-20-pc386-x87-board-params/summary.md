# pc386: the board's 387 as `pc386_system` parameters

Slice 2 of the DE25 pc386 lane (#2029), issue #2031. The lane plan is
`docs/validation/2026-11-20-de25-pc386-shell-plan/summary.md`, on PR #2030
until it merges.

## The change

`pc386_system` takes two new parameters, `X87_PRESENT` (the i386dx+387
identity) and `X87_BACKEND` (0 the sequencer, 1 the DE25 pipeline). Both
default to 0. `x87_present` is now `X87_PRESENT != 0`, where it was tied
to `1'b0`, and the CPU instance passes `X87_BACKEND` on. Neither the DE10
top nor the seal sets them, so the DE10 package keeps its identity off.
Turning the identity on is x87 slice 7 (#1768).

The fit harnesses take the settings in place of editing the RTL:

- `build_fes_pc386_quartus.machine_program(..., x87=...)` adds the
  parameters to the Yosys program. It refuses any key other than the two,
  and any value other than 0 or 1. The seal passes none.
- `fit_i386_device.py --x87-present/--x87-backend` sets them for
  `--design machine`. It refuses them for the CPU design, the
  SystemVerilog front end and a reused `--netlist`, because none of those
  goes through this elaboration. The netlist record carries the settings
  used.
- `quartus_x87_spike.py --identity-on` passes `X87_PRESENT = 1`, and its
  text overlay of `pc386_system.sv` is deleted. The DE25 fits'
  `scratch-edits*.patch` files in earlier records stay, as the history of
  how those fits were made.

## What the change does to the DE10 package

It edits RTL in the sealed source closure, so AGENTS.md's closure-only
rule (D12) does not apply. The DE10 package therefore took its full gates,
listed below.

**The machine netlist is not byte-identical, and is not expected to be.**
These were elaborated in one checkout, one after another, with the
pc386 lock's Yosys, through `fit_i386_device.py --design machine
--project-only`:

| Revision | `pc386_system.v` SHA-256 |
| --- | --- |
| main `8a162c536` | `944810ce055bee600a3cfdbfc250b3e8c662e85332b72c7e265c3b01a8c2ea39` |
| this branch `dc7a120f8` | `75105e0bbfcfb8b342f5085200b8053e7ea5ba154f8ca91f6c40d85a8e6e9da6` |
| main `8a162c536` again | `944810ce…2ea39`, unchanged |

The two netlists are the same size, 5,883,250 bytes. They differ only in
the numbers on Yosys's auto-generated names (`…_sv_<line>_<n>__result`,
894 of them, mostly shifted by 288) and in the order of the statements
those names sort. Two other spellings of the edit gave a third digest:
`parameter bit` with a plain assign, and leaving the CPU instance's
parameter unpassed. So the shift comes from declaring a parameter at all.
This is the name sensitivity of DeanoC/yosys#80. Logical equivalence was
not checked formally. The gates below are the evidence.

**The CPU is untouched.** `yosys_netlist.cpu_source_sha256` is
`31cf2a5d83503901c23413b0bd0c00c31734fce1e719e170126231956788862e` on both
revisions. So the CPU lockstep suites and test386, which run
`fes_i386_cpu` alone, test the same bytes as on main. test386 was not
rerun. The fresh lockstep below ran because the producer requires one.

## Gates

| Gate | Where | Result |
| --- | --- | --- |
| Repo tests (`test_fit_i386_device`, `test_quartus_x87_spike`, `test_build_fes_pc386_quartus`, `test_build_pc386_product`, `test_pc386_seal_process`) | Powerboat | 172 pass |
| `sim_fes_pc386.py --suite system` (`pc386_system` with `fes_i386_cpu`, FreeDOS, at the parameters' defaults) | Powerboat | PASS: prompt at 191,533,231 cycles; disk save, CMOS, record, port-2 hold and status-ring checks all pass |
| Netlist lockstep, DE10 backend, through the producer's `--with-lockstep` | chip-30x0-ai-dev, CUDA toolchain slot | pass: corpus 1,758,699 tests of 941 files, random 32,000, undefined 4,000, directed 84, paging 170, x87 125, BCD 3, transcendental 10, no-387 1,933, memory 27. CPU netlist `7d3534c1`, report `9174a7d0` |
| pc386 seal, registered producer `build_pc386_product.py`, Quartus 17.0.2 Lite, seed 1 | chip-30x0-ai-dev | BUILD_ID `1820782b5b839f43138445ea14093930`; package `92b82fe33fbc8610c2a66bf77b3bff1f1ea6fbfc05a75623ad8d12e2c47e32f1`; RBF `8f98a48ebfdcdce07c7b0afbbd322fbe78e3212edbfe12e10edc8dc3dab68b5f`; 36,432 of 41,910 ALMs; worst setup +0.112 ns, hold +0.086 ns, recovery +11.892 ns, removal +0.420 ns. Every corner passes |
| Kit B, run `k2031b` | Powerboat | pass: the sealed package `92b82fe3` launched from the library on the user-supplied drive `a6977555`; FreeDOS prompt in 0.6 s, Wolfenstein 3D drawn and walked. Core-only: nothing installed. The kit was restored to its baseline (image `179affbb`, bundle `c9ae79e2`), idle and lease-free, and logged in `kitB-STATUS.md` ([result](kit-b-k2031b-result.json)). An earlier attempt, `k2031a`, stopped at the driver's import before touching the kit; it was verified on baseline and logged FREE |

The seal's setup margin is within the seed-1 range earlier pc386 seals
have shown (+0.081 to +0.672 ns). The seal checks are unchanged.

## Reproducing

From `sources/misteross`:

```bash
python3 cores/fes-pc386/tools/build_pc386_product.py --with-lockstep --gpu-router CUDA --cache-root /abs/fes-toolchain-cache --seed 1
```

```bash
python3 scripts/fit_i386_device.py --design machine --device de25 --x87-present 1 --x87-backend 1 --cache-root /abs/fes-toolchain-cache --output /abs/new-dir
```
