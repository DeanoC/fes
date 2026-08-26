# M0/M1 Open MiSTer OSS Toolchain Design

## Purpose

This first project cycle will answer a deliberately narrow question with reproducible evidence:

> Can a fully open toolchain generate a useful Cyclone V bitstream for the exact DE10-Nano FPGA used by MiSTer, and can that bitstream blink a physical board LED?

The open path is Yosys, nextpnr-mistral, Mistral, and openFPGALoader. Quartus Prime Lite 17.0.2 is an optional reference compiler and must never become an OSS build dependency.

The target is the Terasic DE10-Nano/MiSTer FPGA `5CSEBA6U23I7` (Cyclone V SoC, UFBGA 672, speed grade 7).

## Scope

This cycle contains only:

- M0: a pinned, repository-local OSS toolchain, environment wrapper, diagnostics, and reproducibility metadata;
- M1: one 50 MHz input, one counter, and one user LED output, simulated, compiled through both lanes, and validated on hardware;
- a safe board-connection and volatile-programming procedure;
- a permanent reduced regression if the open flow fails where Quartus succeeds.

It excludes raster/video, Pong, PLLs, HPS integration, MiSTer framework integration, BRAM/M10K, LUTRAM, DSPs, SDRAM, audio, and persistent-flash or SD-card changes.

The repository remains private and local until the hardware result is mature. Public packaging, upstream pull requests, and final license selection are deferred.

## Chosen Setup Strategy

The project will use native, repository-local tool builds. The host package manager may supply ordinary build prerequisites, but FPGA tools will be fetched at pinned commits and installed beneath `build/toolchain/`.

This approach was selected because it combines reproducible binaries with uncomplicated USB-Blaster access and straightforward coexistence with a later native Quartus installation. Containers would add device-passthrough friction, while a Nix-based environment would add packaging work before the central feasibility question is answered.

Bootstrap must be idempotent. It may report missing host packages and print the command needed to install them, but it must not silently alter the host system.

## Repository Architecture

The repository has three separate execution lanes:

1. `sim` uses Verilator as the logical oracle and has no dependency on either FPGA compiler.
2. `oss` uses only pinned open-source tools: Yosys, nextpnr-mistral/Mistral, and openFPGALoader.
3. `oracle` uses Quartus Prime Lite 17.0.2 only when explicitly requested.

The OSS lane must not invoke, link to, discover as a fallback, or shell out to Intel/Altera executables. Quartus discovery occurs only inside oracle-specific code through an explicit `QUARTUS_ROOTDIR` or a documented equivalent.

The initial layout will include:

```text
Makefile
README.md
toolchain.lock
scripts/
  bootstrap.sh
  env.sh
  doctor.sh
  build_oss.sh
  build_oracle.sh
  program.sh
  collect_report.py
boards/de10nano/
  pins.qsf
  clocks.sdc
  README.md
experiments/010_blinky/
  rtl/top.v
  sim/
  top.sdc
  oracle/
  expected.md
docs/
  architecture.md
  oracle-method.md
  bringup-log.md
  upstream-findings.md
build/
  toolchain/
  oss/
  oracle/
```

Generated source trees, device databases, logs, reports, netlists, and bitstreams remain in ignored build or controlled third-party directories. Small RTL, constraints, scripts, manifests, and regressions remain versioned.

## Toolchain Pinning and Bootstrap

`toolchain.lock` will record, for each tool:

- upstream repository URL;
- exact Git commit;
- expected version or identity output;
- compatibility rationale;
- source archive or checkout hash where useful;
- relevant build options.

The initial pins will be selected only after checking the current upstream default branches and testing the combination. A moving branch name is never sufficient. The lock will cover Yosys, nextpnr, Mistral, Verilator, and openFPGALoader.

`scripts/bootstrap.sh` will fetch controlled checkouts and build into `build/toolchain/`. `scripts/env.sh` will prepend only those pinned binaries and libraries to the environment. Re-running bootstrap at the same lock state should perform no unnecessary work.

