# Focused software tests

Run the software checks affected by a branch and its current local edits:

```sh
python3 scripts/test_changed.py --base origin/main
```

`--base` is required. The command compares the merge base of that reference and
`--head` (default `HEAD`), using `scripts/affected.py`. It also includes staged,
unstaged and non-ignored untracked paths when the selected head is the current
checkout. It does not fetch, switch branches, select component revisions, commit,
or contact the kit. Review the exact commands without executing tests:

```sh
python3 scripts/test_changed.py --base origin/main --plan-only
```

An explicit `--head` may be used for planning another commit. Execution requires
that commit to be checked out; otherwise the command refuses to test different
bytes from the requested revision. All-zero forty-character `--base` means all
lanes, as used by manual and scheduled full CI runs. `--jobs 2` controls the runtime
Make parallelism; values from 1 through 32 are accepted.

## Selection and coverage

| Changed area | Selected checks |
| --- | --- |
| Host | Parent regressions/consumer consistency; host, appliance and shared expansion linker Go race tests; host UI tests |
| `sources/misteross/expansion` Go module | Parent checks and host/target consumers, including the shared linker tests; no FPGA simulations |
| Runtime | Parent checks, runtime software suite and host protocol consumers |
| Shared contracts | All software lanes and their dependent consumers |
| Parent-owned roots (`scripts/`, `tests/`, `image/`, `platform/`, `profiles/`, `containers/`) | Parent regressions and generated-consumer consistency; no component lanes or core simulations |
| Known FPGA producer/package software | Parent checks and producer/package/functional-identity/search-policy tests; no RTL simulation |
| FPGA core source | Parent checks, FPGA software tests and simulations for that family and its dependent consumers |
| Unknown or root inputs | All software lanes |
| Documentation only | Planner regression tests and whitespace checks |

`AGENTS.md` changes count as behavioral inputs. Renames select both old and new
owners. Shared Coleco VDP, RAM and TV80 changes select Coleco, SG-1000 and SMS.
The shared 6502, `fes.computer` mailbox and the Apple II socket generator and
card producer select Apple II; the shared I2S audio units also select it.
The Coleco directory conservatively selects those same consumers because its
generated headers are shared. Pong's directory also selects the demo, which
uses its board models. New shared units, unclassified scripts, build graph
changes and unknown core families select all registered core families.
The rules in `scripts/affected.py` are checked against current literal source and
include paths in the expanded Make recipes and producer scripts. This is not
recursive compiler/import tracing: relative or dynamically constructed
cross-family references need explicit review. When adding a consumer, update
its rule and coverage test together. A new supported core also needs its
simulation target registered in `scripts/ci_simulations.py`.

Parent-owned matching is the exact directory or any descendant, so sibling
names such as `scripts-other/` or `test-other/` stay unknown. The CI
selection/compiler and shared generator policy files — `scripts/affected.py`,
`scripts/test_changed.py`, `scripts/ci_gate.py`, `scripts/ci_simulations.py`,
`scripts/ci_verilator.sh`, `scripts/generate.py`, `scripts/consistency.py`
(the generated/copied/fixture mapping owner) and their regression suites
under `tests/` — remain fail-broad and select every lane and core. Root files
such as the top-level `Makefile`, `config/` and `.github/` also stay on the
all-lanes path.

## Routine and extended test modes

Beyond lanes, each plan reports `test_modes` (`video`, `media`, `full_race`)
computed by `scripts/test_policy.py`. Routine runs execute the fast regression
and concurrency-focused checks; extended suites return only when their inputs
change or on scheduled/manual full runs. A passing routine run intentionally
omits tests and is not exhaustive green.

