# M0/M1 Open MiSTer OSS Toolchain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a pinned repository-local Cyclone V OSS toolchain and use it to generate, program, and hardware-validate a one-LED blinky RBF for the DE10-Nano.

**Architecture:** A Makefile exposes stable `toolchain`, `doctor`, `sim`, `oss`, `oracle`, `compare`, and `program` entry points. Python modules handle lock validation, diagnostics, manifests, and comparisons; focused shell wrappers perform tool builds and FPGA compilation. Simulation, OSS, and Quartus oracle lanes share RTL and board constraints but remain dependency-isolated.

**Tech Stack:** GNU Make, Bash, Python 3 standard library, Verilog, Verilator, Yosys `synth_intel_alm`, nextpnr-mistral, Mistral, openFPGALoader, optional Quartus Prime Lite 17.0.2.

**Spec:** `docs/superpowers/specs/2026-08-26-m0-m1-open-toolchain-design.md`

## Global Constraints

- Target exactly Cyclone V SoC `5CSEBA6U23I7`, package UFBGA 672, speed grade 7, on Terasic DE10-Nano/MiSTer.
- The OSS lane must never invoke, link to, discover as a fallback, or shell out to Quartus or another proprietary Intel/Altera executable.
- Build FPGA tools from pinned source into `build/toolchain/`; host packages provide ordinary build prerequisites only.
- Bootstrap is idempotent and reports missing prerequisites without installing them silently.
- Blinky uses only `FPGA_CLK1_50`, a fabric counter, and `LED[0]`; no PLL, BRAM/M10K, LUTRAM, DSP, HPS, SDRAM, video, audio, or MiSTer framework code.
- Every build preserves commands and logs; no build target automatically programs hardware.
- Hardware programming is volatile only and must not write flash, HPS storage, or an SD card.
- Quartus is optional for OSS and simulation work, but M1 is not complete until an exact Quartus Prime Lite 17.0.2 oracle build is captured.
- A failure unique to the OSS lane is reduced to a permanent experiment before any upstream-tool patch is attempted.

## File Responsibility Map

- `Makefile`: stable public command interface and experiment selection.
- `.gitignore`: generated tool sources, installations, FPGA artifacts, simulator outputs, and local proprietary paths.
- `README.md`: mission, purity rule, target, current milestone, and quick start.
- `toolchain.lock`: machine-readable repository URLs, exact commits, build order, and pin rationale.
- `scripts/lockfile.py`: parse and validate `toolchain.lock`; emit shell-safe fields for bootstrap.
- `scripts/bootstrap.sh`: prerequisite check, pinned checkout, build, install, and idempotence stamps.
- `scripts/env.sh`: define repository-local paths without probing system FPGA tools.
- `scripts/doctor.py`: non-mutating human and JSON readiness reports.
- `scripts/run_logged.sh`: run one command with a preserved log and readable failure summary.
- `scripts/build_oss.sh`: lint, synthesize, place/route, emit RBF, and collect OSS artifacts.
- `scripts/build_oracle.sh`: explicit Quartus-only compilation and artifact collection.
- `scripts/collect_manifest.py`: deterministic build manifest construction and artifact hashing.
- `scripts/compare_builds.py`: concise JSON/Markdown comparison without requiring identical resources or RBF bytes.
- `scripts/program.py`: artifact, manifest, cable, and board preflight followed by explicit volatile programming.
- `tests/`: Python standard-library tests for repository interfaces and scripts.
- `boards/de10nano/pins.qsf`: only clock V11 and LED0 W15 assignments.
- `boards/de10nano/clocks.sdc`: 50 MHz input constraint.
- `boards/de10nano/README.md`: pin provenance and safe board connection/programming guide.
- `experiments/010_blinky/rtl/top.v`: parameterized counter-to-LED RTL.
- `experiments/010_blinky/sim/tb.cpp`: short deterministic Verilator test using a reduced counter width.
- `experiments/010_blinky/oracle/top.qpf` and `top.qsf`: minimal Quartus project reusing the shared RTL and constraints.
- `experiments/010_blinky/expected.md`: expected simulation and physical LED behavior.
- `docs/bringup-log.md`: exact CLI/device findings and signed hardware observations.
- `docs/architecture.md`, `docs/oracle-method.md`, `docs/upstream-findings.md`: stable project policy and discovered tool limitations.

---

### Task 1: Repository Contract and Public Command Surface

**Files:**
- Create: `.gitignore`
- Create: `README.md`
- Create: `Makefile`
- Create: `tests/test_repository_contract.py`
- Create: `docs/architecture.md`
- Create: `docs/oracle-method.md`
- Create: `docs/bringup-log.md`
- Create: `docs/upstream-findings.md`

