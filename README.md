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
- `050_lut_mul`, an eight-by-eight unsigned product on HPS GP, kept in logic cells.
- `060_dsp_mul`, an eight-by-eight unsigned product on HPS GP. Yosys emits one
  `MISTRAL_MUL9X9`; nextpnr-mistral places one DSP BEL. Quartus measures one
  DSP block.
- `070_mixed_mem`, lab and block tables on HPS GP (eight MLABs and one M10K).
- Deterministic ROM-less Pong game and raster simulation with `make sim-pong`.
  `make build-pong` stages the pinned MiSTer framework and compiles the wrapper
  with explicitly configured Quartus 17.0.2. Outputs and provenance are under
  `build/rebuild/pong/`. The diagnostic build has passed native gameplay,
  controls and HDMI audio checks.

`make sim-pong` tests the standalone digital-control Pong game and continuous
320x240 raster with Verilator. Set `VERILATOR=/absolute/path/to/verilator` to
reuse an installed tool. `make build-pong` adds the MiSTer board wrapper and
produces a programmable RBF.

`cores.lock` also selects SNES Release 20260823. `make fetch-core CORE=snes`
uses the existing fetch/hash-check lane. A staged seed-3 Quartus diagnostic
passed timing and native LoROM/HiROM hardware checks. The normal SNES recipe
now explicitly selects fitter seed 3 and requires passing timing. Export admits
Mega Drive, SNES and Pong; new builds require their own hardware acceptance.
See the architecture document for artifact identities.

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

## Loading an experiment

FogCast owns transfer and FPGA load. The designated native kit has no
`/dev/MiSTer_cmd`. Claim a lease and stream a local RBF with `scripts/kit.py`
(see Shared native development kit below). Direct host
`POST /api/v1/session/development-rbf` is the same physical path when the host
already holds the lease. HDMI stays powered down. These experiments are not
MiSTer-compatible cores: after programming, the runtime probes SPI identity on
the FPGA-manager GPO/GPI pair, that probe fails, and Stop restores idle through
the existing development reboot handshake.

`make program` is a separate Main-FIFO or JTAG diagnostic for a conventional
MiSTer or DE10-Nano. It is not the native kit path and never writes flash or
the SD card.

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

## Shared native development kit

Use Python 3.11+ `scripts/kit.py` for an interactive development session against
FogCast's lease-enabled target agent. The agent owns the lease and cleanup;
this client only calls its existing native development-RBF API. Builds do not
claim a kit. Configure a private FogCast host TOML, or set `FOGCAST_BASE_URL`
and `FOGCAST_TOKEN` in your environment (never put tokens in command arguments).

```sh
python3 scripts/kit.py --config /absolute/path/config.toml status
python3 scripts/kit.py --config /absolute/path/config.toml session \
  --owner my-agent --purpose 'mailbox OSS bring-up'
```

At the session prompt, enter `load build/oss/020_linux_mailbox/top.rbf`, `status`,
`stop`, or `release`. Quote paths containing spaces. Repeat loads in the same
session. A non-MiSTer development image programs, then fails the MiSTer SPI
identity probe; `load` keeps the lease and reports `development probe timed out`
so the operator can peek GPI before Stop. Stop returns hardware to idle while
retaining ownership; release, EOF, or Ctrl-C requests cleanup and frees
ownership. Keep stdin open between commands (including when using an agent's
persistent terminal session). `make kit-session` is a convenience using the
environment configuration.

The client renews every 20 seconds and checks the held state, generation and
token on each grant. It measures relative expiry against its monotonic clock,
so a target without a working real-time clock is supported. If renewal fails it disables mutations and
ends the session; it never silently reacquires. If the process dies or cannot
release, the target's 90-second lease expires and the agent attempts cleanup.
Failed cleanup leaves the kit unavailable for inspection/recovery, rather than
handing uncertain hardware to another owner. A release response may show
`revoking` until cleanup completes; use `status` to confirm it becomes free.

An operator can deliberately replace a stuck owner using the generation shown
by `status`. The existing bearer credential is the operator credential in this
first version; owner names are descriptive, not authorization identities.

```sh
python3 scripts/kit.py --config /absolute/path/config.toml takeover \
  --owner operator --purpose 'mailbox bring-up' \
  --expected-generation GENERATION_FROM_STATUS --reason 'previous agent exited'
```

Takeover retries the same request for at most 60 seconds while cleanup runs;
it does not interrupt FPGA programming and never forces a different generation.
It opens a normal renewable interactive session once granted. An unconfirmed
claim can expire automatically; inspect status before trying again.

`make program` uses direct Main/SSH or JTAG and **bypasses this lease**. Treat it
as an explicit maintenance escape only after coordinating with the current
owner. Ordinary native bring-up uses `kit.py`; do not run both paths concurrently.
No build target uploads an RBF automatically. The native development loader
still requires a compatible MiSTer framework ABI; ownership does not make an
arbitrary bare experimental RBF compatible.

## Three-system bundles

Run `make export-core-bundle CORE=snes` after `make rebuild-core CORE=snes`, or
`make export-core-bundle CORE=pong` after `make build-pong`. Each prints a sealed
`build/bundles/<system>/<sha256>/` directory containing `<system>.rbf` and
`<system>-rbf.toml`. Pong export requires a clean committed source tree and
checks its local/framework input record. SNES and Pong export reject failed,
missing or stale timing evidence. Neither build nor export programs the kit.