| Mode | Routine run | Re-included by |
| --- | --- | --- |
| `video` | Parent suite omits the full-device video classes `RealProducerEvidenceTests`, `NativeProducerEvidenceTests`, `STProducerEvidenceTests` (`test_factory_video_parts.py`) and `FactoryVideoPublicationTests`, `RasterFactoryVideoPublicationTests` (`test_factory_video_publication.py`); lightweight classes in the same files still run | Video producer/admission inputs (`scripts/factory_video_parts.py`, `core_catalog.py`, `core_dev_accept.py`, `core_dev.py`, `package_acceptance_isolated.py`, `recipes.py`, `bundle.py`, `artifact_cache.py`, `core-recipes.toml`), `sources/misteross`, `sources/FogCast/corepackage`, `corecatalog`, and non-test `.go` under `catalog`/`fogcast` |
| `media` | Parent suite omits the container drivers `ContainerImageTests`, `RealImageTests` (`test_media_image.py`), `ContainerTests`, `RealBootstrapTests` (`test_appliance.py`), `ContainerTests`, `RealCardTests` (`test_appliance_media.py`) | Image/platform/containers/profiles roots, `scripts/media*.py`, `scripts/appliance*.py`, `platform.py`, `image_toolchain.py`, `boot-media.lock.toml`, the three media test files, `sources/FogCast/appliance`, `cmd/target-image-lock` |
| `full_race` | Host functional coverage is partitioned exactly once: the race-expensive `fogcast`/`corepackage` run all assertions non-race, and every other package runs its whole suite under `-race -short`. `fogcast`/`corepackage` additionally get concurrency-focused `-run` race instrumentation (`HOST_RACE_FOCUS`: Concurrent/Cancel/Session/Lifecycle/Stop/Target/Lock/Queued/Drain/Lease/Discovery/Watch/Input/Mesh names). Not exhaustive — full mode races every package in all three modules | Shared contracts (`sources/mister-packages`), `AGENTS.md`, unknown inputs and new branches select every mode; weekly schedule and manual dispatch always run all modes |

Shared `scripts/build.py`, `inputs.py`, `native_dev.py`, `environment.py` and
FogCast `go.mod`/`go.sum` select both video and media. CI validates
`test_modes` against the lanes (`video`/`media` need parent, `full_race` needs
host) in the plan step and again in the required integration gate, and retains
`/tmp/affected.json` as the `fes-ci-plan-<run_id>-<attempt>` artifact.
`scripts/parent_tests.py` prints the selected and intentionally omitted test
IDs as JSON; with `media` selected it requires a working Docker before running
rather than silently skipping. `scripts/host_tests.py` owns the Go commands;
`--full` races every package in all three modules without `-short` or `-run`.

The affected runner passes the planned modes through: parent checks run
`scripts/parent_tests.py --video/--media`, host checks run
`scripts/host_tests.py` with `--full` only under `full_race`. Opt into the
exhaustive local run with `python3 scripts/test_changed.py --base origin/main
--full`, which enables every mode for the already-selected lanes. The affected
runner's routine Go commands also keep the reduced catalog fixture. The
comprehensive `make test` (parent) and `make -C sources/FogCast test` (FogCast)
suites remain unchanged full entry points that include every class. To exercise
the full 10k-row catalog fixture locally:

```sh
(cd sources/FogCast && go test -race -timeout 30m ./catalog -run '^TestQueryGamesFixtureStaysBounded$')
make -C sources/FogCast test GO_TEST_FLAGS=
```

The Atari 520ST family and shared FX68K files select `make sim-fes-atari-st`,
covering CPU/MMU, SDRAM, peripherals, media upload, expansion and all video
modes. Shared direct/scanline parts select Coleco and Atari ST; the shared
computer mailbox and audio units include ST. Its reused C64 system PLL and
ZX81 AY engine select their original consumers plus ST. The addon-SDRAM
controller selects RAM tester and ST simulations. The ST producer is
registered, with no factory image entry; CI still runs host checks only.

The local runner includes the default and OSS
SMS/SG-1000 simulations; Coleco's aggregate includes its OSS unit/board cases,
the CPU-bus probe and registered diagnostic-through-socket test. It
never invokes a synthesis, placement, Quartus, image-building or deployment target.

