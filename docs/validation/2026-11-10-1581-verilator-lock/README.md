# #1581 S3: Verilator lock forward, 10 November 2026

Slice S3 of [RTL front-end lanes](../../design/rtl-frontend-lanes.md)
(tracker [#1581](https://github.com/DeanoC/fes/issues/1581), issue
[#1648](https://github.com/DeanoC/fes/issues/1648)). The i386 lock's
`[tool.verilator]` moves from `5e4151e3`, an untagged 5.051 development
snapshot, to the v5.054 release. The i386 simulation runners now take
Verilator only from that lock's authenticated toolchain slot. No RTL changed.

**Result.** The default RTL and netlist lockstep suites pass with the locked
v5.054 and give the same reports as system Verilator 5.032 on the same tree.
Every field is equal, every trace hash included; only the tool-identity
fields differ. No build uses `-Wno-fatal` any more. Each locked build
finishes with zero warnings under the waivers listed below.

## Tools

| Tool | Version | Where |
| --- | --- | --- |
| Verilator, locked | v5.054 (`bbb60a403932a9f695cb6ff001ceb73de6ad9234`), `Verilator 5.054 2026-11-01 rev v5.054`, `bin/verilator` sha256 `d74a5f9c…` | i386 lock slot `44e245aa…`, OFF lane (`make toolchain-fes-i386`) |
| Verilator, baseline | `Verilator 5.032 2025-01-01 rev (Debian 5.032-1+b2)` | `/usr/bin/verilator` |
| Yosys (netlist runs, both) | i386 lock `238eac9a` | slot `053d3998…`, the slot of the previous lock. The new slot's Yosys, the same commit rebuilt, writes the same netlist: sha256 `449aa0ee…` from both. |
| Oracle, corpus | Bochs 3.1 level 3 (`fes-i386-oracle`), SingleStepTests/80386 `459d49fbe628` | `build/sim` |

The tree is `65ec1f22a` (main). The baseline runs used the runner as it is on
main (`--verilator /usr/bin/verilator`). The locked runs used this PR's runner
(`--cache-root`). The RTL is the same in both.

## Commands

```
# baseline
python3 scripts/test_fes_i386_lockstep.py --verilator /usr/bin/verilator --jobs 12
python3 scripts/test_fes_i386_lockstep.py --verilator /usr/bin/verilator --yosys <slot 053d…>/install/bin/yosys --jobs 12
# locked
make toolchain-fes-i386 CACHE_ROOT=/abs/out/cache/misteross-toolchains
python3 scripts/test_fes_i386_lockstep.py --cache-root /abs/out/cache/misteross-toolchains --jobs 12
python3 scripts/test_fes_i386_lockstep.py --cache-root /abs/out/cache/misteross-toolchains --yosys <slot 053d…>/install/bin/yosys --jobs 12
# comparison
python3 -I compare_reports.py reports/rtl-system-5.032.json reports/rtl-locked-5.054.json
python3 -I compare_reports.py reports/netlist-system-5.032.json reports/netlist-locked-5.054.json
```

`compare_reports.py` walks both reports and leaves out only the top-level
`verilator` and `yosys` fields. It lists every other difference: counts,
cycles, faults, divergences and hashes. It also collects every `*hash*`
field: the seven case-mode `trace_hash`es, and the corpus, random and
protected-mode `trace_hashes` maps.

| Suite | Result, system 5.032 | Result, locked 5.054 | Comparison |
| --- | --- | --- | --- |
| RTL lockstep (default) | passed | passed | 0 differences; 11 of 11 hash fields equal |
| Netlist lockstep (default) | passed | passed | 0 differences; 11 of 11 hash fields equal |

The full reports are in [`reports/`](reports/). The locked reports carry the
new `verilator` record (lock, commit, sha256, version, slot key).

The front-end checks (`test_fes_i386_frontend.py`, RTL and `--yosys`) also
pass with the locked Verilator, with zero warnings in all six builds. So do the
latency bench and the governor bench. `test_fes_i386_test386.py --cpu rtl`
gives the same `rtl-report.json` under the locked v5.054 and system 5.032,
apart from the `verilator` field. On both, the run passes POST 21h and stops
at the unbuilt `IRETD` at `F2D53h`.

## Warnings

| Build | Flags before | Flags now | Warnings with 5.054 |
| --- | --- | --- | --- |
| CPU RTL (lockstep, latency, governor, test386) | `-Wall -Wno-UNUSEDSIGNAL` | unchanged | 0 |
| BIU DMA bench | `-Wall -Wno-fatal` | `-Wall -Wno-UNUSEDSIGNAL` | 0 |
| Front-end RTL benches | `-Wall -Wno-UNUSEDSIGNAL` | unchanged | 0 |
| Yosys netlists (lockstep, front end) | `-Wall -Wno-UNUSEDSIGNAL -Wno-fatal` | `-Wall -Wno-UNUSEDSIGNAL -Wno-CASEOVERLAP -Wno-UNDRIVEN -Wno-UNOPTFLAT -Wno-WIDTHEXPAND -Wno-WIDTHTRUNC --no-assert-case` | 0 |

- **BIU bench.** Without `-Wno-fatal`, the only warnings were two
  `UNUSEDSIGNAL`s in package functions that the BIU calls with part of their
  input (`fes_i386_pkg.sv:535` `seg_chk`, `:729` `str_reads`). The CPU build
  already waives that class.
- **Netlist builds.** The five waived classes are all that Verilator 5.032
  and 5.054 raise on Yosys's flat output of the CPU: 339 `CASEOVERLAP`, 3
  `UNDRIVEN`, 67 to 133 `UNOPTFLAT`, 119 `WIDTHEXPAND` and 258 `WIDTHTRUNC`.
  They come from Yosys's `$pmux` cases and bit-blasted assignments, not from
  the RTL, whose builds stay clean.
- **`--no-assert-case`.** v5.054 checks case assertions by default, which
  5.032 did not. Yosys writes every `$pmux` as a `casez` marked
  `// synopsys parallel_case`. The first netlist run without the flag stopped
  at `fes_i386_cpu.netlist.v:67540`: "synthesis parallel_case, but multiple
  matches found for '8'h18'". That pmux feeds `dc_next[191]`, from
  `fes_i386_seq`'s `case (d.seq)`. The two hot selects are
  `dc[99:95] == 5'h07` and `dc[99:95] inside {5'h09, 5'h0a}`, so their
  settled values cannot both be 1. They overlap only transiently, while
  Verilator settles the netlist's combinational loops (the `UNOPTFLAT`
  warnings). The RTL builds keep case assertions on.
- **Spike lint** (`tests/test_fes_i386_frontend.py`,
  `test_spikes_and_their_rtl_lint`). Under 5.032 all five spikes lint clean
  with `-Wall -Wno-UNUSEDSIGNAL`. Under 5.054 they raise two classes that
  5.032 does not have. `MULTIDRIVEN` comes from `spike_stimulus.sv:17`'s
  `initial reset = 1'b1` beside its `always_ff`. `PROCASSINIT` comes from
  `spike_rom.sv:12`'s `logic [7:0] a = 8'd1` and the stimulus's declaration
  initializers. Both are the envelopes' FPGA power-up values. The test waives
  those two classes by name; the spike sources are unchanged, so the
  benchmark fingerprints do not move. Nothing else is waived, and the CPU RTL
  raises neither class.

## Before this slice

- Every slot built from the lock already built Verilator, and its
  `ready.json` recorded it. No consumer read it: the i386 runners took
  `verilator` from `PATH`, or from `--verilator`/`VERILATOR`, and the
  lockstep, front-end and test386 runners refused the shared cache.
- The previous pin, `5e4151e3` (`Verilator 5.051 devel rev
  v5.050-268-g5e4151e3`), stopped with an internal optimizer error at
  `fes_i386_seq.sv:317` ([i386 CS limit
  record](../2026-11-02-i386-cs-limit/summary.md)).
