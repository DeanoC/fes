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
| Runtime | Parent checks, runtime software suite and host protocol consumers (native test-only sources omit host) |
| Shared contracts | All software lanes and their dependent consumers |
| Parent-owned roots (`scripts/`, `tests/`, `image/`, `platform/`, `profiles/`, `containers/`) | Parent regressions and generated-consumer consistency; no component lanes or core simulations |
| Known FPGA producer/package software | Parent checks and producer/package/functional-identity/search-policy tests; no RTL simulation |
| FPGA core source | Parent checks, FPGA software tests and simulations for that family and its dependent consumers |
| Unknown or root inputs | All software lanes |
| Documentation only | Planner regression tests and whitespace checks |

`AGENTS.md` changes are documentation and select no executable lane. Renames select both old and new
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
names such as `scripts-other/` or `test-other/` stay unknown. Planner and
generator regression changes select parent tests and generated-consumer checks;
they do not simulate unchanged RTL. Changed generated outputs still select their
actual consumers. Simulation matrix/compiler changes select the FPGA lane and
all core simulations. The parent Makefile, workflow and shared software test runner select all
software lanes without RTL simulations. Host runner changes select parent and
host checks. Contract Go tooling and core recipe/provisioning changes also skip
unchanged RTL; contract definitions retain consumer coverage.

Platform provisioning, platform Go tests and image shell tests run only when
`media` is selected, including scheduled/manual runs. A room, host or core edit
does not provision an OS test container just because it selects parent regressions.
Unknown inputs still select all lanes and cores.

## Go dead-code analysis

CI's consistency job always runs `make check`. Its dead-code step passes the
changed-path list to `scripts/check_deadcode.py`: known non-Go parent scripts,
tests, image/container/profile recipes and non-Go runtime/FPGA inputs skip the
expensive Go analysis. Any change in a Go module (including non-Go build or
embedded inputs), Go source/dependency files elsewhere, checker/workflow inputs,
shared configuration or unknown roots keeps the full host and ARM analysis.
Documentation follows the outer planner's documentation-only policy.
Mixed changes and renames retain all relevant inputs. Scheduled/manual full
runs pass the full-run marker and always analyze every module.

`make deadcode` remains an unconditional full check. To inspect CI selection
without installing tools, use `python3 scripts/check_deadcode.py --plan-only
--paths-json '["scripts/appliance.py"]'`. Invalid path payloads fail; an omitted
path list runs full analysis. A skip is reported explicitly in the CI step log.

## Affected host Go packages

For Go source/test edits in FogCast, its appliance module or the shared expansion
module, routine host checks run the changed
packages and their transitive import consumers. Test and external-test imports
also count as dependencies. The selector reads both native and race build
variants with `go list`; selection happens before the existing three-way sharding.
Each selected package retains its existing functional/race policy.

CI also passes `--build-host-if-needed` to the host runner. When the validated
change set contains only Go `_test.go` files and safe accompanying guides, the
runner skips the additional standalone `make host` binary build. All selected Go
tests still run. Production Go, build/dependency inputs, uncertain ownership or
embedded guides, omitted paths, and full/scheduled runs keep the build on shard 1.
The JSON report records whether the build is required and scheduled on that
shard; a required build failure fails the runner before tests start. Plain
`make host` remains unconditional, and local host test runs include no binary
build unless the option is supplied.

Accompanying Markdown guides in the root `docs/` tree, a selected Go module's
`docs/` tree, or the root/module `README.md` and `AGENTS.md` do not restore the
whole host suite. The selector uses `go list` embed metadata from both native
and race variants: a guide below a package with production or test embed inputs
retains the full suite, including deleted files covered by directory embeds.
This is deliberately conservative rather than trying to reproduce Go's pattern
matching. Markdown in `testdata`, package-local assets, guide symlinks and
unreviewed locations also retain full coverage.

Both CI and `test-changed` pass the same changed-path list, including both sides
of renames and local edits for local runs. The host runner reports the selected
package identities. To inspect a specific change without executing tests:

```sh
python3 scripts/host_tests.py --plan-only --paths-json '["sources/FogCast/cmd/fogcast/main.go"]'
```

A non-Go change outside those reviewed guides, dependency lock change,
missing/deleted whole package or omitted path list keeps the complete routine
host suite. A Go listing error
fails the check. `--full` and scheduled/manual coverage never narrow packages.
This is native Go import coverage; it does not infer runtime file reads or
cross-platform behavior. Host builds, browser checks, parent regressions and
other component lanes retain their own selection rules.

## Affected parent regression groups

Routine reviewed changes select parent integration groups before test setup:

| Change | Parent groups |
| --- | --- |
| FogCast UI and resources | Policy and build orchestration |
| Host/package/protocol consumers, runtime, or FPGA sources | Policy, build, packages and acceptance tooling |
| Appliance module or target-image selector | All groups, including media |
| Recognized parent test modules outside the policy group | Policy, build and the owning group, plus cross-group fixture consumers |
| `scripts/appliance.py`, `appliance_inside.py`, `appliance_media.py`, `appliance_media_inside.py`, `appliance_expand_dry_run.py` | Policy, build and media |
| Documentation alongside code/tests | Keep the code groups and add the documentation-reading test modules |
| Selection-policy scripts/tests, other parent scripts/tests, image/platform, configuration, shared definitions, unknown paths | All groups |

Policy includes planner, generated-consumer and image-boundary checks. Build
includes source snapshots, receipts and kit software assembly. Package checks
include admission, publication and video-part regressions. Media owns OS/card
assembly. Acceptance tooling contains host-side diagnostic-runner regressions;
selecting it does not contact a kit.

Mixed changes union their groups; any full-coverage input restores all groups.
The existing video/media modes can add their owning groups and still control
extended classes. New unassigned test modules always run until reviewed and
assigned. Import failures remain failures even when groups are filtered.

