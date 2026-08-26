# Quartus Oracle Method

Quartus Prime Lite 17.0.2 is an optional reference compiler, not an open-tool
dependency. The oracle is accepted only after its exact version is reported;
the identical blinky RTL, target device, pinout, and 50 MHz timing intent are
used in both lanes. Oracle outputs remain under `build/oracle/<EXP>/` and
include logs, the RBF, fitter and timing reports, a normalized resource/timing
summary, and a schema-2 manifest.

## User-assisted installation boundary

No installer, proprietary archive, account flow, license acceptance, or
hardware operation is automated by this repository. If the user elects to
install the optional oracle, the two archive names and SHA-1 values to verify
before running an installer are:

| Bundle | SHA-1 |
| --- | --- |
| `Quartus-lite-17.0.0.595-linux.tar` (base) | `e71eeca4c8e1efaca902a58a37544c0572c6f45e` |
| `Quartus-lite-17.0.2.602-linux.tar` (Update 2) | `02aebab728d54e3ca8660d2646fdf93bc669b0ac` |

The base bundle is not itself version 17.0.2. Use the stable official download
page, complete any account/license prompts manually, and install to a path
without spaces as recommended by Altera:

<https://www.altera.com/downloads/fpga-development-tools/quartus-prime-lite-edition-design-software-version-17-0-linux>

Set only the explicit environment boundary after installation. Either of the
following documented layouts is accepted:

```text
QUARTUS_ROOTDIR=/path/to/17.0/quartus
  /path/to/17.0/quartus/bin/quartus_sh

QUARTUS_ROOTDIR=/path/to/17.0
  /path/to/17.0/quartus/bin/quartus_sh
```

`scripts/build_oracle.sh` does not search `PATH` and has no download or
installer fallback. It rejects a missing root, symlinked installation path,
path containing spaces, a missing executable, or any version other than the
exact `17.0.2`. The unavailable result is intentionally friendly:
`Quartus oracle unavailable; OSS and simulation remain usable`.

## Minimal project and command

`experiments/010_blinky/oracle/top.qpf` and `top.qsf` describe one `top`
entity for Cyclone V device `5CSEBA6U23I7`. The QSF reuses the shared
`experiments/010_blinky/rtl/top.v`, `boards/de10nano/pins.qsf`, and
`boards/de10nano/clocks.sdc` inputs, with the V11 clock, W15 LED0, 3.3-V LVTTL
assignments, RBF generation enabled, and incremental compilation disabled.
There are no QIP/QSYS or generated-IP assignments.

After the version and path checks, the wrapper stages that project below
`build/oracle/<EXP>/project/` and runs the only proprietary command in this
lane from that directory:

```text
<QUARTUS_ROOTDIR>/bin/quartus_sh --flow compile top
```

The wrapper copies only regular RBF, fitter, and timing report files to the
oracle output directory. It preserves the version, compile, normalization,
and manifest command logs and never writes outside the repository's ignored
`build/oracle/` tree.

Before compiling, it validates and removes only the exact staged
`build/oracle/<EXP>/project/output_files` directory. Symlinked paths or entries
fail closed, so stale reports cannot be reused and cleanup cannot redirect
outside the oracle tree. Attestation then requires the current compile to
produce the exact regular, non-empty `top.rbf`, `top.fit.rpt`, and `top.sta.rpt`
files; wildcard or cross-lane artifacts are not accepted.

Normalization is conservative. The timing gate reads the structured Fmax table
and uses exactly one `FPGA_CLK1_50` row's `Restricted Fmax` value; unrestricted
maximums, missing rows, and ambiguous rows do not pass. Direct fitted-resource
evidence is taken only from the physical rows emitted by the Cyclone V Fitter
Summary (`RAM Blocks`/`M10K` or block-memory rows, `DSP Blocks`, and `PLLs`).
The normal summary does not provide measured MLAB/LUTRAM or HPS rows, so those
classes use an explicitly labelled `static_exclusion` contract: the wrapper
records `used: null`, `available: null`, `status: excluded`, and the hashes for
exactly these scanned inputs:

```text
experiments/<EXP>/rtl/top.v
boards/de10nano/pins.qsf
boards/de10nano/clocks.sdc
experiments/<EXP>/oracle/top.qsf
```

It rejects any matching memory/HPS entity or any report row that claims a
measured count. Comparison checks the canonical per-class exclusion patterns,
the exact path set, and every nested hash against the corresponding manifest
source record; arbitrary patterns, stale hashes, and missing/extra paths fail
closed.
Each required class is present in `hard_block_evidence`; measured rows carry
`evidence_kind: fitter_summary` and `measured: true`, while static records carry
`evidence_kind: static_exclusion`, `measured: false`, and no fitted count or
fabricated capacity. Missing, malformed, ambiguous, or unrecognized evidence
fails closed.
The schema-2 build summary records the exact shared RTL, pin-QSF, and clock-SDC
SHA-256 values. It also records the absolute `quartus_sh` path, executable
SHA-256, exact `17.0.2` version string, version-output SHA-256, and a matching
Quartus tool pin record; comparison validates both provenance records.

## Comparison and acceptance

Run the comparison only after both manifests exist:

```bash
make compare EXP=010_blinky
```

This writes `build/compare/010_blinky/comparison.json` and
`comparison.md`. The semantic comparison aligns target, lane/build status,
ALMs/registers/hard blocks, requested and reported timing, RBF SHA-256, and
the optional hardware observation. A missing resource remains `absent`; it
is never rendered as a zero resource. Resource-count and RBF-byte differences
are informational.

The command fails closed only for the stated gates: a missing or failed build,
an unrouted design, timing below 50 MHz, an unexpected hard block or unknown
resource, a recorded failed simulation, missing or mismatched common source
hashes, malformed/absent required hard-block evidence, missing/malformed or
non-`17.0.2` Quartus provenance, or a missing, cross-lane, empty, or
hash/size-mismatched required RBF artifact. Resource counts and byte
differences remain informational, and absent resources are shown as `absent`,
never as zero. Static exclusions are rendered as `excluded (static)` in the
comparison table. The comparison does not claim hardware behavior until an
operator records that observation separately.