Before wrappers assume command syntax, bootstrap verification will capture current help/version output and confirm:

- the accepted Mistral device identifier for `5CSEBA6U23I7`;
- the current nextpnr-mistral constraint-input option;
- the current RBF-output option;
- the available routed-netlist and timing-report options;
- openFPGALoader's current DE10-Nano board identifier and detection commands.

These results will be copied into `docs/bringup-log.md` with exact commands. If the exact device is unsupported, device-database bring-up becomes the first reduced problem and blinky work stops.

## Diagnostics

`make doctor` will report, without mutating the machine:

- host OS and architecture;
- compiler and core build tools;
- Python;
- each required repository-local OSS tool and its pinned identity;
- optional Quartus location and exact version;
- USB/JTAG visibility where detectable;
- exact-device support status.

The output will group results as required OSS dependencies, optional oracle dependencies, and hardware readiness. Missing optional Quartus must not make OSS or simulation diagnostics fail. The command exits unsuccessfully only when the requested readiness class is not met; a documented flag or subtarget will distinguish general reporting from strict OSS readiness.

## Blinky Experiment

`010_blinky` will use only:

- `FPGA_CLK1_50` as a 50 MHz clock;
- a fabric counter implemented in registers and combinational logic;
- one user LED output;
- a deterministic initial/reset strategy supported by both synthesis lanes.

Yosys synthesis will explicitly disable BRAM, LUTRAM, and DSP inference. No PLL, derived fabric clock, HPS primitive, or vendor IP is permitted. A clock enable or counter bit will set the visible cadence.

The minimal board QSF will contain only the clock and LED assignments required by this experiment. Every pin and electrical assignment will cite an authoritative Terasic or MiSTer source. The checked-in SDC will describe the 50 MHz input clock.

## Build Interfaces and Data Flow

The common interface will be:

```text
make toolchain
make doctor
make sim EXP=010_blinky
make oss EXP=010_blinky
make oracle EXP=010_blinky
make compare EXP=010_blinky
make program EXP=010_blinky BUILD=oss
make clean EXP=010_blinky
```

The OSS data flow is:

```text
RTL + constraints
  -> Verilator lint/test
  -> Yosys synth_intel_alm
  -> synthesized JSON
  -> nextpnr-mistral place/route and timing
  -> Mistral RBF generation
  -> manifest and SHA-256
  -> explicit openFPGALoader programming
```

`make oss` will preserve the exact command lines and complete logs for every stage. Where the pinned backend supports them, it will also preserve routed JSON and timing output. The manifest will include experiment name, repository commit or dirty state, source hashes, tool identities and SHAs, exact commands, target device, host information, artifact hash, resource summary, and timing summary.

No build target automatically programs hardware.

## Quartus Oracle

Quartus Prime Lite is preferred over Standard because Lite supports Cyclone V and does not require a paid license. The oracle must report exact version 17.0.2 before its result is accepted.

The user-provided link for `Quartus-lite-17.0.0.595-linux.tar` is the authentic base 17.0.0 Lite bundle, SHA-1 `e71eeca4c8e1efaca902a58a37544c0572c6f45e`. It is not by itself version 17.0.2. The official complete Update 2 bundle is `Quartus-lite-17.0.2.602-linux.tar`, SHA-1 `02aebab728d54e3ca8660d2646fdf93bc669b0ac`, and requires the base software to be installed first according to Altera's instructions.

The repository will link to the stable official download page rather than an expiring account SSO or license-acceptance URL. It will document installer names and checksums but will not download automatically, redistribute, or commit proprietary files.

Quartus will be installed in a path without spaces because Altera warns of a vulnerability affecting versions 11.0 through 18.0 in paths containing spaces. The oracle wrapper will fail with a concise installation/detection message when Quartus is absent, without affecting `make toolchain`, `make doctor`, `make sim`, or `make oss`.

