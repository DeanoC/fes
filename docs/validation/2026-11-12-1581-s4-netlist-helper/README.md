# #1581 S4: one Yosys netlist program; lockstep reads the seal's

S4 of [`docs/design/rtl-frontend-lanes.md`](../../design/rtl-frontend-lanes.md) adds
`sources/misteross/scripts/yosys_netlist.py`. It holds:

- the recipes: the pc386 seal's, and PC/XT's;
- `program()`, which renders a recipe for a top, its sources, include directories and
  parameters, and `canonical()`, the same recipe with those as placeholders;
- `run()`, one Yosys invocation;
- authentication of the pc386 lock's Yosys from its slot;
- the CPU source list and its digest.

Every producer and check that feeds Quartus or Verilator a Yosys netlist now calls it:

| Caller | Before | After |
| --- | --- | --- |
| pc386 sealer | its own program, then the Python `plain_identifiers` rewrite | the seal recipe, with `rename -unescape` in the same run; `plain_identifiers` deleted |
| lockstep, front-end, governor and latency checks | `--yosys PATH`: any Yosys, `read; proc -norom; flatten; opt -purge; write_verilog -noattr` | `--netlist`: the seal recipe at top `fes_i386_cpu` (or a bench top), with the pc386 lock's Yosys |
| i386 Quartus oracle | `--yosys PATH`, the lockstep program | the seal recipe, keeping `hdlname` for its stage labels, with the pc386 lock's Yosys |
| i386 benchmark and Q's aligner check | inline synthesis programs | `synthesis_program()` |
| PC/XT board fitter | its own program | the PC/XT recipe; the programs are byte-identical to main's (`tests/fixtures/pcxt-elaboration-programs.json`) |

