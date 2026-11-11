# #1660: the pc386 lock's Verilator moves to v5.054

`sources/misteross/cores/fes-pc386/toolchain.lock` moves `[tool.verilator]` from
`5e4151e3` to v5.054 `bbb60a40`. That is the i386 lock's pin since #1653 (#1581 S3). The
old pin was an untagged 5.051 snapshot that stops with an internal error at
`fes_i386_seq.sv:317`.

The same change corrects two stale rationales:

- `[tool.yosys]` said the i386 lock "should follow". It now pins the same `0a7927bf`
  (#1581 D2, #1664).
- `[tool.nextpnr]` cited `scripts/build_fes_pc386_oss.py`, which M5 (#1268) deleted. It
  now names the M4 record and says nextpnr routes of pc386 are parked (#1235).

The Yosys, Mistral, nextpnr and openFPGALoader pins don't change. The lock's sha256 goes
from `2ea51377…` to `2f08dd9b…`.

## Why this needs evidence

The fes.pc386 producer (`scripts/build_fes_pc386_quartus.py`) derives the BUILD_ID from its
build record. Three things in that record move with this change:

- `toolchain_lock_sha256`, the lock's digest.
- The Yosys identity, which includes the slot binary's sha256. The new lock builds a new
  slot, and slot builds aren't bit-reproducible.
- `revision`, the commit. This one moves with every commit.

The rule in `AGENTS.md` (#1581, D12) says a closure-only BUILD_ID change needs no reseal or
HIL when the PR shows three things:

1. The netlist is byte-identical to main apart from the BUILD_ID constant.
2. A build with the PR's tooling and main's BUILD_ID reproduces main's RBF byte for byte.
3. The PR's own build passes the normal timing and seal gates.

This record shows all three.

## Who reads the Verilator pin

Nothing in a pc386 build or simulation reads it:

- The producer takes only Yosys from the lock's slot.
- The pc386 simulations take `$(VERILATOR)`, which is the system binary unless set (5.032
  on Powerboat). The Makefile passes it to `sim_fes_pc386.py --verilator`, and the script
  defaults to `$VERILATOR` or `verilator`. That includes the i386 CPU the machine and
  system simulations compile.
- Only the standalone i386 tests (`scripts/test_fes_i386_*.py`, through
  `scripts/i386_verilator.py`) take Verilator from a slot, the i386 lock's.

The change keeps this lock's slot on the same Verilator as the i386 lock.

`make sim-fes-pc386` with the slot's v5.054 doesn't build yet (#1678). v5.054's new lints
stop it in two places:

- **`devices` suite:** 4 × PROCASSINIT in `pc386_ata.sv` and `pc386_rtc.sv`.
- **`audio` suite:** with PROCASSINIT off, `devices` and `storage` pass, and CASEINCOMPLETE
  and MULTIDRIVEN in the vendored OPL2 stop `audio`.

#1678 is the work needed before the pc386 simulations can take the locked Verilator. On
this branch, the same suites (`devices`, `storage`, `audio`, `opl2` and `benches`) pass
with the system 5.032.

## Runs

All three builds ran on Powerboat at seed 1, with Quartus Lite 17.0.2 from
`cores/fes-pc386/quartus.lock` (`quartus_sh` sha256 `71035cc1…`). They used the same
checkout path, `~/fes-scratch/p1660/wt`, so every relative path in the project matches.

| Run | Tree | Lock | Slot (HIP) | Yosys `0a7927bf` binary | BUILD_ID |
| --- | --- | --- | --- | --- | --- |
| main | `d56f0e558` (main) | `2ea51377…` | `544f7f8c…` | `2f204840…` | `4946e8584c9093a86d3e6ae0d029770c` |
| PR | `83622e34a` (this branch) | `2f08dd9b…` | `05416979…` | `1c1fb5cd…` | `ff4ce1dfbfa3955f92f5f34dea7c738a` |
| forced | `83622e34a` | `2f08dd9b…` | `05416979…` | `1c1fb5cd…` | `4946e858…` (main's) |

**The new slot.** `0541697912462d855f737dd27ed3680fff37e571e293fe0ad37a6abe03158bd0` was
built by `scripts/bootstrap.sh` with the environment `make toolchain-fes-pc386` sets (HIP,
`gfx1100;gfx1201`). `FES_TOOLCHAIN_LOCKFILE` pointed at a byte-identical copy of the
committed lock. Its `ready.json` records the lock sha256 `2f08dd9b…`, and its Verilator
reports `5.054 2026-11-01 rev v5.054`.

**main and PR** are `make build-fes-pc386-quartus CACHE_ROOT=… SEED=1` on clean trees.

**forced** is `forced_build_id.py`, the committed script, run at `83622e34a`. It makes the
producer's `build()` steps with main's BUILD_ID in place of the record's:

- the clean-source check and tool authentication;
- the project, firmware ROM, Yosys and Quartus steps and the evidence checks;
- afterwards, the same source and tool identity checks.

It writes no build record or manifest and exports no package.

**What differs between main and PR.** `83622e34a` has main merged in at `9b80dd9d5`, which
adds D2 (#1664) and #1659 to `d56f0e558`. In the two build summaries, the
`inputs` digests (every producer pinned input) differ only in
`cores/fes-pc386/toolchain.lock`. The other files the merge changes are:

- the i386 lock and README;
- `i386-cpu-design.md`, the support matrix and the D2 record;
- the Z80 sealers, with their new `quartus_timing_summary.py` and its test fixture.

None of those is read by this producer.

**Earlier round.** A first round at `78cd7bd22` used an earlier wording of the lock (sha256
`81a47c25…`) and its own slot, `ec95e218…`. That commit didn't yet have D2 merged in. That
round reached the same results: the same netlist, and main's RBF from the forced run. The
lock wording was then corrected and everything above was rerun, so only the second round's
summaries are kept.

## Results

**1. Netlist.** All three runs write the same machine netlist, `project/pc386_system.v`:
5,661,734 bytes, sha256
`0513c812f07f3d8a0387abd800d3bef9d1cf6f447d47d5bb9bc98a205892fc2e`.

The BUILD_ID isn't in the netlist: the shell reads it as the `FES_PC386_BUILD_ID` Verilog
macro from the QSF. The PR's `top.qsf` differs from main's only in that line. The forced
run's `top.qsf` is identical to main's.

**2. Main's BUILD_ID with the PR's tooling.** The forced run's `core.rbf` is byte-identical
to main's: 3,866,592 bytes, sha256
`4f69408c0974f60670ae5d46f2dde5b3b11d74dd96e9e4cce5ac597e16191a2d`. Its timing and
resource figures match main's exactly.

**3. The PR's own build** passes the producer's gates:

| | main | PR |
| --- | --- | --- |
| Setup worst slack | +0.753 ns | +0.803 ns |
| Hold | +0.077 ns | +0.076 ns |
| Recovery | +7.097 ns | +6.880 ns |
| Removal | +0.577 ns | +0.275 ns |
| Min pulse width | +1.122 ns | +1.122 ns |
| TNS (all analyses) | 0 | 0 |
| Clocks system / pixel / audio | 61.44 / 74.27 / 12.29 MHz | same |
| ALMs | 36,002 | 36,123 |
| Registers | 32,413 | 32,470 |
| RAM blocks / DSPs | 268 / 38 | 268 / 38 |
| RBF | 3,866,592 B, `4f69408c…` | 3,846,716 B, `25684871…` |

The PR build's RBF and resource counts differ from main's because Quartus maps a different
BUILD_ID constant differently. D12 says that alone needs no reseal.

This build's BUILD_ID belongs to `83622e34a`. Each later commit, the squash merge included,
has a new `revision` and so a new BUILD_ID, as every pc386 change does.

## What else this change touches

- **Each host** must rebuild the pc386 HIP slot (`make toolchain-fes-pc386 CACHE_ROOT=…`),
  because the slot key hashes the lock. The old slots `544f7f8c…` (HIP) and `9092d69c…`
  (OFF) stay for older trees.
- **The design doc** (`docs/design/rtl-frontend-lanes.md:70`, `:607`, `:627`) now says
  the stale rationale is fixed and both locks pin v5.054.

## Files

- `main-build-summary.json`, `pr-build-summary.json`: the producer's `build-summary.json`
  for each run. They hold the timing, resources, RBF, pinned-input digests and tools.
- `forced-build-summary.json`: the forced run's evidence. It holds the BUILD_ID, revision,
  tools, timing, resources and RBF.
- `forced_build_id.py`: the forced run.