The oracle project uses the identical blinky RTL, target, pinout, and timing intent. Its outputs remain under `build/oracle/` and include logs, RBF, fitter report, timing report, resource summary, and manifest.

## Failure Handling and Regression Policy

Every pipeline stage stops on its first real error while preserving its complete log and already-produced inputs. Wrappers will surface the failed stage, command, log path, and next diagnostic action.

An OSS failure will not be patched inside nextpnr or Mistral until:

1. the behavior is reduced to the smallest standalone experiment;
2. RTL simulation passes where applicable;
3. Quartus confirms the reduced design is valid, when the oracle is available;
4. the current OSS failure and tool identities are captured;
5. a regression is defined that fails before and passes after a fix.

Reduced failures are permanent project outputs under `experiments/`, not temporary debris. Local upstream-tool patches must remain separable and be recorded in `docs/upstream-findings.md`.

## Hardware Connection and Programming Safety

The board guide will identify the DE10-Nano power input and onboard USB-Blaster connector using authoritative board documentation. The operator will:

1. power the board normally;
2. connect the USB-Blaster port to the Linux development host;
3. verify USB enumeration and user permissions;
4. verify openFPGALoader detects exactly one intended DE10-Nano;
5. run the explicit programming target;
6. power-cycle to recover the normal volatile configuration if needed.

The programming wrapper will validate the artifact path, target metadata, SHA-256, board identifier, and cable detection before invoking openFPGALoader. Ambiguous multiple-board detection stops with instructions to select one explicitly.

M1 performs only volatile FPGA configuration. It will not write persistent flash, modify the HPS, or alter an SD card.

## Verification Gates

M0 is complete when:

- a clean checkout can build or locate all pinned OSS tools through the documented bootstrap;
- tool identities match the lock;
- `make doctor` clearly reports OSS, oracle, and hardware readiness;
- current CLI syntax and exact-device support are captured from the pinned binaries;
- no Quartus executable is needed for toolchain, simulation, or OSS builds.

M1 is complete only when all of these gates pass in order:

1. Verilator lint and a deterministic counter simulation pass.
2. Yosys synthesis succeeds with no unexpected hard blocks.
3. nextpnr-mistral places and routes for the exact device with no unrouted nets.
4. Timing meets the checked-in 50 MHz constraint.
5. Mistral emits a nonempty RBF and the build manifest is complete.
6. Quartus Lite 17.0.2 compiles the identical experiment as an oracle.
7. openFPGALoader programs the OSS RBF.
8. The operator observes the documented LED cadence.
9. The observation, tool versions, artifact hash, and result are recorded in `docs/bringup-log.md`.

Resource counts and RBF bytes need not match Quartus. Comparison is structural and behavioral: build status, unexpected primitives, utilization, timing, decoded resource classes where useful, and hardware behavior.

## Next Boundary

Raster/video design does not begin until the OSS-generated blinky RBF is visibly verified on a DE10-Nano and the working state is committed. If blinky fails, that failure becomes the project until it is reduced and either fixed or documented as a precise upstream blocker.

## References

- [Altera Quartus Prime Lite 17.0 for Linux](https://www.altera.com/downloads/fpga-development-tools/quartus-prime-lite-edition-design-software-version-17-0-linux)
- [MiSTer: Compiling for MiSTer](https://mister-devel.github.io/MkDocs_MiSTer/developer/mistercompile/)
- [MiSTer: Useful Links, including Quartus Lite 17.0.2](https://mister-devel.github.io/MkDocs_MiSTer/developer/links/)
- [Yosys](https://github.com/YosysHQ/yosys)
- [nextpnr](https://github.com/YosysHQ/nextpnr)
- [Mistral](https://github.com/Ravenslofty/mistral)
- [openFPGALoader](https://github.com/trabucayre/openFPGALoader)
