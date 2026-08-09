# Overlord DE10-Nano/Cyclone V slice handoff

## Evidence class

This is a **Software-tested / durably-retrievable** Stage A handoff. It records the
exact external repository observations, a pinned resource-slice fork,
and an actual Overlord generation run. It is not a complete Main_MiSTer
dependency closure, a reproducibility result, HIL evidence, or an Accepted
hardware comparison.

The governing Stage A requirement is in [ROADMAP.md](../ROADMAP.md). The
Stage A0 Main candidate lock remains the input contract; its current decision
is recorded by the hash-named [candidate review](main-lock-reviews/415445d1871a25b6f6f9f30d52deec640147beea6a23d8527ddc5741f4ae789a.md).

## Pinned source observations

| Input | Repository | Commit | Tree |
| --- | --- | --- | --- |
| Overlord generator | `deanoc-overlord` | `1a358e5222d9b4cecfcbf9d18dca0d3db2a4b41a` | `1cb0c14f581822e3606a35f631c7f56337405bba` |
| Standard resources | `deanoc-ikuy-std-resources` | `cfa6b1ecbbbaac0ae0da0ed1686795eb2f0792b3` | `f187c36eaec69240a15f6e46cae9c0ee9cfafb69` |

The repository locators are the public HTTPS authorities recorded in
[stage-a0-overlord.lock.toml](../../build/stage-a0-overlord.lock.toml). The
local checkouts are ignored development evidence; their physical paths do not
enter this handoff. Both commits are published on the pinned public fork
branches and are durably retrievable. Main_MiSTer remains a separate
local-only candidate until its material and license catalog is closed.

## Probe result

The probe command is:

```sh
scripts/stage-a0-overlord-probe.sh \
  --overlord <pinned-overlord-checkout> \
  --resources <pinned-resource-checkout> \
  --lock <stage-a0-overlord.lock.toml> \
  --output <new-report-file>
```

It runs without network access, requires clean checkouts whose commit/tree
match the pinned lock, and emits `fogcast.stage-a0.overlord-probe.v1`. Against
the local slice, the observed result is `status=ready-for-generation`,
`generation=not-run` (the probe classifies inputs; it does not run Scala).

| Required capability | Result | Blocker |
| --- | --- | --- |
| DE10-Nano board definition | Present | — |
| Cyclone V SoC definition | Present | — |
| Cyclone V register map | Present (direct Main register slice; aperture/shared-memory topology remains open) | `OVERLORD_REGISTERS_CYCLONE_V_APERTURE_INCOMPLETE` |
| `arm-none-linux-gnueabihf` toolchain configuration | Present (convention) | — |
| Main_MiSTer software dependency closure | Present (explicit adapter and manifest; native Overlord compilation remains unsupported) | `OVERLORD_SOFTWARE_MAIN_MISTER_NATIVE_BUILD_ADAPTER_REQUIRED` |

The probe includes a synthetic complete-catalog fixture in its shell test; that
fixture only proves the probe's capability classification and is not a claim
about the real resources.

The updated slice probe report is
`artifacts/stage-a0/observed/overlord-probe-slice-cfa6b1e.json` with
SHA-256 `424b1709e24ae1a2e12ea7d6400e30111ad136cfc527b18883b44a7a672e5eec`.

## Generation result

The pinned Overlord binary was built from the pinned fork with Java 21/SBT and
run without network access:

```sh
target/universal/stage/bin/overlord generate report \
  <resource-checkout>/fogcast-stage-a0-de10-nano.yaml \
  --board de10_nano
```

The generated report and selected headers are retained under the ignored
`artifacts/stage-a0/observed/overlord-run/slice-v5/`; generated headers remain
in the ignored resource checkout output. Stable hashes from this run are:

| Output | SHA-256 |
| --- | --- |
| Overlord `report.txt` | `27ac6a5bfd6343e6704140a7aab92346780647d34f3149ef8d93e903e92e15d8` |
| generated Main memory map | `784c6b5769192a93ea681831674d3db929805c98501636ef8ff647b05fd90b37` |
| generated system-manager header | `126768b5e84612c2c21123c76ee342be80c9b6d6eb465092f68fdc804475b625` |
| generated bridge-window header | `c9002f7fcb99b07a8bb4f4db45742e5700cb6fbcfb601ae415ed41e82113478e` |
| generated FPGA-manager header | `04a54d73844dd0c5d1ec222d6d54e76379b6edbd0a9256b9342810414e1609f1` |
| generated reset-manager header | `6315956c181b1d27f2e9b03726272f6a73d4732abd826067a697ac165ef9b65a` |
| generated NIC301 header | `18f4792f191a314501832c486b97d0016e8c36ebf7518b29d316079a4ded1542` |
| generated SDR header | `bb3a172459486519122e2be9702ba380dc4cb0f39e02ea69e360a53802fb0edb` |
| generated FPGA-manager data header | `ab54a912cbaf4d4c4c7779e8123d1a39431dad69ae247b7db6f10febbab99b4a` |
| copied Main build manifest | `dc937b59892604f5a86ac96936cd7ff09e25f18ae6b758e8014a24c7fa039e91` |

The slice contains a DE10-Nano board definition, a Cyclone V SoC instance,
the directly used HPS register banks and bridge-window contracts from Main's
Cyclone V headers, an ARM hard-float Linux toolchain declaration matching
`gcc`/`ld`/`strip`, and software actions that emit the memory-map/register
headers plus a copied, provenance-bound Main build adapter and dependency
manifest. The register list remains partial for the full 16 MiB `/dev/mem`
aperture and core/shared-memory windows. The adapter is intentional: Overlord's
native software actions copy/link/render files but do not compile/link Main.

## Required next work

The next work is to extend this published slice against the remaining Stage A0
Main inputs:

- the full DE10-Nano HPS/FPGA and shared-memory topology;
- component-level source/license closure for Main's bundled and prebuilt
  libraries;
- generated output hashes and configuration provenance.

Unexpected resource additions, address changes, privilege changes, compiler or
linker changes, or dependency-closure differences remain gate failures. The
independent-build runner has now completed two fresh byte-identical captures,
but the result remains Software-tested because the candidate lock's
material/license catalog is not promotable. Do not mark this handoff
Reproducible or HIL-observed until component-level material closure, a valid
final lock, and the separate physical comparison have been run.