`rename -unescape` at the old pin `0a7927bf` asserted on `pc386_system`, because
`cpu.ex_out` and `cpu.ex.out` both map to `cpu_ex_out`. It also left memories escaped.
[DeanoC/yosys#76](https://github.com/DeanoC/yosys/pull/76) (issue #75) fixed both and merged as
`e240a465`, and the i386 and pc386 locks now pin it. Since `0a7927bf`, that commit adds only #70
(AGENTS.md) and #76.

## Runs

All runs were on Powerboat. The trees:

- main: `d91880944`;
- branch: `91ead94bc`, which has main merged in.

| Slot | Lock | Router | Yosys binary sha256 | Used by |
| --- | --- | --- | --- | --- |
| `e4d761ef…` | pc386 `294c8945…` | HIP | `be9f3be5…` (`e240a465`) | branch seals, lockstep `--netlist`, front end `--netlist` |
| `0a4c6c2a…` | i386 `147746f7…` | OFF | `f61b478f…` (`e240a465`) | Verilator v5.054 for every i386 simulation |
| `05416979…` | pc386 `2f08dd9b…` (main) | HIP | `1c1fb5cd…` (`0a7927bf`) | main seals |
| `6ec78306…` | PC/XT `quartus.lock` | — | `d45b5d6e…` (`3a5accbd`) | both PC/XT builds |

A first round of these runs is not used. Two copies of the evidence queue ran at once in the same
build and simulation directories, so every result was discarded and the runs repeated once each.

## Results

### 1. Lockstep on the seal's CPU netlist

- **Runs:** `test_fes_i386_lockstep.py --jobs 10` (RTL) and the same with `--netlist`, on the branch
  ([`reports/lockstep-rtl.json`](reports/lockstep-rtl.json),
  [`reports/lockstep-netlist.json`](reports/lockstep-netlist.json)). Both pass.
- **Comparison:** `python3 compare_reports.py reports/lockstep-rtl.json reports/lockstep-netlist.json`
  (D2's copy of S3's comparison, skipping only `verilator` and `yosys`) finds all 13 hash fields
  equal. The only other difference is the netlist run's `netlist` record.
- **The netlist run's record:**
  - `program_sha256`: `23ce90ec…`, the digest of the seal recipe's canonical form;
  - `cpu_source_sha256`: `ba402cb7…`, over the CPU's 30 sources in read order;
  - `netlist_sha256`: `879dee74…`;
  - the Yosys identity: the pc386 lock, its sha256 and the slot binary.
- **Reproduces:** `python3 scripts/yosys_netlist.py cpu --cache-root …` writes the CPU netlist again
  with the same `879dee74…`.
- **Front end:** `test_fes_i386_frontend.py --netlist` passes on every bench
  ([`reports/frontend-netlist.json`](reports/frontend-netlist.json)). Q's aligner synthesizes to 5,037
  cells, with 0 multipliers and 0 carry cells.

### 2. The seal and the lockstep record the same inputs

The branch seal's `build-summary.json` has a new `netlist` record
([`reports/pc386/branch-seed1.json`](reports/pc386/branch-seed1.json)):

- its `program_sha256`, `cpu_source_sha256` and `yosys` are equal to the lockstep report's;
- `machine_netlist_sha256` is the digest of the sealed `pc386_system.v`.

S5 turns this into the producer gate.

### 3. pc386

- **Netlist:** `pc386_system.v` changes only in identifier spelling and Yosys's auto-numbered
  names. `python3 netlist_shape.py <main> <branch>`, which abstracts every identifier and drops
  whitespace, finds both netlists 116,616 lines long, with no line shape in one that is not in the
  other. The byte sizes are 5,742,770 (main, `plain_identifiers`) and 5,674,652 (branch,
  `rename -unescape`).
- **Builds:** `make build-fes-pc386-quartus SEED=n` on main and the branch, seeds 1-3
  ([`reports/pc386/`](reports/pc386)). Every build passes every analysis on every clock, with TNS 0.

| Worst slack (ns) | main seeds 1 / 2 / 3 | main mean | branch seeds 1 / 2 / 3 | branch mean |
| --- | --- | --- | --- | --- |
| Setup | 0.561 / 0.763 / 0.191 | 0.505 | 0.209 / 0.287 / 0.678 | 0.391 |
| Hold | 0.067 / 0.091 / 0.075 | 0.078 | 0.091 / 0.079 / 0.072 | 0.081 |
| Recovery | 7.232 / 7.046 / 7.130 | 7.136 | 6.975 / 6.448 / 7.443 | 6.955 |
| Removal | 0.749 / 0.678 / 0.258 | 0.562 | 0.265 / 0.267 / 0.267 | 0.266 |
| Min pulse width | 1.122 | 1.122 | 1.122 | 1.122 |

| Resources | main seeds 1 / 2 / 3 | branch seeds 1 / 2 / 3 |
| --- | --- | --- |
| ALMs | 36,833 / 36,778 / 36,728 | 36,841 / 36,788 / 36,782 |
| Registers | 32,674 / 32,647 / 32,649 | 32,778 / 32,644 / 32,675 |
| RAM blocks / DSPs | 269 / 38 | 269 / 38 |

The clocks are 61.44, 74.27 and 12.29 MHz in every build.

- **Reading the tables:** the setup ranges overlap. The branch's three removal results sit at
  main's lowest seed. ALMs move by +24 on average (0.07%) and registers by +42 (0.13%). The netlist
  structure is the same, so these differences are Quartus reacting to different names, as a
  BUILD_ID change does (#1660 moved removal slack by 0.3 ns and registers by 57). They are not a
  change in the design.
- **No HIL:** pc386 has none (`AGENTS.md`). The seals stay development builds.

### 4. PC/XT (D12)

The PC/XT BUILD_ID moves because `quartus_fes_pcxt.py` and the new helper are in its closure. The
RTL, its programs and its Yosys do not change. The D12 evidence (`AGENTS.md`, hard rules):

1. **Netlist:** main's and the branch's `elaborated.v` differ only in the mailbox constant that
   carries the BUILD_ID (`a2a705a7…` against `1020af35…`).
2. **Main's BUILD_ID with the branch's tooling:** the branch fitter run with main's BUILD_ID
   `d84f8edf…`, seed 1 ([`reports/pcxt/control-main-build-id-summary.json`](reports/pcxt/control-main-build-id-summary.json)),
   writes main's `top.rbf` byte for byte: `41dafd7c…`, the same as main's signoff and repeat RBFs.
   Its `elaborated.v` equals main's once the run's own path is normalised (`27742f70…` for all
   three).
3. **The branch's own build:** `build_fes_pcxt_quartus.py` passes its internal timing on seeds 1-3,
   and its repeat RBF matches its signoff RBF (`3b6c8274…`;
   [`reports/pcxt/branch-build-summary.json`](reports/pcxt/branch-build-summary.json)).

So no PC/XT HIL is needed.

**About the path:** PC/XT's `elaborated.v` is not path-free on main either. Function-result wires
keep the absolute capture path in their names, for example
`…_func__home_…_signoff_source_cores_fes_common_vendor_kf8259_…`, so main's own signoff and repeat
netlists differ. The comparison replaces `_home_deano_*_source_cores_` with one token
(`sed -E 's/_home_deano_[A-Za-z0-9_]*_source_cores_/_ROOT_source_cores_/g'`). The RBFs do not depend
on it.

## Not covered

- **The i386 front-end spike** no longer reads under the locked Yosys on main
  (`spike_frontend.sv:107`, #1738). The oracle's labels were checked on the dag, m2 and cpu spikes:
  `quartus_fes_i386.py`'s `register_names` and `node_label` give the same per-stage register counts
  on main's escaped netlist as on the new `hdlname` one.
- **`benchmark_fes_i386.py`** moved to `synthesis_program()` without a routed run. The i386 lock has
  no HIP slot for its nextpnr, and i386 routes are parked (#1235).

## What else this change touches

- **Each host** rebuilds the pc386 HIP slot (`make toolchain-fes-pc386`) and the i386 OFF slot
  (`make toolchain-fes-i386`), since both locks change.
- **`--yosys` is gone** from the i386 checks. Use `--netlist` with the cache root that holds the
  pc386 slot.
- **The fes.pc386 and PC/XT BUILD_IDs move.** pc386 is resealed above. PC/XT is covered by D12.

## Files

- `reports/lockstep-{rtl,netlist}.json`, `reports/frontend-netlist.json`: the check reports.
- `reports/pc386/{main,branch}-seed{1,2,3}.json`: the producer's `build-summary.json` for each build.
- `reports/pcxt/{main,branch}-build-summary.json` and `reports/pcxt/control-main-build-id-summary.json`:
  the PC/XT producer summaries and the control fit's.
- `compare_reports.py`: the lockstep comparison, from
  [`2026-11-10-1581-d2-yosys-lock`](../2026-11-10-1581-d2-yosys-lock/README.md).
- `netlist_shape.py`: the structural netlist comparison.
