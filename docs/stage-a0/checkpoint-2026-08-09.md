# Stage A0 checkpoint — 2026-08-09

## Current classification

Stage A0 is **Software-tested**, not Reproducible, HIL-observed, or Accepted.
The final-lock schema, reviewed material identities, six complete policy
documents, and two fresh independent builds now pass. Promotion remains
blocked only by explicit license review and the fact that the retained
comparison is local-only; no physical/HIL claim has been made.

## Closed in this checkpoint

- The public `DeanoC/Main_MiSTer` fork is pinned at commit
  `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` and its HTTPS commit is
  retrievable.
- Two fresh captures from distinct roots completed in the pinned
  network-disabled durable-image transport and toolchain. The retained
  comparison is `artifacts/stage-a0/independent-durable-20260809/comparison.json`;
  it reports
  `two_builds_byte_identical = true` with final hashes:

  - `bin/MiSTer`: `f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e`
  - `bin/MiSTer.elf`: `51a9864bb12ebdf8961b30ac0a45d533fd2a2885a96d81fc29f8363d1804706d`

- The published Overlord/resource fork inputs are pinned at Overlord
  `a9fe9106dcc06db6d80eb22af0cd11facc8f7851` and resources
  `e13a97d8324baff83dd1cb8d4531e8648284fad7`. The resource slice now has a
  Cyclone V SoC instance, directly used register banks, corrected system
  manager offsets, Makefile-matched `gcc`/`ld`/`strip` metadata, and an
  explicit Main build adapter plus dependency manifest, and declares the HPS
  16 MiB aperture, 512 MiB core-memory window, and two address-level bus
  connections. This is a resource graph contract, not a claim of completed
  FPGA gateware.
- The generated slice is retained under
  `artifacts/stage-a0/observed/overlord-run/slice-v7-*`; the updated capability
  probe is `artifacts/stage-a0/observed/overlord-probe-slice-v7.json`.
- ELF policy observation now separates the six bundled shared-library inputs
  from Main source material and uses a reversible logical name for
  `libstdc++.so.6`.
- The tracked container workflow completed successfully in run
  [31312562553](https://github.com/DeanoC/FogCast-POC/actions/runs/31312562553),
  publishing the digest-pinned image
  `ghcr.io/deanoc/fogcast-stage-a0-firstbuild@sha256:ed821006efd42153736b57caf44a4ed571b8949ac6ec47db42fd3fec9cccc1c5`
  with config digest
  `sha256:8e94815d34cd5522f5aba74ce5fc47ab3004f79ab27702c2d13b96766a703338`.
- `build/stage-a0-main.lock.toml` is now a schema-valid durable software-test
  lock. `artifacts/stage-a0/materials-reviewed-final.json` names all 17
  consumed materials, including the six prebuilt shared libraries and the
  six policy files. The six reviewed policy copies are under
  `artifacts/stage-a0/policy-reviewed-final/`.

## Open gates

1. **License disposition:** every material has an SPDX review reference,
   notice locator, corresponding-source locator, and `review-required`
   redistribution status. A legal/redistribution review must replace those
   placeholders before publishing a redistributable image or binary.
2. **Durable comparison authority:** the independent report is produced from
   the durable lock but its capture/source availability remains local-only;
   a CI or separately retrieved run must establish the durable comparison
   authority if the stage requires it.
3. **HIL:** no hardware equivalence claim has been made. HIL follows the
   durable lock and a known-good comparator on the disposable `misterpi` kit.

The durable-image publication and software lock are therefore closed
infrastructure steps, not a redistribution approval. See the [exit
decision](exit-decision-2026-08-09.md) for the remaining bounded legal and HIL
work; another unchanged byte-comparison run is not an advancement.
