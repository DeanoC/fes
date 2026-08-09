# Overlord DE10-Nano/Cyclone V slice handoff

## Evidence class

This is a **Software-tested / local-only** Stage A handoff. It records the
exact external repository observations, a pinned local resource-slice fork,
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
| Standard resources | `deanoc-ikuy-std-resources` | `fd653052fbaffbaced17e46a4e6af9c633942bb3` | `9b8e7b433af9f8b5e7111fe703566c0d1d231d4e` |

The repository locators are the public HTTPS authorities recorded in
[stage-a0-overlord.lock.toml](../../build/stage-a0-overlord.lock.toml). The
local checkouts are ignored development evidence; their physical paths do not
enter this handoff. Both commits are local fork commits based on the public
repositories and remain `source_availability = local-only` until published to
a durable HTTPS or content-addressed authority.

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
| Cyclone V register map | Present (minimum slice) | `OVERLORD_REGISTERS_CYCLONE_V_SLICE_INCOMPLETE` |
| `arm-none-linux-gnueabihf` toolchain configuration | Present (convention) | — |
| Main_MiSTer software dependency closure | Present (generator adapter only) | `OVERLORD_SOFTWARE_MAIN_MISTER_CLOSURE_INCOMPLETE` |

The probe includes a synthetic complete-catalog fixture in its shell test; that
fixture only proves the probe's capability classification and is not a claim
about the real resources.

The local-slice probe report is
`artifacts/stage-a0/observed/overlord-probe-slice-fd65305-b.json` with
SHA-256 `b5a6864af00bf14b45412e00ff3935023a2f0e6ba3856f8e1d7d269a4c2fcc76`.

## Generation result

The pinned Overlord binary was built from the pinned fork with Java 21/SBT and
run without network access:

```sh
target/universal/stage/bin/overlord generate report \
  <resource-checkout>/fogcast-stage-a0-de10-nano.yaml \
  --board de10_nano
```

The generated report is retained under the ignored
`artifacts/stage-a0/observed/overlord-run/slice/`; generated headers remain in
the ignored resource checkout output. Stable hashes from this run are:

| Output | SHA-256 |
| --- | --- |
| Overlord `report.txt` | `c8e03be173c49ec1dc1ab65b9571fec75604ef01f9ba5b10ae02022700b605b2` |
| generated Main memory map | `686ad243bec1d7a48ae0d9d35e359585b604eaba613f3764d31514f93cafb66f` |
| generated system-manager header | `d44d9a5ca6cc52f8087315db2fdaa88ea3f97c6e5972767d2c994362bfa163d5` |
| generated bridge-window header | `165d930d8e7e4d542c3b73128c06895b05bee63b828b139b106537d753790988` |

The slice contains a DE10-Nano board definition, Cyclone V SoC/CPU metadata,
the observed HPS-to-FPGA bridge windows from Main's Cyclone V headers, an ARM
hard-float toolchain convention, and a software action that emits a
Main-shaped memory-map header. The register list and software action are
deliberately partial: they demonstrate generation and address binding, not a
claim that every HPS peripheral or Main dependency is modeled.

## Required next work

The next work is to promote this local slice into a durable, reviewed catalog
and extend it against the Stage A0 Main inputs:

- DE10-Nano board and Cyclone V HPS/FPGA topology;
- the memory/register map needed by `Main_MiSTer`, including the existing
  Cyclone V address constants;
- the ARM hard-float Linux toolchain configuration;
- the software dependency closure for the current Main build; and
- generated output hashes and configuration provenance.

Unexpected resource additions, address changes, privilege changes, compiler or
linker changes, or dependency-closure differences remain gate failures. The
independent-build runner is now implemented, but cannot run against the
candidate lock until final-lock/material/license closure is valid. Do not mark
this handoff Reproducible or HIL-observed until those checks and the separate
physical comparison have been run.
