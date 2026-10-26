# i386 toolchain lock: Yosys d4ac300d9d, and the per-byte records it reads

Dated record, 2026-10-25. Host simulation and routed diagnostics on Powerboat.
No package, RBF or kit. It gives the i386 RTL its own toolchain lock, removes
the two Yosys workarounds the new Yosys makes unnecessary, and re-measures
the spikes so that only Yosys differs from the
[front-end fixes record](2026-10-25-i386-phase1-front-end-fixes.md). Run
data: [`2026-10-25-i386-toolchain-lock.jsonl`](2026-10-25-i386-toolchain-lock.jsonl).

## The lock

[`cores/fes-common/rtl/i386/toolchain.lock`](../../cores/fes-common/rtl/i386/toolchain.lock)
pins:
- **Yosys `d4ac300d9d`:** DeanoC/yosys main, 487 commits past the
  repository lock's `3a5accbd`. It includes the upstream Yosys 0.70 merge
  and the fork's #43 (fixes #34), #49 (fixes #41), #31 (`nomerge`) and #32.
- **nextpnr `bd7a1c0d`**, the corrected timing model the i386 figures come
  from.
- **Mistral `8fcc4cb4`.**

The shared `toolchain.lock` stays at Yosys `3a5accbd`: a pin bump there must
build every core it touches, and only the i386 RTL needs this one. The path
is one the tooling already accepts:
- `scripts/affected.py` classes the i386 RTL directory as having no core
  consumer.
- `lockfile.py` validates any path.
- `pnr_recipe` binds only the locks registered cores name.

`scripts/benchmark_fes_i386.py` now defaults to this lock. The
[simulation README](../../cores/fes-common/sim/i386/README.md#timing-spikes)
shows the one-time `bootstrap.sh` provisioning; here it built shared cache
slot `9b766266`.

## Workarounds

Removed:
- **#34**, a packed array of packed structs mis-sized. L1's length classes
  and each buffer byte's decode were one plain array per field. They are
  records again: `fes_i386_pkg::lclass_t` and `lbyte_t`.
- **#41**, a 2-D packed array in a generate block. The per-variant length,
  end and next-half fields were flattened into vectors indexed by hand. They
  are 2-D fields again, and Q reads them inside its generate block.

Kept, with their issues still open:
- **#33:** the field-by-field `form_t` setting.
- **#35-#40:** among them gate-level small adds, and `proc -norom` before
  synthesis.

New: **[DeanoC/yosys#52](https://github.com/DeanoC/yosys/issues/52).** Both
Yosys versions read `arr[i].field` on a packed array of structs as a
generate-block reference:
- with a variable index, it is an error;
- with a constant one, a silent, undriven implicit wire.

The records are therefore read and written whole through a temporary. The
first form of this cleanup hit that bug: on `3a5accbd` its length sweep
failed through #52, not #34. With the temporaries in place, the failure on
`3a5accbd` is #34 alone, and `d4ac300d9d` reads the RTL right.

## Checks

`scripts/test_fes_i386_frontend.py`, on Verilator and with `--yosys` on the
netlist each Yosys writes (`proc -norom; flatten; opt -purge`):

| RTL | Verilator | Yosys `d4ac300d9d` netlist | Yosys `3a5accbd` netlist |
| --- | --- | --- | --- |
| Front-end fixes RTL (one array per field) | pass | pass, records identical | pass (the earlier record) |
| Record RTL (this change) | pass, records identical | pass, records identical, no implicit wires | length sweep: all 393,216,000 cases per default size mismatch (#34) |

"Records identical" means equal, field by field, to the front-end fixes
record's results for all three checks:
- the exhaustive length sweep, with every accumulator variant;
- the 14 sampled front-end streams, with exact issue timing and
  redirect-to-issue counts;
- the cycle-by-cycle D/AG stream with its timed latencies.

## Timing spikes

`scripts/benchmark_fes_i386.py`, seeds 1-4, HIP through `gpu-run` with
`NEXTPNR_REQUIRE_GPU=1`, nextpnr `bd7a1c0d`. Every route logged a live HIP
backend. Synthesis is unchanged: `proc -norom; synth_intel_alm -family
cyclonev -nolutram`. The first column is the front-end fixes record's;
between the first two columns only Yosys differs.

| Stage | Yosys `3a5accbd`, fixes RTL | Yosys `d4ac300d9d`, fixes RTL | Yosys `d4ac300d9d`, record RTL |
| --- | --- | --- | --- |
| F1 | 87.6-94.6 | 90.3-93.7 | 83.0-105.6 |
| F2 | 78.4-89.3 | 84.7-87.8 | 82.6-87.8 |
| L1 | 89.7-92.0 | 90.5-95.4 | 89.5-93.2 |
| L2 | 90.2-93.5 | 89.5-91.5 | 89.2-94.5 |
| **Q** | 76.1-86.0 | 76.8-83.2 | 78.7-82.5 |
| D | 85.3-92.4 | 85.8-90.1 | the same routes |
| AG | 91.1-95.8 | 85.5-95.7 | the same routes |
| M2 (control) | 102.0-106.2 | 97.8-106.5 | not re-routed |

Routed fmax in MHz, seeds 1, 2, 3, 4:

| Spike | Yosys `3a5accbd`, fixes RTL | Yosys `d4ac300d9d`, fixes RTL | Yosys `d4ac300d9d`, record RTL |
| --- | --- | --- | --- |
| Front end | 85.97, 80.56, 80.95, 76.06 | 83.24, 83.19, 76.79, 82.82 | 78.66, 81.34, 82.46, 80.80 |
| D+AG | 89.59, 87.67, 92.36, 85.34 | 86.42, 90.07, 85.52, 85.82 | 86.42, 90.07, 85.52, 85.82 |
| M2 | 102.07, 106.19, 102.45, 102.03 | 101.25, 97.77, 106.45, 103.39 | - |

- **The netlists change.** The front end synthesizes to 5,751 COMB cells
  instead of 5,869, and D+AG to 2,266 instead of 2,351. D maps one LUT
  level deeper before placement (8 instead of 7).
- **No stage reaches 100 MHz** on either Yosys, apart from the M2 control.
- **The ranges overlap** the earlier ones within the seed-to-seed spread. Q
  stays the slowest front-end stage, at 77-83 MHz. AG's and M2's lowest
  seeds fall, M2's below 100 MHz once (97.8 on seed 2), and F2's lowest seed
  rises. Under rule 9 the clock still lands at the measured figure: about
  77-83 MHz for the front end and 85-90 MHz for D and AG on the corrected
  model.
- **D+AG's RTL** is untouched by the record change. Its two sets of routes
  are byte-identical, as the deterministic router should give.
- **The record RTL** maps Q one LUT level shallower before placement (7
  instead of 8). The front end's whole-spike range narrows to 78.7-82.5 MHz;
  four seeds do not separate that from the spread.

The stage labels are the front-end fixes record's. The record RTL's buffer is
one register array, `dec`, which L2's label now names; the fixes RTL's routes
are labelled with that record's harness.

The record-RTL re-route with Yosys `d4ac300d9d` is of `5e3658dc3f`, before the
later fetch-response `mem_ready` (now named `mem_valid`) qualifier; that RTL
change was not routed in this comparison. The fixes-RTL routes on the new Yosys are of `137aa86b41`
with this lock added. Since the record-RTL re-route, the harness's L2 label
and documentation changed, and the fetch-response qualifier was added.
