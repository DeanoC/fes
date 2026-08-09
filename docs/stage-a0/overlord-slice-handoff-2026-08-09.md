# Overlord DE10-Nano/Cyclone V slice handoff

## Evidence class

This is a **Software-tested / local-only** Stage A handoff. It records the
exact external repository observations and the capability probe result. It is
not an Overlord-generated image, a reproducibility result, HIL evidence, or an
Accepted hardware comparison.

The governing Stage A requirement is in [ROADMAP.md](../ROADMAP.md). The
Stage A0 Main candidate lock remains the input contract; its current decision
is recorded by the hash-named [candidate review](main-lock-reviews/415445d1871a25b6f6f9f30d52deec640147beea6a23d8527ddc5741f4ae789a.md).

## Pinned source observations

| Input | Repository | Commit | Tree |
| --- | --- | --- | --- |
| Overlord generator | `deanoc-overlord` | `9b5a2fb375b7078589c0c245588d7533a2e34227` | `1159c9fa6a0222a6756c2aa124147e27f1250d3d` |
| Standard resources | `deanoc-ikuy-std-resources` | `1cdfbda8f1bb3ca4df37f955ce39c8c850a946f9` | `358a2b1e1588b48c1a8c3b44dceb76774c5e9aa7` |

The repository locators are the public HTTPS authorities recorded in
[stage-a0-overlord.lock.toml](../../build/stage-a0-overlord.lock.toml). The
local checkouts are ignored development evidence; their physical paths do not
enter this handoff.

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
the pinned resource checkout, the observed result is `status=blocked`,
`generation=not-run`.

| Required capability | Result | Blocker |
| --- | --- | --- |
| DE10-Nano board definition | Missing | `OVERLORD_BOARD_DE10_NANO_MISSING` |
| Cyclone V SoC definition | Missing | `OVERLORD_SOC_CYCLONE_V_MISSING` |
| Cyclone V register map | Missing | `OVERLORD_REGISTERS_CYCLONE_V_MISSING` |
| `arm-none-linux-gnueabihf` toolchain configuration | Missing | `OVERLORD_TOOLCHAIN_ARM_NONE_LINUX_GNUEABIHF_MISSING` |
| Main_MiSTer software dependency closure | Missing | `OVERLORD_SOFTWARE_MAIN_MISTER_MISSING` |

The probe includes a synthetic complete-catalog fixture in its shell test; that
fixture only proves the probe's capability classification and is not a claim
about the real resources.

The pinned real-checkout probe report SHA-256 is
`47cc82ee5bb3786442c24e3e04db7f5ecefd30256a084dd2cb456d8a59024803`.

## Required next work

Add the minimum resource definitions to the selected canonical catalog and
then run the actual pinned Overlord generator. The generated output must be
captured as a new, hash-bound report and compared to the Stage A0 Main inputs:

- DE10-Nano board and Cyclone V HPS/FPGA topology;
- the memory/register map needed by `Main_MiSTer`, including the existing
  Cyclone V address constants;
- the ARM hard-float Linux toolchain configuration;
- the software dependency closure for the current Main build; and
- generated output hashes and configuration provenance.

Unexpected resource additions, address changes, privilege changes, compiler or
linker changes, or dependency-closure differences remain gate failures. Do not
mark this handoff Reproducible or HIL-observed until those checks and the
separate physical comparison have been run.