CI uses the same producer-test list and affected families, but runs independent
components and simulation targets in separate jobs. Coleco has 16 focused jobs:
seven unit/board scenarios in default and registered OSS lanes (14 jobs), plus
the CPU expansion probe and diagnostic through the registered socket. These
preserve the cases and compiler flags in the local aggregate commands. Each job
has isolated build output; the matrix runs at most eight jobs concurrently.

PRs run against their merge result; pushes to `main` validate the merged result.
Feature-branch pushes do not duplicate PR runs. Merge groups remain supported;
weekly and manual runs select the complete suite. The stable `integration`
check requires every planned job to succeed, including compiler preparation.
Missing, skipped or cancelled required jobs fail the gate. Deliberately omitted
jobs are recorded in the plan, not reported as successful tests in the CI receipt.

Parent checks run the Python regression suite and the existing
`consistency.check` function against current module files. This allows ordinary
uncommitted development changes while still comparing schemas, generated bytes,
fixtures and source copies. It does **not** replace `make check`'s committed-source
selection gate. Parent platform/image packaging suites and the required browser
integration lane are outside this focused command. Individual tests may report
their own skips; a passing command does not turn a skipped test into coverage.
The recipe tests access bundle helpers through a module import (`from tests
import test_bundle`) so discovery does not re-run the imported bundle suite.
The regression suite also executes FogCast's contained-development diagnostic
contract against the host and image inputs, so image-only changes retain that
cross-component coverage without selecting the host lane.

## Results and prerequisites

The command writes a JSON plan/result to stdout, including changed paths, local
edits, selection reasons, skipped lanes, exact argument arrays, command results
and coverage limits. Test output goes to stderr. `--plan-only` reports `planned`,
not `passed`. Exit status is zero for a completed passing run or a valid plan,
one for a test failure, and two for invalid input or missing prerequisites.

Executable tools and required module/test files are checked before tests start.
No test lane is silently dropped because Go, Make, a C/C++ compiler, Node, ripgrep
Verilator or strace is absent. The FPGA read-audit tests use strace; the
functional FPGA producers also require it to check documentation exclusions.
Language dependencies, authenticated local test fixtures,
and any test-container prerequisites must already be prepared; the runner does
not install them. Such failures stop the run and remain failures. It clears
`GOOS`, `GOARCH` and `GOARM` from the test environment so a prior cross-build shell
does not redirect native tests to the target architecture.

Commands use argument arrays without a shell. The first failed command stops
execution and reports the remaining commands as not started. The command runs
against the working tree, so do not concurrently change selected sources or run
other builds in the same checkout. Test outputs may populate normal ignored
build directories. Passing software checks is not image reproducibility, FPGA
build evidence or hardware acceptance.

For integration diagnostics involving uncommitted module edits, use
`make dev-snapshot` and run `make host` or `make dev` in the returned checkout.
This freezes final working bytes without changing the source index or branch;
it does not replace focused tests. The resulting commit is marked development
only and cannot pass release, image or media selection checks. See
[the development guide](development.md#diagnose-local-edits-without-committing-them).

The contract lane needs the pinned Python test dependencies. Create a local
virtual environment before running affected tests that include contracts:

```sh
python3 -m venv out/test-venv
. out/test-venv/bin/activate
python -m pip install -r sources/mister-packages/requirements-test.txt
python scripts/test_changed.py --base origin/main
```

Preflight checks `jsonschema` and `rfc3986_validator` in the selected Python
interpreter before any test command starts. It reports missing dependencies
with the install command; it does not install packages automatically.

The simulation-only `fes-menu` family is selected by its RTL or simulation
script changes. Local affected checks and the required CI simulation matrix
run `make sim-fes-menu`, covering reader and video regressions. This does not
select a menu artifact for the appliance or qualify physical scanout.
