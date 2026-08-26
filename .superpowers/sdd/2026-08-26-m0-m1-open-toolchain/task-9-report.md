# Task 9 report — safe volatile programming preflight

Date: 2026-08-26
Worktree: `/home/deano/misteross/.worktrees/m0-m1-open-toolchain`
Branch: `feature/m0-m1-open-toolchain`

## Result

Implemented the adapted Task 9 programming boundary in the four prescribed
files. The default transport is `mister`, which requires explicit
`MISTER_HOST`/`MISTER_USER` (or CLI equivalents), performs a consolidated
read-only ARM/FIFO/Main identity check, stages one cryptographically
unpredictable private directory below remote `/tmp`, verifies remote metadata
and SHA-256, and writes exactly one `load_core /tmp/.../artifact.rbf` line to
`/dev/MiSTer_cmd` only when not in dry-run mode. `jtag` remains an optional
external USB-Blaster path using only `--write-sram` and a private immutable
local snapshot.

The local gate accepts only a schema-2 manifest for the requested experiment
and lane, exact target `5CSEBA6U23I7`, a canonical regular nonempty
`build/<lane>/<experiment>/top.rbf`, matching SHA-256 and byte count, pass
build/route/timing/hard-block status, no unknown/used hard blocks, and
lane-appropriate RBF provenance (measured stable for OSS; explicitly
unmeasured with a reason for the oracle). Paths, host/user/cable identifiers,
remote staging, and subprocess command construction are validated before any
action.

## TDD and verification evidence

The required focused suite was first run after adding the tests but before
`scripts/program.py` existed:

```text
python3 -m unittest tests.test_program_preflight -v
Ran 14 tests ...
FAILED (errors=14)
FileNotFoundError: .../scripts/program.py
```

After the minimal implementation, the focused suite passed. It was expanded
with explicit build/route/timing/hard-block/stability/schema/size checks and
SSH/SCP failure, staging-collision, remote-hash, FIFO, and command-injection
cases:

```text
python3 -m unittest tests.test_program_preflight -v
Ran 21 tests in 1.369s
OK
```

Fresh final verification:

```text
python3 -m py_compile scripts/program.py tests/test_program_preflight.py
git diff --check
timeout 180s python3 -m unittest discover -s tests -p 'test_*.py' -v
Ran 139 tests in 109.394s
OK
```

The focused tests use fake `openFPGALoader`, `ssh`, and `scp` executables.
They prove that dry-run never invokes the programming/upload/load action, an
ambiguous or zero-device scan stops, hashes and symlinks fail closed, and a
failed programmer/SCP/FIFO/hash check cannot advance to the next action.

## Disconnected checks

The canonical existing OSS artifact was validated locally:

```text
artifact: build/oss/010_blinky/top.rbf
manifest_schema: 2
target: 5CSEBA6U23I7
build_status: pass
route_status: pass
timing_status: pass
hard_block_status: pass
rbf_stability: measured/pass
size: 7007204 bytes
sha256: 6afe6c8b7bb61a3a442d4fe9df88dc2f9dfe52ebdcf807501549db4168f95632
```

With no endpoint configured, the default primary dry-run exited 2 after this
local validation with:

```text
program: mister transport requires an explicit host (--host or MISTER_HOST)
```

It did not invoke SSH. An explicit optional JTAG dry-run invoked only the
read-only discovery command and exited 2 with:

```text
openFPGALoader --board de10nano --scan-usb
program: no DE10-Nano detected by openFPGALoader
```

No upload, FIFO write, SRAM programming, reboot, or power-cycle was run.

## Documentation/provenance

`boards/de10nano/README.md` documents both paths, the Terasic **Power DC
Jack** and **USB-Blaster II** labels (Getting Started Guide Figure 2-3),
Linux discovery and permissions, single-device selection, SRAM volatility,
and recovery. It cites the authenticated `DeanoC/Main_MiSTer` `input.cpp` and
`fpga_io.cpp` sources at immutable commit
`d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` for the FIFO, exact `load_core PATH`,
absolute-RBF load, and application restart behavior.
It explicitly preserves FogCast's possible `/media/fat/MiSTer`, never uses a
persistent path, and warns that a minimal blinky can make Main exit when the
MiSTer framework handshake is absent; reboot/power-cycle restores the normal
menu.

## Commit and concerns

The implementation commit is:

```text
feat: add safe volatile DE10-Nano programming
```

No hardware, credentials, private endpoint, or persistent storage was used.
The real ARM transport remains operator-gated until the designated MiSTer Pi
host/user are supplied and the operator confirms the expected Main_MiSTer
process. The optional JTAG transport remains unavailable on this host because
the MiSTer Pi and SuperStation One have no onboard USB-Blaster and no external
USB-Blaster is connected. The task report is intentionally under the ignored
`.superpowers/sdd/` ledger tree, matching prior task reports.

## Fix Round 1 — review findings and verification

This section records the review-hardening round applied after the initial
`43bace6` implementation. Tests were added first against that baseline: the
focused suite ran 33 tests with 12 expected RED failures for the eight review
findings. The implementation then reached GREEN:

```text
timeout 180s python3 -m unittest tests.test_program_preflight -v
Ran 34 tests in 2.909s
OK
```

The focused suite now covers all of the following fail-closed gates:

- each lane's exact schema-2 resource classes, unknown-resource map,
  classified hard-block entries, zero forbidden usage, finite timing, exact
  50 MHz request, and OSS-versus-oracle stability semantics;
- per-run random remote `/tmp/misteross-<32-hex>/artifact.rbf` staging,
  atomic mode-0700 directory creation, second-run uniqueness, dangling
  symlink/race failure, root/type/mode/nlink metadata, remote hash, and no
  FIFO dispatch before verification;
- exactly one root-owned Main PID, root-owned FIFO metadata, matching FIFO
  inode held open by that PID, explicit expected Main SHA for non-dry network
  actions, and dry-run-only discovered-hash reporting;
- explicit `misterpi`/`de10nano` board attestations, physical JTAG
  `--cable-index` selection (with `--cable usb-blasterII` retained as the
  interface), zero/one/two/out-of-range probe cases, and immutable JTAG
  snapshot behavior when the canonical RBF is replaced during discovery;
- exact wording that FIFO/JTAG operations are dispatched with outcome
  unverified, never claimed as completed, and no flash/persistent path or
  option in any command.

The remote preflight commands are consolidated into one read-only SSH probe.
SSH uses bounded options with `BatchMode=no`; normal operator-interactive
authentication remains available, while no password is stored or embedded.
The volatile stage directory is intentionally left for reboot/recovery and is
not a persistent path. Main source provenance is pinned in the board README
to commit `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d`.

The full disconnected test suite and final checks for this round were:

```text
timeout 180s python3 -m unittest discover -s tests -p 'test_*.py' -q
Ran 152 tests in 117.132s
OK

bash -n scripts/build_oracle.sh
python3 -m py_compile scripts/program.py tests/test_program_preflight.py
git diff --check
make help
```

The default disconnected dry run (with endpoint variables explicitly unset)
validated the canonical OSS artifact and stopped before SSH with exit 2:

```text
program: mister transport requires an explicit host (--host or MISTER_HOST)
```

The optional disconnected JTAG dry run performed only local snapshot creation
and USB discovery, then stopped safely with exit 2:

```text
cable discovery: .../openFPGALoader --board de10nano --scan-usb
program: no DE10-Nano detected by openFPGALoader
```

No real target, credential, endpoint, upload, load, reboot, or hardware
observation is used in this fix round.
