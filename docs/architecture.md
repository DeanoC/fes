# Current build architecture

misteross turns small experiment RTL into local MiSTer RBF artifacts. Network
deployment and target lifecycle are outside this repository.

## Build lanes

```text
experiment RTL + constraints
  |-- sim ----> Verilator result
  |-- oss ----> Yosys -> nextpnr-mistral/Mistral -> top.rbf
  `-- oracle -> Quartus Prime Lite 17.0.2 -------> top.rbf

oss manifest + oracle manifest -> compare report
```

`sim` checks the experiment's logical behavior with Verilator. Simulation jobs,
production source lists, and OSS synthesis flags come from the closed experiment
policy. Simulation-only models never enter either synthesis lane.

`oss` uses only the pinned repository-local tools described by
`toolchain.lock`. Generated sources and tools live under `build/toolchain/`.
Build output lives under `build/oss/<experiment>/`.

`oracle` uses an explicitly configured Quartus Prime Lite 17.0.2 installation.
It uses the same production RTL and timing intent as the OSS lane. Output lives
under `build/oracle/<experiment>/`. Quartus is not an OSS or simulation
dependency.

`compare` reads the two lane manifests and writes its result under
`build/compare/<experiment>/`. Differences between compiler-produced RBF bytes
are expected; the comparison focuses on target, sources, resources, timing,
and successful artifact production.

## Experiments

`010_blinky` is a 50 MHz counter driving one LED. It is the smallest physical
output test.

`020_linux_mailbox` uses one Cyclone V HPS general-purpose interface to return
the constant message `OSS FPGA OK\n`. It has no external FPGA output. The
simulation substitutes a small HPS model; both synthesis lanes use the real
HPS primitive boundary.

`030_m10k_rom` walks an initialized 256-byte table on the 50 MHz clock and
drives one LED from stored bit 0. OSS synthesis maps the table to exactly one
M10K. PLL, DSP, MLAB, and HPS remain forbidden.

`040_mlab_ram` is a 32-by-8 writeable table on the HPS general-purpose
interface. Linux peeks and pokes GPO/GPI; there is no LED. Yosys maps the
table to eight `MISTRAL_MLAB` cells. nextpnr packs those into LABs and does
not report an MLAB utilization key, so the closed policy counts the Yosys
cells and requires the HPS primitive in the route report. PLL, DSP, and M10K
remain forbidden.

## Artifact boundary

The integration outputs are:

```text
build/oss/<experiment>/top.rbf
build/oracle/<experiment>/top.rbf
build/cores/<name>/releases/*.rbf
build/rebuild/<name>/<name>.rbf
build/current/<name>.rbf
build/bundles/megadrive/<rbf-sha256>/megadrive.rbf
build/bundles/megadrive/<rbf-sha256>/megadrive-rbf.toml
```

The core workflow has three separate operations:

```sh
make fetch-core CORE=megadrive
make rebuild-core CORE=megadrive
make select-core CORE=megadrive
make export-core-bundle CORE=megadrive
```

Rebuild compiles the pinned source. Select copies an operator-chosen artifact
to the mutable `build/current/megadrive.rbf` convenience path. Export validates
the rebuild against its closed comparison evidence and writes an immutable,
content-addressed directory containing exactly `megadrive.rbf` and
`megadrive-rbf.toml`. FogCast receives the exporter's printed bundle path under
`build/bundles/megadrive/<rbf-sha256>/`, rather than a mutable rebuild or
selection path. No attestation record, run ID, recovery journal, or
fault-injection result is required.

Building an RBF never touches hardware. FogCast loads a local experiment RBF
on the designated native kit through `POST /api/v1/session/development-rbf`
(target `POST /v1/development/rbf` → native `load_development_rbf`). That path
programs the FPGA manager, then probes MiSTer SPI identity on the same
FPGA-manager GPO/GPI pair the HPS general-purpose experiments use. A
non-MiSTer image does not satisfy the probe; Stop restores idle with the
existing development reboot handshake. `make program` remains a separate
Main-FIFO or JTAG diagnostic and is not the native kit path.

## Pinned core trees

`cores.lock` is the upstream version pin: git identity plus the official
release RBF hash. `make fetch-core` checks out that exact commit under
`build/cores/<name>/` and hashes the official RBF. That hash check is the
lock test. It does not clone `HEAD`, does not reset dirty trees, and does
not run Quartus.

`make rebuild-core` copies the fetched tree into
`build/rebuild/<name>/project/` (excluding `.git` and prior compile
artifacts) and runs Quartus Prime Lite 17.0.2 `quartus_sh --flow compile`
on the locked project. The staged copy pins `BUILD_DATE` to the YYMMDD
from the locked release name (`MegaDrive_20260603.rbf` → `260603`) via
`MISTER_BUILD_DATE`; override with `--build-date`. The fetch checkout is
not modified. The produced RBF is copied to
`build/rebuild/<name>/<name>.rbf`. Its hash is recorded next to the
upstream hash; they are not required to match. Quartus is never taken from
`PATH`; `QUARTUS_ROOTDIR` is required.

The Mega Drive Lite rebuild has been loaded on real MiSTer hardware, so
fetch → Quartus 17.0.2 → RBF is a working path. Upstream remains the
fallback if a later rebuild is broken.

`make select-core` copies the rebuild to `build/current/<name>.rbf`.
`ARTIFACT=upstream` falls back to the official release. This selection is for
operator use and is not the FogCast release handoff.

`make export-core-bundle CORE=megadrive` accepts only the pinned Mega Drive
revision and the MiSTer ABI. It rehashes the rebuild and recipe, validates the
closed `compare.json`, writes the two-file bundle under its RBF digest, removes
all write bits from the files and directory, and prints the absolute bundle
path. FogCast owns which exported RBF is installed on a target.