Documentation in a mixed change does not restore every group. The explicit
`DOCUMENTATION_MODULES` set retains link, image-assembly, consistency,
coordination, core-build and bundle tests that check repository guides. Update
that set when adding a documentation reader outside those modules. Temporary
Markdown fixtures do not make a test a repository-documentation reader.
Documentation-only CI continues through its existing always-run checks; this
selection affects the parent lane when code or tests also changed.

CI and the local affected runner pass the same changed-path list. The parent
JSON report includes groups, retained documentation modules and selected/omitted
test IDs. For example:

```sh
python3 scripts/parent_tests.py --video false --media false --plan-only \
  --paths-json '["sources/FogCast/ui/rooms/examples/fes-cores/main.lua"]'
```

No path list, scheduled/manual runs, and `--full` retain every group. `--full`
also enables both extended modes. This policy narrows component integration
regressions and reviewed parent inputs. Shared build scripts and unreviewed parent
implementation retain full regression coverage because imports alone do not
describe their subprocess and file inputs. The explicit parent script allowlist
lives in `scripts/parent_affected.py`; adding a script requires checking its
consumers, including subprocess calls and recipe-file reads. Test fixture consumers
across groups are retained explicitly. Extended video/media mode selection is
unchanged and adds its owning group when required.

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
| `full_race` | Host functional coverage is partitioned exactly once: the race-expensive `fogcast`/`corepackage` run all assertions non-race, and every other package runs its whole suite under `-race -short`. `fogcast`/`corepackage` additionally get concurrency-focused `-run` race instrumentation (`HOST_RACE_FOCUS`: Concurrent/Cancel/Session/Lifecycle/Stop/Target/Lock/Queued/Drain/Lease/Discovery/Watch/Input/Mesh names). Not exhaustive — full mode races every package in all three modules | Shared contracts (`sources/mister-packages`), unknown inputs and new branches select every mode; weekly schedule and manual dispatch always run all modes |

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

The suites do not depend on the caller's umask, `TMPDIR` length or location,
an inherited `GOTOOLCHAIN`, or the system Go version. Unix-socket tests take
their path from FogCast `internal/testsock`, which stays under the kernel's
`sun_path` limit. Kit-state tests fix their own umask and choose a temp base
without a shared writable ancestor; their path checks stay inside that fixture.
The module-source build test probes the Go toolchain FogCast selects with
`GOTOOLCHAIN=auto`.

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

### Runtime build regressions

CI and `test-changed` use the same runtime target selection. Reviewed C/C++
source, header and test changes (optionally with guides) run `test-routine`:
all runtime tests once, header-dependency queries, the version-rebuild regression,
and the active-tree and hardware-support guards. The dependency check first
requires each binary to be up to date, then uses Make's assumed-new header
queries to prove it would rebuild. It changes no timestamps and compiles nothing.

Runtime Makefiles, shell scripts, third-party inputs, CI/policy changes and
unknown inputs retain `make test`, which actually touches the representative
headers and rebuilds every test binary, verifying its timestamp. The full
functional suite runs once before those timestamp-only header changes.
Scheduled/manual runs and `test-changed --full` retain this exhaustive mode. Direct `make test` remains
exhaustive. Inspect selection with
`python3 scripts/runtime_tests.py --plan-only --paths-json '["sources/libmister-runtime/src/native/input.cpp"]'`.

Native C/C++ test sources under runtime `tests/unit`, `tests/integration` and
`tests/support` select runtime and the relevant parent integration checks without
the host Go/UI lane. Production source or protocol headers, fixtures, shell
scripts, build inputs and unclassified paths retain host consumer checks.
Mixed changes union their lanes, including both sides of renames and deletions;
manual/scheduled all-input runs retain every lane. Local `--full` still expands
checks only inside selected lanes.

### Reviewed parent planner inputs

Edits to `scripts/affected.py`, `scripts/ci_gate.py`, `scripts/check_diff.py`
and their reviewed regression modules (`test_affected`, `test_ci_gate`,
`test_check_diff`, `test_test_changed`) select parent policy and build groups.
These retain workflow, integration, source/receipt and status consumers while
omitting unrelated media, package and acceptance suites. Mixed changes add their
consumer groups; accompanying guides retain their documentation readers.
Unassigned test modules still run.

Changes to `parent_affected.py`, `parent_tests.py`, `test_parent_affected.py`,
shared test-mode policy, other unreviewed inputs, or an omitted/unknown path list
retain the full parent suite. Manual/scheduled full plans and `--full` remain
exhaustive. This selection changes parent checks only; component lane selection
and the integration gate stay unchanged.

### Browser UI checks for Go test changes

CI and the local host runner use `host_tests.py --ui-if-needed`. Only changes
confined to Go test files in known packages (plus verified guides) can omit the
browser UI suite. UI and host API tests retain it, as do production code,
Go dependencies, build/policy inputs, unknown or removed packages, embedded
guides, and full runs. The Go package graph and reverse test consumers are
still validated and tested. The UI boundary guard still scans all Go files,
including changed tests. CI runs the UI check once, on its last host shard.
In the `scripts/test_changed.py` plan, the host command under `commands` includes
`host_ui.target` (`test-ui` or `test-ui-boundary`). The standalone host runner's
JSON report puts the same selection at the top level: `host_ui.scheduled`
describes the browser suite and `host_ui.boundary_scheduled` describes the
boundary guard.
Node.js is required only when browser tests are scheduled; both runners check
this before executing tests. Inspect the exact target without executing checks with:
`python3 scripts/host_tests.py --ui-if-needed --plan-only --paths-json '["sources/FogCast/internal/rebootguard/fallback_test.go"]'`.
