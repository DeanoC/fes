# Overlord DE10-Nano/Cyclone V slice handoff

## Evidence class

This is a **Software-tested / durably-retrievable** Stage A handoff. It records the
exact external repository observations, a pinned resource-slice fork,
and an actual Overlord generation run. It is not a complete Main_MiSTer
dependency closure, a reproducibility result, HIL evidence, or an Accepted
hardware comparison.

The governing Stage A requirement is in [ROADMAP.md](../ROADMAP.md). The
current Stage A0 Main input contract is the schema-valid durable lock in
[`build/stage-a0-main.lock.toml`](../../build/stage-a0-main.lock.toml); the
hash-named candidate review remains historical context.

## Pinned source observations

| Input | Repository | Commit | Tree |
| --- | --- | --- | --- |
| Overlord generator | `deanoc-overlord` | `a9fe9106dcc06db6d80eb22af0cd11facc8f7851` | `80793cfea3504edbac540ea2dd4c92ef58ac994d` |
| Standard resources | `deanoc-ikuy-std-resources` | `e13a97d8324baff83dd1cb8d4531e8648284fad7` | `3a284fd066f0b719c3a7e65901d3d4efe26cdf4c` |

The repository locators are the public HTTPS authorities recorded in
[stage-a0-overlord.lock.toml](../../build/stage-a0-overlord.lock.toml). The
local checkouts are ignored development evidence; their physical paths do not
enter this handoff. Both commits are published on the pinned public fork
branches and are durably retrievable. Main_MiSTer is a separate source
project; its Stage A0 software lock is now durably retrievable while
redistribution licenses remain review-required.

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
| Cyclone V register map | Present (direct Main register slice plus declared HPS/core-memory windows) | — |
| `arm-none-linux-gnueabihf` toolchain configuration | Present (convention) | — |
| Main_MiSTer software dependency closure | Present (explicit adapter and manifest; native Overlord compilation remains unsupported) | `OVERLORD_SOFTWARE_MAIN_MISTER_NATIVE_BUILD_ADAPTER_REQUIRED` |

The probe includes a synthetic complete-catalog fixture in its shell test; that
fixture only proves the probe's capability classification and is not a claim
about the real resources.

The updated slice probe report is
`artifacts/stage-a0/observed/overlord-probe-slice-v7.json` with
SHA-256 `357f5550181539a5eca40c3fa5aed176d04369323dcccc7f4089548cced466ce`.

## Generation result

The pinned Overlord binary was built from the pinned fork with Java 21/SBT and
run without network access:

```sh
target/universal/stage/bin/overlord generate report \
  <resource-checkout>/fogcast-stage-a0-de10-nano.yaml \
  --board de10_nano
```

The generated report and selected headers are retained under the ignored
`artifacts/stage-a0/observed/overlord-run/slice-v7-*`; generated headers remain
in the ignored resource checkout output. Stable hashes from this run are:

| Output | SHA-256 |
| --- | --- |
| Overlord `report.txt` | `6aca36a0e6288c55e092814b8641259d77966a502ca7cfc8fcd6da9804d737f1` |
| generated Main memory map | `3afd55292a0d851d8e1388c12c6786fdaa681b8712e0c3618ce56ce13a71a364` |
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
Cyclone V headers, a declared 16 MiB HPS register aperture and 512 MiB
core/shared-memory window, two address-level SoC-to-CPU bus links, an ARM
hard-float Linux toolchain declaration matching `gcc`/`ld`/`strip`, and
software actions that emit the memory-map/register headers plus a copied,
provenance-bound Main build adapter and dependency manifest. The graph is an
address-level resource contract; it does not claim that the complete FPGA
gateware core or HPS interconnect has been implemented. The adapter is
intentional: Overlord's native software actions copy/link/render files but do
not compile/link Main.

## Required next work

The next work is to extend this published slice against the remaining Stage A0
Main inputs:

- component-level source/license closure for Main's bundled and prebuilt
  libraries;
- generated output hashes and configuration provenance.

Unexpected resource additions, address changes, privilege changes, compiler or
linker changes, or dependency-closure differences remain gate failures. The
independent-build runner has now completed two fresh byte-identical captures
against the durable software-test lock. The result remains Software-tested
because license dispositions are explicitly review-required, the retained
comparison is local-only, and the separate physical comparison has not been
run. Do not mark this handoff HIL-observed or Accepted without that target
evidence.
