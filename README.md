# Open MiSTer FPGA development environment

This repository builds small experimental RBF files for the MiSTer/DE10-Nano
Cyclone V FPGA (`5CSEBA6U23I7`). It exists to make ordinary MiSTer core
development possible with both the open-source Mistral toolchain and Quartus.

## What works now

- Verilator simulation for the included experiments.
- A pinned repository-local Yosys, nextpnr-mistral, Mistral, and
  openFPGALoader toolchain.
- Open-source synthesis, place-and-route, and RBF generation.
- An optional Quartus Prime Lite 17.0.2 reference build using the same RTL.
- Semantic comparison between the OSS and Quartus outputs.
- `010_blinky`, a small LED counter.
- `020_linux_mailbox`, a small HPS GPI/GPO mailbox experiment.
- `030_m10k_rom`, an initialized table driving one LED through one M10K.
- `040_mlab_ram`, a 32-by-8 writeable table on HPS GP, mapped to eight MLABs.

Pinned third-party cores live in `cores.lock`. That file is the **upstream
version**: exact git commit plus the official release RBF hash.
`make fetch-core CORE=megadrive` checks those bytes out.
`make rebuild-core CORE=megadrive` compiles our own RBF from that tree
with Quartus Prime Lite 17.0.2. That rebuild has been loaded on real
MiSTer hardware, so the fetch → Quartus → RBF path works end to end.
The two artifacts are not required to bit-match (Lite cannot reproduce
Standard). `make select-core CORE=megadrive` copies the hardware-verified
rebuild to `build/current/megadrive.rbf`. `ARTIFACT=upstream` falls back
to the official release. The fetch checkout is not modified. Rebuild
identity is not a lock failure. Selection is an operator convenience, separate
from compilation and from the immutable FogCast handoff.

The useful outputs are ordinary local files:

```text
build/oss/<experiment>/top.rbf
build/oracle/<experiment>/top.rbf
build/cores/megadrive/releases/MegaDrive_20260603.rbf   # upstream
build/rebuild/megadrive/megadrive.rbf                   # our rebuild
build/current/megadrive.rbf                             # selected
build/bundles/megadrive/<rbf-sha256>/megadrive.rbf      # exported
build/bundles/megadrive/<rbf-sha256>/megadrive-rbf.toml # manifest
```

`make export-core-bundle CORE=megadrive` validates the rebuild and its
comparison evidence, then seals those two files in the digest directory. It
prints the completed absolute directory as the printed bundle path. FogCast
receives that printed bundle path, not a mutable build path such as
`build/rebuild/megadrive/megadrive.rbf` or `build/current/megadrive.rbf`.
FogCast owns transferring the bundle's RBF to the disposable MiSTer Pi and
loading it. This repository does not own FogCast deployment, target recovery,
hardware ownership, or network policy.

## Quick start

Check prerequisites and build the pinned OSS tools:

```sh
make toolchain-check
make toolchain
source scripts/env.sh
```

Build the mailbox experiment with the open toolchain:

```sh
make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
```

The resulting development core is:

```text
build/oss/020_linux_mailbox/top.rbf
```

If Quartus 17.0.2 is installed, build and compare the reference output:

```sh
make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
```

Fetch the upstream Mega Drive pin and compile the source-built RBF:

```sh
make fetch-core CORE=megadrive
make rebuild-core CORE=megadrive
make export-core-bundle CORE=megadrive
```

The export command is the FogCast handoff. For local operator use, select the
current RBF separately (rebuild is the default; upstream is the fallback):

```sh
make select-core CORE=megadrive
make select-core CORE=megadrive ARTIFACT=upstream
```

`build/current/megadrive.rbf` remains an operator selection and is not the
FogCast release handoff. The handoff is the printed digest directory under
`build/bundles/megadrive/`.

See `docs/oracle-method.md` for the explicit Quartus path and
`docs/linux-mailbox-development.md` for the mailbox experiment.

## Optional direct programming

`make program` remains an optional volatile diagnostic for a selected build.
It can use the resident Main command FIFO on a MiSTer target or an external
USB-Blaster/JTAG connection on a DE10-Nano. It is not the intended FogCast UI
path and never writes flash or the SD card.

```sh
PROGRAM_DRY_RUN=1 MISTER_HOST=misterpi MISTER_USER=root \
  make program EXP=020_linux_mailbox BUILD=oss
```

The old `dev-bundle`, `dev-load`, `dev-preflight`, and `dev-fault-inject`
transport was abandoned. It duplicated FogCast's responsibility and is absent
from this recovery branch; Git history retains it.

## Repository layout

- `experiments/`: RTL, simulation, constraints, and minimal Quartus projects.
- `boards/de10nano/`: shared device and pin constraints.
- `scripts/`: tool bootstrap, build, comparison, diagnostics, and optional
  direct programming.
- `toolchain.lock`: pinned OSS tool sources and commits.
- `build/`: ignored generated tools, reports, manifests, and RBF files.

The current build structure is described in `docs/architecture.md`.
