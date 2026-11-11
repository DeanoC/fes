# #1660: the pc386 lock's Verilator moves to v5.054

`sources/misteross/cores/fes-pc386/toolchain.lock` moves `[tool.verilator]` from
`5e4151e3` to v5.054 `bbb60a40`. That is the i386 lock's pin since #1653 (#1581 S3). The
old pin was an untagged 5.051 snapshot that stops with an internal error at
`fes_i386_seq.sv:317`. The same change corrects two stale rationales:

- `[tool.yosys]` said the i386 lock "should follow". It now pins the same `0a7927bf`
  (#1581 D2, #1664).
- `[tool.nextpnr]` cited `scripts/build_fes_pc386_oss.py`, which M5 (#1268) deleted. It
  now names the M4 record and says nextpnr routes of pc386 are parked (#1235).

The Yosys, Mistral, nextpnr and openFPGALoader pins don't change. The lock's sha256 goes
from `2ea51377…` to `81a47c25…`.

## Why this needs evidence

The fes.pc386 producer (`scripts/build_fes_pc386_quartus.py`) writes
`toolchain_lock_sha256` into its build record. So any byte change to this lock moves the
fes.pc386 BUILD_ID, and the slot key moves with it. The rule in `AGENTS.md` (#1581, D12)
says a closure-only BUILD_ID change needs no reseal or HIL when the PR shows three things:

1. The netlist is byte-identical to main apart from the BUILD_ID constant.
2. A build with the PR's tooling and main's BUILD_ID reproduces main's RBF byte for byte.
3. The PR's own build passes the normal timing and seal gates.

This record shows all three.

## Who reads the Verilator pin

Nothing in a pc386 build or simulation:

- The producer takes only Yosys from the lock's slot.
- `sim_fes_pc386.py` takes `$(VERILATOR)`, the system binary by default (5.032 on
  Powerboat).
- The i386 parts take Verilator from the i386 lock's slot (`scripts/i386_verilator.py`).

The change keeps this lock's slot on the same Verilator as the i386 lock.

`make sim-fes-pc386` with the new slot's v5.054 doesn't build yet. v5.054's new lints
stop it:

- 4 × PROCASSINIT in `pc386_ata.sv` and `pc386_rtc.sv`.
- With that lint off, CASEINCOMPLETE and MULTIDRIVEN in the vendored OPL2 stop the `audio`
  suite.

That is #1678, the work needed before the pc386 sims can take the locked Verilator. On
this branch, the same suites pass with the system 5.032 (`devices`, `storage`, `audio`,
`opl2`, `benches`).

## Runs

All three builds ran on Powerboat, in the same checkout path, at seed 1. They used Quartus
Lite 17.0.2 from `cores/fes-pc386/quartus.lock`.

| Run | Tree | Lock | Slot (HIP) | Yosys sha256 | BUILD_ID |
| --- | --- | --- | --- | --- | --- |
| main | `d56f0e558` (main) | `2ea51377…` | `544f7f8c…` | `2f204840…` | `4946e8584c9093a86d3e6ae0d029770c` |
| PR | `78cd7bd22` (main + the lock edit) | `81a47c25…` | `ec95e218…` | `7cb7c295…` | `143e2da3eb52bfea56023ef3e880ef7b` |
| forced | `78cd7bd22` | `81a47c25…` | `ec95e218…` | `7cb7c295…` | `4946e858…` (main's) |

- **Commits:** both slots build Yosys `0a7927bf`, and their binaries differ because slot
  builds aren't bit-reproducible.
- **New slot:** `ec95e218de8347a5ddf38f8db136bfb88b6a55c0ed9281b0b969453ca5eef3a4` was
  built by `scripts/bootstrap.sh` with the environment `make toolchain-fes-pc386` sets
  (HIP, `gfx1100;gfx1201`). `FES_TOOLCHAIN_LOCKFILE` pointed at a byte-identical copy of
  the edited lock. Its Verilator reports `5.054 2026-11-01 rev v5.054`.
- **main and PR** are `make build-fes-pc386-quartus CACHE_ROOT=… SEED=1` on clean trees.
- **forced** is `forced_build_id.py`. It runs the producer's own steps, writing to the
  producer's output path, with main's BUILD_ID in the QSF. It exports no package. The run
  used a copy of the script with the two paths written in.
- **Merging main:** after these runs the branch merged main, which brought D2 (#1664) and
  #1659. Those change the i386 README and lock and the Z80 sealers, none of which is a
  pc386 pinned input. The merge commit changes the build record's `revision` and so the
  BUILD_ID, as every commit does.

## Results

**1. Netlist.** All three runs write the same machine netlist,
`project/pc386_system.v`: 5,661,734 bytes, sha256
`0513c812f07f3d8a0387abd800d3bef9d1cf6f447d47d5bb9bc98a205892fc2e`. The BUILD_ID isn't in
it, because the shell reads it as the `FES_PC386_BUILD_ID` Verilog macro from the QSF. The
PR's `top.qsf` differs from main's only in that line. The forced run's `top.qsf` is
identical to main's.

**2. Main's BUILD_ID with the PR's tooling.** The forced run's `core.rbf` is byte-identical
to main's: 3,866,592 bytes, sha256
`4f69408c0974f60670ae5d46f2dde5b3b11d74dd96e9e4cce5ac597e16191a2d`. Its timing and
resource figures match main's exactly.

**3. The PR's own build** passes the producer's gates:

| | main | PR |
| --- | --- | --- |
| Setup worst slack | +0.753 ns | +0.641 ns |
| Hold | +0.077 ns | +0.069 ns |
| Recovery | +7.097 ns | +7.157 ns |
| Removal | +0.577 ns | +0.393 ns |
| Min pulse width | +1.122 ns | +1.122 ns |
| TNS (all analyses) | 0 | 0 |
| Clocks system / pixel / audio | 61.44 / 74.27 / 12.29 MHz | same |
| ALMs | 36,002 | 36,168 |
| Registers | 32,413 | 32,309 |
| RAM blocks / DSPs | 268 / 38 | 268 / 38 |
| RBF | 3,866,592 B, `4f69408c…` | 3,853,192 B, `8ee52719…` |

The PR build's RBF and resource counts differ from main's because Quartus maps a different
BUILD_ID constant differently. D12 says that alone needs no reseal.

## What else this change touches

- **Each host** must rebuild the pc386 HIP slot (`make toolchain-fes-pc386 CACHE_ROOT=…`).
  The slot key hashes the lock. The old slots `544f7f8c…` (HIP) and `9092d69c…` (OFF) stay
  for older trees.
- **The design doc** (`docs/design/rtl-frontend-lanes.md:70`, `:607`) now says the stale
  rationale is fixed.

## Files

- `main-build-summary.json`, `pr-build-summary.json`: the producer's `build-summary.json`
  for each run (timing, resources, RBF, pinned-input digests, tools).
- `forced-build-summary.json`: the forced run's evidence (BUILD_ID, tools, timing,
  resources, RBF).
- `forced_build_id.py`: the forced run.