**Interfaces:**
- Consumes: approved design specification.
- Produces: `make help`, validated experiment names, and directories used by all later tasks.

- [ ] **Step 1: Write the failing repository-contract tests**

Create `tests/test_repository_contract.py` with `unittest` cases that run from the repository root:

```python
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class RepositoryContractTests(unittest.TestCase):
    def test_help_lists_public_targets(self):
        result = subprocess.run(
            ["make", "help"], cwd=ROOT, text=True, capture_output=True, check=True
        )
        for target in ("toolchain", "doctor", "sim", "oss", "oracle", "compare", "program"):
            self.assertIn(target, result.stdout)

    def test_unknown_experiment_is_rejected(self):
        result = subprocess.run(
            ["make", "sim", "EXP=../../tmp"], cwd=ROOT, text=True, capture_output=True
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid EXP", result.stderr + result.stdout)

    def test_generated_directories_are_ignored(self):
        result = subprocess.run(
            ["git", "check-ignore", "build/oss/010_blinky/top.rbf"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run the contract tests and verify failure**

Run: `python3 -m unittest tests.test_repository_contract -v`

Expected: failures because `Makefile` and `.gitignore` do not exist.

- [ ] **Step 3: Create the minimal repository surface**

Create a `Makefile` whose first target is `help`, whose experiment guard accepts only `^[0-9][0-9][0-9]_[a-z0-9_]+$`, and whose not-yet-implemented targets fail with `target not implemented in this task` rather than silently succeeding. Define `EXP ?= 010_blinky`, `BUILD ?= oss`, and `PYTHON ?= python3` once.

Create `.gitignore` entries for:

```gitignore
/build/
/third_party/
/.cache/
__pycache__/
*.pyc
*.vcd
*.fst
*.log
*.json
*.rbf
*.sof
*.smsg
*.rpt
*.qws
db/
incremental_db/
output_files/
```

Use negated rules only when a later checked-in fixture genuinely needs an otherwise ignored extension.

Write the concise README and four documentation stubs with concrete project policy from the spec. `docs/bringup-log.md` begins with a dated host survey showing that CMake, Ninja, Python, and Git were present while FPGA tools and a USB-Blaster were not detected on 2026-08-26. `docs/upstream-findings.md` starts with `No upstream tool defects recorded.`

- [ ] **Step 4: Run the contract tests and inspect help**

Run: `python3 -m unittest tests.test_repository_contract -v && make help`

Expected: three tests pass; help shows every public target and the `EXP`/`BUILD` variables.

- [ ] **Step 5: Commit the repository contract**

```bash
git add .gitignore README.md Makefile tests docs/architecture.md docs/oracle-method.md docs/bringup-log.md docs/upstream-findings.md
git commit -m "chore: establish M0 repository contract"
```

### Task 2: Typed Toolchain Lock

**Files:**
- Create: `toolchain.lock`
- Create: `scripts/lockfile.py`
- Create: `tests/test_lockfile.py`

**Interfaces:**
- Consumes: Python 3.11+ `tomllib` and the five verified upstream HEAD values sampled on 2026-08-26.
- Produces: `load_lock(path: Path) -> dict[str, ToolPin]`, `validate_lock(path: Path) -> list[str]`, and CLI `python3 scripts/lockfile.py get TOOL FIELD`.

- [ ] **Step 1: Write failing lock validation tests**

Test that all five tools exist, every commit is exactly 40 lowercase hexadecimal characters, URLs use HTTPS, build order places `mistral` before `nextpnr`, and `get nextpnr commit` prints only its SHA. Use a temporary malformed TOML file to verify that a symbolic commit such as `main` is rejected.

```python
class LockfileTests(unittest.TestCase):
    def test_checked_in_lock_is_complete(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertEqual(set(lock), {"yosys", "mistral", "nextpnr", "verilator", "openfpgaloader"})
        for pin in lock.values():
            self.assertRegex(pin.commit, r"^[0-9a-f]{40}$")
            self.assertTrue(pin.repo.startswith("https://github.com/"))

    def test_mistral_precedes_nextpnr(self):
        lock = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertLess(lock["mistral"].order, lock["nextpnr"].order)
```

- [ ] **Step 2: Run tests and verify import/file failures**

Run: `python3 -m unittest tests.test_lockfile -v`

Expected: failure because `scripts.lockfile` and `toolchain.lock` do not exist.

- [ ] **Step 3: Implement the TOML lock and parser**

Use these initial immutable pins, each captured from its upstream `HEAD` on 2026-08-26:

```toml
[tool.yosys]
repo = "https://github.com/YosysHQ/yosys.git"
commit = "13b43f8c85ec430a33ee55d058fb4c32b42b6910"
order = 10
rationale = "Current upstream Intel ALM synthesis baseline sampled 2026-08-26."

[tool.mistral]
repo = "https://github.com/Ravenslofty/mistral.git"
commit = "d509238a203aadbb76291ab06543a401df91cf54"
order = 20
rationale = "Current Cyclone V database/compiler baseline sampled 2026-08-26."

[tool.nextpnr]
repo = "https://github.com/YosysHQ/nextpnr.git"
commit = "7d4f72c0aabc15da932748a54e82a6ff7b41921e"
order = 30
rationale = "Current Mistral backend baseline sampled 2026-08-26."

[tool.verilator]
repo = "https://github.com/verilator/verilator.git"
commit = "5e4151e3e0c8ecf11d9845a93495f37a31b2f667"
order = 40
rationale = "Current lint and simulation baseline sampled 2026-08-26."

[tool.openfpgaloader]
repo = "https://github.com/trabucayre/openFPGALoader.git"
commit = "0c5ebaab1fa63c9d9c684abc0b8e68546ea8ea86"
order = 50
rationale = "Current DE10-Nano programmer baseline sampled 2026-08-26."
```

Implement `ToolPin` as a frozen dataclass. Reject unknown top-level keys, missing fields, non-HTTPS URLs, duplicate order values, and non-SHA commits. Make CLI failures exit 2 with one-line messages and no traceback.

- [ ] **Step 4: Run lock tests and CLI checks**

Run:

```bash
python3 -m unittest tests.test_lockfile -v
python3 scripts/lockfile.py validate
python3 scripts/lockfile.py get nextpnr commit
```

Expected: tests pass; validation prints `toolchain.lock: valid`; the final command prints `7d4f72c0aabc15da932748a54e82a6ff7b41921e`.

- [ ] **Step 5: Commit the lock contract**

```bash
git add toolchain.lock scripts/lockfile.py tests/test_lockfile.py
git commit -m "build: pin initial open FPGA toolchain"
```

### Task 3: Idempotent Bootstrap and Repository-Local Environment

**Files:**
- Create: `scripts/bootstrap.sh`
- Create: `scripts/env.sh`
- Create: `tests/test_bootstrap_interface.py`
- Modify: `Makefile`
- Modify: `README.md`

**Interfaces:**
- Consumes: `python3 scripts/lockfile.py get TOOL FIELD`.
- Produces: `build/toolchain/src/TOOL`, `build/toolchain/build/TOOL`, `build/toolchain/install/bin`, per-tool `.built-COMMIT` stamps, and `make toolchain`.

- [ ] **Step 1: Write failing dry-run and environment tests**

Tests run `scripts/bootstrap.sh --check-prereqs` and `--print-plan` without cloning. Assert the plan contains all five exact SHAs, Mistral precedes nextpnr, and no `apt`, `dnf`, `pacman`, or `sudo` command is executed. Source `scripts/env.sh` in a clean Bash process and assert the first PATH entry is `$ROOT/build/toolchain/install/bin`.

- [ ] **Step 2: Run tests and verify missing-script failures**

Run: `python3 -m unittest tests.test_bootstrap_interface -v`

Expected: failure because bootstrap and environment scripts do not exist.

- [ ] **Step 3: Implement prerequisite reporting and dry-run plan**

Use `set -euo pipefail`, resolve the repository root from the script location, and define separate source, build, and install roots. `--check-prereqs` checks commands plus development headers using small compile/CMake probes. Its failure output groups Debian/Ubuntu package suggestions without running them. Include at least Git, C/C++ compiler, CMake, Ninja, Make, Python development headers, Boost, Eigen3, libffi, readline, Tcl, zlib, liblzma, libusb-1.0, libftdi1, pkg-config, autoconf, flex, bison, and help2man.

`--print-plan` prints tool, repository, commit, source directory, build directory, and install prefix in numeric lock order and exits without network or filesystem mutation.

- [ ] **Step 4: Implement pinned checkout/build functions**

Implement `checkout_pin TOOL` so an absent source is cloned with `--filter=blob:none --no-checkout`, the exact commit is fetched explicitly, detached checkout is performed, submodules are synchronized/updated when present, and `git rev-parse HEAD` must equal the lock. An existing mismatched or dirty checkout causes a readable stop; it is never reset or deleted automatically.

Implement one focused build function per tool:

- Yosys: upstream Makefile with `PREFIX=$INSTALL_ROOT`, followed by `make install`.
- Mistral: CMake/Ninja Release build with install prefix.
- nextpnr: CMake/Ninja with `-DARCH=mistral`, `-DMISTRAL_ROOT=$SRC_ROOT/mistral`, and the shared install prefix.
- Verilator: upstream autoconf flow with the shared prefix.
- openFPGALoader: CMake/Ninja Release build with the shared prefix.

Before marking a stamp, run each installed binary's version/help command and write it beside the stamp. A matching stamp plus successful binary identity check skips rebuilding.

- [ ] **Step 5: Wire the environment and Make target**

`scripts/env.sh` must be sourceable, export `OPEN_MISTER_ROOT`, prepend only `build/toolchain/install/bin`, and add repository-local library/pkg-config paths. It must not search for or export Quartus.

Change `make toolchain` to execute `scripts/bootstrap.sh`. Add `make toolchain-check` for `--check-prereqs`.

- [ ] **Step 6: Run interface tests and prerequisite check**

Run:

```bash
python3 -m unittest tests.test_bootstrap_interface -v
make toolchain-check
scripts/bootstrap.sh --print-plan
```

Expected: unit tests pass. If prerequisites are missing, the check names exact missing capabilities and exits nonzero without changing the host; install them only as an explicit implementation action within project scope, then rerun until it passes.

- [ ] **Step 7: Build the pinned toolchain and verify idempotence**

Run `make toolchain`, capture elapsed output, then run `make toolchain` again.

Expected: the first run builds five pinned tools; the second validates identities and reports each as already built. If a pinned upstream combination does not build, preserve the log, make the smallest compatibility correction, update the lock rationale and tests, and commit that evidence rather than switching to moving branches.

- [ ] **Step 8: Commit bootstrap**

```bash
git add Makefile README.md scripts/bootstrap.sh scripts/env.sh tests/test_bootstrap_interface.py toolchain.lock
git commit -m "build: bootstrap pinned local FPGA tools"
```

### Task 4: Non-Mutating Doctor and Current CLI/Device Evidence

**Files:**
- Create: `scripts/doctor.py`
- Create: `tests/test_doctor.py`
- Modify: `Makefile`
- Modify: `docs/bringup-log.md`

**Interfaces:**
- Consumes: repository-local environment and optional `QUARTUS_ROOTDIR`.
- Produces: `make doctor`, `make doctor-strict`, `python3 scripts/doctor.py --json`, and a dated CLI/device evidence section.

- [ ] **Step 1: Write failing doctor tests with a fake tool directory**

Create temporary executable shims for required tools that return fixed version strings. Test these cases:

- general report exits 0 while showing absent Quartus as `OPTIONAL MISSING`;
- `--strict oss` exits nonzero when a required OSS binary is absent;
- `--json` contains `host`, `required_oss`, `optional_oracle`, `hardware`, and `device` keys;
- Quartus is inspected only when `QUARTUS_ROOTDIR` is set;
- device readiness requires evidence that nextpnr-mistral accepts `5CSEBA6U23I7`.

- [ ] **Step 2: Run doctor tests and verify failure**

Run: `python3 -m unittest tests.test_doctor -v`

Expected: import or command failure because doctor is absent.

- [ ] **Step 3: Implement structured diagnostics**

Represent each check as `{name, status, detail, required}`. Use `platform`, `shutil.which`, and `subprocess.run` with timeouts. Inspect CMake, Ninja, Make, compiler, Python, Yosys, Verilator, Mistral CLI tools, nextpnr-mistral, openFPGALoader, optional Quartus, `lsusb`, and JTAG/board listing. Never run a compiler or programmer operation that changes state.

General mode always returns 0 after rendering. `--strict oss` fails if a required host or OSS check fails. `--strict hardware` additionally requires one unambiguous DE10-Nano/USB-Blaster detection. JSON mode writes only JSON to stdout.

- [ ] **Step 4: Confirm actual Mistral/nextpnr/openFPGALoader syntax**

With `scripts/env.sh` sourced, run and save complete output for:

```bash
nextpnr-mistral --version
nextpnr-mistral --help
openFPGALoader --version
openFPGALoader --list-boards
mistral-cv --help
```

Use the pinned tools' own device-list or a minimal parse-only invocation to prove the exact accepted identifier for `5CSEBA6U23I7`. Record the exact QSF input, RBF output, routed-netlist, timing, cable-listing, and programming flags in `docs/bringup-log.md`. If executable names differ, update the doctor and lock rationale to the observed names and retain the raw help logs under `build/toolchain/evidence/`.

- [ ] **Step 5: Wire and verify doctor targets**

Run:

```bash
python3 -m unittest tests.test_doctor -v
make doctor
make doctor-strict
python3 scripts/doctor.py --json | python3 -m json.tool >/dev/null
```

Expected: tests pass, general report clearly separates three readiness groups, strict OSS readiness passes, and JSON parses.

- [ ] **Step 6: Commit diagnostics and evidence**

```bash
git add Makefile scripts/doctor.py tests/test_doctor.py docs/bringup-log.md
git commit -m "feat: report toolchain and device readiness"
```

### Task 5: Minimal Board Constraints and Simulated Blinky

**Files:**
- Create: `boards/de10nano/pins.qsf`
- Create: `boards/de10nano/clocks.sdc`
- Create: `boards/de10nano/README.md`
- Create: `experiments/010_blinky/rtl/top.v`
- Create: `experiments/010_blinky/sim/tb.cpp`
- Create: `experiments/010_blinky/expected.md`
- Create: `tests/test_blinky_sources.py`
- Modify: `Makefile`

**Interfaces:**
- Consumes: Verilator from repository-local environment.
- Produces: top module `top #(parameter integer COUNTER_BITS = 25) (input wire FPGA_CLK1_50, output wire [0:0] LED)` and `make sim EXP=010_blinky`.

- [ ] **Step 1: Write failing source-policy tests**

Test that the QSF contains exactly two location assignments, `PIN_V11` to `FPGA_CLK1_50` and `PIN_W15` to `LED[0]`; both ports use `3.3-V LVTTL`; the SDC contains a 20.000 ns clock; and RTL text does not contain PLL, RAM, HPS, DSP, or vendor primitive names. Also assert the board README cites both the Terasic DE10-Nano manual and MiSTer `sys.tcl` source URLs.

- [ ] **Step 2: Run source tests and verify missing-file failures**

Run: `python3 -m unittest tests.test_blinky_sources -v`

Expected: failure because the board and experiment files do not exist.

- [ ] **Step 3: Add authoritative minimal constraints**

Use these verified assignments:

```tcl
set_location_assignment PIN_V11 -to FPGA_CLK1_50
set_instance_assignment -name IO_STANDARD "3.3-V LVTTL" -to FPGA_CLK1_50
set_location_assignment PIN_W15 -to LED[0]
set_instance_assignment -name IO_STANDARD "3.3-V LVTTL" -to LED[0]
```

Use this timing intent:

```tcl
create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
```

Document that Terasic's DE10-Nano/DE0-Nano-SoC manual lists V11 as the 50 MHz clock and W15 as LED0, and MiSTer's maintained `MemTest_MiSTer/sys/sys.tcl` independently uses those assignments for exact device `5CSEBA6U23I7`.

- [ ] **Step 4: Write blinky RTL and deterministic testbench**

Implement a parameterized counter initialized to zero. On every `FPGA_CLK1_50` rising edge increment it, and continuously drive `LED[0]` from its most significant bit. The production default `COUNTER_BITS=25` gives a full LED cycle of approximately 0.671 seconds at 50 MHz.

The C++ testbench is built with `-GCOUNTER_BITS=4`; it clocks the design for 24 rising edges and asserts LED is low for counts 0-7, high for counts 8-15, and low again after wrap. A mismatch prints cycle, expected, and observed values and exits nonzero.

- [ ] **Step 5: Implement the simulation target**

For `010_blinky`, run Verilator lint first, then `verilator --cc --exe --build --top-module top -GCOUNTER_BITS=4` with the RTL and C++ testbench, placing generated output only under `build/sim/010_blinky/`. Run the resulting executable and preserve `verilator-lint.log`, `verilator-build.log`, and `simulation.log`.

- [ ] **Step 6: Run simulation and source-policy tests**

Run:

```bash
python3 -m unittest tests.test_blinky_sources -v
make sim EXP=010_blinky
```

Expected: all source tests pass; Verilator lint/build pass; simulation reports the two expected transitions and exits 0.

- [ ] **Step 7: Commit blinky simulation**

```bash
git add Makefile boards/de10nano experiments/010_blinky tests/test_blinky_sources.py
git commit -m "feat: add minimal simulated DE10-Nano blinky"
```

### Task 6: Logged Commands and Deterministic Build Manifests

**Files:**
- Create: `scripts/run_logged.sh`
- Create: `scripts/collect_manifest.py`
- Create: `tests/test_manifest.py`

**Interfaces:**
- Consumes: output directory, experiment, lane, target, source paths, command-log paths, and artifact paths.
- Produces: `run_logged.sh LOG COMMAND...` and stable `manifest.json` with SHA-256 values.

- [ ] **Step 1: Write failing manifest and log-runner tests**

Tests create temporary source/artifact files and assert:

- repeated manifest generation with fixed inputs is byte-identical;
- hashes are lowercase 64-character SHA-256 values;
- manifest records target `5CSEBA6U23I7`, lane, source hashes, artifact hashes, host, Git state, tool pins, and commands;
- a failing logged command preserves stdout/stderr and prints `failed command` plus the log path.

- [ ] **Step 2: Run tests and verify missing implementations**

Run: `python3 -m unittest tests.test_manifest -v`

Expected: failures because both scripts are absent.

- [ ] **Step 3: Implement the command runner and manifest collector**

`run_logged.sh` uses `set -o pipefail`, writes an escaped command header, combines stdout/stderr through `tee`, preserves the real command exit status, and emits a one-line failure summary to stderr.

`collect_manifest.py` uses only the standard library. Sort object keys and file lists. Store timestamps in UTC ISO 8601, but allow `SOURCE_DATE_EPOCH` in tests. Record dirty Git state explicitly instead of pretending a commit fully identifies uncommitted sources. Refuse missing artifacts and paths outside the repository/build roots.

- [ ] **Step 4: Run tests and inspect a fixture manifest**

Run: `python3 -m unittest tests.test_manifest -v`

Expected: all tests pass and temporary manifest JSON is deterministic.

- [ ] **Step 5: Commit artifact infrastructure**

```bash
git add scripts/run_logged.sh scripts/collect_manifest.py tests/test_manifest.py
git commit -m "feat: capture reproducible FPGA build evidence"
```

### Task 7: OSS Synthesis, Place-and-Route, and RBF Pipeline

**Files:**
- Create: `scripts/build_oss.sh`
- Create: `tests/test_oss_purity.py`
- Modify: `Makefile`
- Modify: `docs/bringup-log.md`

**Interfaces:**
- Consumes: experiment RTL/QSF/SDC, repository-local tools, confirmed Task 4 CLI flags, `run_logged.sh`, and `collect_manifest.py`.
- Produces: `build/oss/010_blinky/{yosys.log,synth.json,nextpnr.log,top.rbf,timing.txt,manifest.json}` plus routed JSON if supported.

- [ ] **Step 1: Write failing OSS purity and command-construction tests**

Test the script in `--print-commands` mode with a fake repository-local bin directory. Assert commands contain `synth_intel_alm -nobram -nolutram -nodsp -top top`, exact device `5CSEBA6U23I7`, shared QSF/SDC, and RBF output. Assert script text and printed commands contain none of `quartus`, `qsys`, `sopc`, `/opt/intel`, or `QUARTUS_ROOTDIR`. Assert an invalid experiment is rejected before any command runs.

- [ ] **Step 2: Run purity tests and verify failure**

Run: `python3 -m unittest tests.test_oss_purity -v`

Expected: failure because `build_oss.sh` is absent.

- [ ] **Step 3: Implement the exact pinned CLI pipeline**

Use Task 4's captured `--help` evidence rather than historical syntax. Build each command as a Bash array, print it with shell escaping, and execute through `run_logged.sh`. The Yosys program is:

```text
read_verilog experiments/010_blinky/rtl/top.v;
synth_intel_alm -nobram -nolutram -nodsp -top top;
stat;
write_json build/oss/010_blinky/synth.json
```

Pass the exact device, minimal QSF, and 50 MHz SDC to nextpnr-mistral using confirmed flags. Request routed JSON and timing output only if the pinned help proves support. Require a successful exit, no `unrouted` failure markers, a satisfied 50 MHz constraint, and a nonempty RBF before invoking the manifest collector.

- [ ] **Step 4: Run purity tests, simulation, and the full OSS build**

Run:

```bash
python3 -m unittest tests.test_oss_purity -v
make sim EXP=010_blinky
make oss EXP=010_blinky
```

Expected: purity tests and simulation pass; synthesis, placement, routing, timing, and RBF generation succeed. If the exact device or minimal route fails, stop this plan at this task, preserve logs, create the smallest numbered regression experiment, and follow the spec's reduce-first policy.

- [ ] **Step 5: Inspect artifacts and rebuild deterministically**

Run:

```bash
test -s build/oss/010_blinky/top.rbf
python3 -m json.tool build/oss/010_blinky/manifest.json >/dev/null
sha256sum build/oss/010_blinky/top.rbf
make oss EXP=010_blinky
```

Expected: required artifacts remain present; a second build succeeds without tool rebuilds; the manifest accurately records whether RBF bytes are stable rather than assuming they must be.

- [ ] **Step 6: Record current command results and commit**

Add exact tool versions, accepted device syntax, commands, utilization, timing, RBF size, and SHA-256 to the bring-up log.

```bash
git add Makefile scripts/build_oss.sh tests/test_oss_purity.py docs/bringup-log.md
git commit -m "feat: generate Cyclone V RBF through OSS flow"
```

### Task 8: Optional Quartus 17.0.2 Oracle and Build Comparison

**Files:**
- Create: `experiments/010_blinky/oracle/top.qpf`
- Create: `experiments/010_blinky/oracle/top.qsf`
- Create: `scripts/build_oracle.sh`
- Create: `scripts/compare_builds.py`
- Create: `tests/test_oracle_boundary.py`
- Create: `tests/test_compare_builds.py`
- Modify: `Makefile`
- Modify: `docs/oracle-method.md`

**Interfaces:**
- Consumes: explicit `QUARTUS_ROOTDIR`, identical shared RTL/constraints, OSS manifest, and Quartus reports.
- Produces: oracle artifacts/manifest, `comparison.json`, `comparison.md`, and friendly unavailable status.

- [ ] **Step 1: Write failing oracle-boundary tests**

Test that absent `QUARTUS_ROOTDIR` returns nonzero with `Quartus oracle unavailable; OSS and simulation remain usable`; a root containing a fake 17.0.0 binary is rejected; a fake 17.0.2 binary reaches print-command mode; and neither `make oss` nor `make sim` references or executes the fake Quartus marker.

- [ ] **Step 2: Write failing comparison tests**

Use fixture manifests with differing ALM counts and RBF hashes but passing builds. Assert comparison exits 0 and reports differences. Change oracle build status to failure and assert comparison exits nonzero. Assert missing artifacts are reported as failures, not zero-valued resources.

- [ ] **Step 3: Run tests and verify failures**

Run: `python3 -m unittest tests.test_oracle_boundary tests.test_compare_builds -v`

Expected: failures because oracle and comparison scripts are absent.

- [ ] **Step 4: Implement the minimal Quartus project and explicit wrapper**

Set family `Cyclone V`, device `5CSEBA6U23I7`, top entity `top`, shared RTL path, shared 20 ns SDC, V11 clock, and W15 LED0 in the QSF. Disable unrelated auto-generated IP and incremental compilation. The wrapper resolves only `$QUARTUS_ROOTDIR/quartus/bin/quartus_sh` or the exact documented installation layout, validates `Version 17.0.2`, invokes command-line compile, then copies RBF, fit/timing reports, logs, and parsed resource/timing summaries under `build/oracle/010_blinky/`.

Document the required base `Quartus-lite-17.0.0.595-linux.tar` SHA-1 `e71eeca4c8e1efaca902a58a37544c0572c6f45e` and Update 2 `Quartus-lite-17.0.2.602-linux.tar` SHA-1 `02aebab728d54e3ca8660d2646fdf93bc669b0ac`, installation path without spaces, and manual account/license acceptance. Do not add automated proprietary downloads.

- [ ] **Step 5: Implement semantic comparison**

Read manifests and normalized resource/timing summaries. Produce aligned Markdown and JSON covering target, lane status, ALMs/registers/hard blocks, requested clock, reported timing, RBF hash, and hardware observation. Fail only on explicit acceptance conditions: failed/missing build, unrouted design, timing below 50 MHz, unexpected hard blocks, failed simulation, or missing required artifact.

- [ ] **Step 6: Run boundary and comparison tests**

Run: `python3 -m unittest tests.test_oracle_boundary tests.test_compare_builds -v`

Expected: all tests pass without Quartus installed.

- [ ] **Step 7: Install and validate Quartus with the user**

Pause at the account-gated download. Have the user download the base and Update 2 Lite bundles. Verify both SHA-1 values before running their installers, install to a path without spaces, set `QUARTUS_ROOTDIR`, and run `quartus_sh --version`.

Expected: exact version contains `17.0.2`; `make doctor` changes only oracle readiness.

- [ ] **Step 8: Run oracle and compare**

Run:

```bash
make oracle EXP=010_blinky
make compare EXP=010_blinky
```

Expected: Quartus compiles identical RTL/constraints; both reports are generated; resource or byte differences are informational unless an acceptance rule fails.

- [ ] **Step 9: Commit oracle lane**

```bash
git add Makefile experiments/010_blinky/oracle scripts/build_oracle.sh scripts/compare_builds.py tests/test_oracle_boundary.py tests/test_compare_builds.py docs/oracle-method.md
git commit -m "feat: add isolated Quartus oracle comparison"
```

### Task 9: Safe Volatile Programming Preflight

**Files:**
- Create: `scripts/program.py`
- Create: `tests/test_program_preflight.py`
- Modify: `Makefile`
- Modify: `boards/de10nano/README.md`

**Interfaces:**
- Consumes: `BUILD` lane manifest/RBF, exact expected device, and openFPGALoader discovery output.
- Produces: `make program EXP=010_blinky BUILD=oss` with preflight and optional `--cable` selection.

- [ ] **Step 1: Write failing preflight tests using a fake programmer**

Cover missing RBF, hash mismatch, wrong target in manifest, zero boards, two boards without explicit selection, one DE10-Nano, and programmer failure. Assert no programming command contains flash/persistent options such as `--write-flash`, `-f`, or an address. Assert `--dry-run` performs every check but never invokes the fake program action.

- [ ] **Step 2: Run tests and verify missing implementation**

Run: `python3 -m unittest tests.test_program_preflight -v`

Expected: failure because `program.py` is absent.

- [ ] **Step 3: Implement strict preflight and volatile programming**

Load the selected lane manifest; require exact experiment, device `5CSEBA6U23I7`, successful build, and matching RBF SHA-256. Query the confirmed Task 4 openFPGALoader cable/board command, parse devices conservatively, and require one intended board unless the user supplies an exact discovered cable identifier. Print artifact, hash, board, cable, and final escaped command before execution. Do not offer persistent modes.

- [ ] **Step 4: Document physical connection and recovery**

Using the authoritative Terasic manual, identify the DC power connector and onboard USB-Blaster USB connector by their board labels. Explain Linux USB enumeration, udev/group permissions, exact discovery command, single-board-first recommendation, volatile configuration, and power-cycle recovery. Explicitly say that the M1 procedure does not write the microSD card or flash.

- [ ] **Step 5: Run preflight tests and a disconnected dry run**

Run:

```bash
python3 -m unittest tests.test_program_preflight -v
python3 scripts/program.py --experiment 010_blinky --build oss --dry-run
```

Expected: tests pass; disconnected hardware produces a safe `no DE10-Nano detected` stop after artifact validation.

- [ ] **Step 6: Commit programming safety**

```bash
git add Makefile scripts/program.py tests/test_program_preflight.py boards/de10nano/README.md
git commit -m "feat: add safe volatile DE10-Nano programming"
```

### Task 10: Hardware Bring-Up, Observation, and M1 Gate

**Files:**
- Modify: `docs/bringup-log.md`
- Modify: `build/oss/010_blinky/manifest.json` (generated, not committed)
- Modify: `build/oracle/010_blinky/manifest.json` (generated, not committed)

**Interfaces:**
- Consumes: completed simulation, OSS build, oracle build, one connected DE10-Nano, and user observation.
- Produces: a committed hardware-validation record and a clear M0/M1 pass or reduced blocker.

- [ ] **Step 1: Run the complete pre-hardware gate**

Run:

```bash
python3 -m unittest discover -s tests -v
make doctor-strict
make sim EXP=010_blinky
make oss EXP=010_blinky
make oracle EXP=010_blinky
make compare EXP=010_blinky
```

Expected: all automated tests pass; both lanes build; comparison has no acceptance failure.

- [ ] **Step 2: Connect exactly one board and verify detection**

Follow `boards/de10nano/README.md`: power one DE10-Nano, connect its onboard USB-Blaster port, leave the second board disconnected, and run `make doctor` plus the confirmed openFPGALoader discovery command.

Expected: one USB-Blaster/DE10-Nano is detected with user access and no programming has occurred.

- [ ] **Step 3: Program the OSS RBF explicitly**

Run: `make program EXP=010_blinky BUILD=oss`

Expected: preflight prints the exact artifact SHA-256 and device, openFPGALoader exits successfully, and no persistent-memory operation appears.

- [ ] **Step 4: Obtain and record the hardware observation**

Ask the user to confirm that LED0 repeatedly stays low for about 0.336 seconds and high for about 0.336 seconds. Record date/time, board identifier or distinguishing label, lane `oss`, RBF SHA-256, programmer command/version, programmed status, observed cadence, observer, and pass/fail in a fenced JSON block in `docs/bringup-log.md`.

- [ ] **Step 5: Decide the terminal path from evidence**

If the LED behavior passes, mark M0 and M1 complete in the bring-up log. If programming succeeds but behavior fails, program the oracle RBF, record both observations, and create the smallest reduced experiment before changing tools. If OSS build/programming fails while oracle succeeds, stop advancement and document the exact failing stage, commands, hashes, and logs as the first upstream-quality regression.

- [ ] **Step 6: Run final verification**

Run:

```bash
python3 -m unittest discover -s tests -v
git diff --check
git status --short
```

Expected: all tests pass; no whitespace errors; only the intentional bring-up log change is tracked, while generated tool/build artifacts remain ignored.

- [ ] **Step 7: Commit the hardware result**

```bash
git add docs/bringup-log.md
git commit -m "test: validate OSS blinky on DE10-Nano hardware"
```

Do not begin raster or Pong work in this plan. The next design cycle begins only after the hardware-result commit exists.
