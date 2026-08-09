# Stage A0 checkpoint — 2026-08-09

## Current classification

Stage A0 is **Software-tested**, not Reproducible, HIL-observed, or Accepted.
The build-repeatability gate now passes. A durable GHCR build image has been
published and exported by workflow, but the retained local captures still use
the disposable local image; promotion remains blocked by material/license
closure, the final lock, and the full hardware/shared-memory topology.

## Closed in this checkpoint

- The public `DeanoC/Main_MiSTer` fork is pinned at commit
  `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` and its HTTPS commit is
  retrievable.
- Two fresh captures from distinct workspace roots completed in the pinned
  network-disabled container and toolchain. The retained comparison is
  `artifacts/stage-a0/independent.Ta6PwW/independent.json`; it reports
  `two_builds_byte_identical = true` with final hashes:

  - `bin/MiSTer`: `f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e`
  - `bin/MiSTer.elf`: `51a9864bb12ebdf8961b30ac0a45d533fd2a2885a96d81fc29f8363d1804706d`

- The published Overlord/resource fork inputs are pinned at Overlord
  `1a358e5222d9b4cecfcbf9d18dca0d3db2a4b41a` and resources
  `cfa6b1ecbbbaac0ae0da0ed1686795eb2f0792b3`. The resource slice now has a
  Cyclone V SoC instance, directly used register banks, corrected system
  manager offsets, Makefile-matched `gcc`/`ld`/`strip` metadata, and an
  explicit Main build adapter plus dependency manifest.
- The generated slice is retained under
  `artifacts/stage-a0/observed/overlord-run/slice-v5/`; the updated capability
  probe is `artifacts/stage-a0/observed/overlord-probe-slice-cfa6b1e.json`.
- ELF policy observation now separates the six bundled shared-library inputs
  from Main source material and uses a reversible logical name for
  `libstdc++.so.6`.
- The tracked container workflow completed successfully in run
  [31312562553](https://github.com/DeanoC/FogCast-POC/actions/runs/31312562553),
  publishing the digest-pinned image
  `ghcr.io/deanoc/fogcast-stage-a0-firstbuild@sha256:ed821006efd42153736b57caf44a4ed571b8949ac6ec47db42fd3fec9cccc1c5`
  with config digest
  `sha256:8e94815d34cd5522f5aba74ce5fc47ab3004f79ab27702c2d13b96766a703338`.

## Open gates

1. **Component material/license closure:** bundled libraries, copied headers,
   the logo, prebuilt `.so` files, toolchain packages, and the build utilities
   still need exact corresponding-source and license records. No blanket
   GPL/MIT assignment is accepted.
2. **Final lock/material catalog:** the current policy output is still a
   candidate observer result. The promoted-lock scratch file is not a release
   lock and is deliberately not part of the checkpoint.
3. **Topology closure:** the resource slice still needs the full 16 MiB
   `/dev/mem` aperture and Main's core/shared-memory windows, plus a real
   HPS-to-FPGA/core graph when that core definition exists.
4. **HIL:** no hardware equivalence claim has been made. HIL follows a valid
   final lock and durable artifact/material retrieval.

The durable-image publication is therefore a closed infrastructure step, not
yet a promoted build input. See the [exit decision](exit-decision-2026-08-09.md)
for the bounded next sequence and the reason to stop rerunning unchanged
verification.

The next promotion attempt must regenerate the policy/material catalog from a
fresh capture, add component-level records, and validate every dependency hash
against those records. Passing syntax or another byte comparison alone does not
change this classification.
