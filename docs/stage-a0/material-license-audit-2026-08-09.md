# Stage A0 material and license audit — 2026-08-09

## Classification

This is a provenance research record, not a redistribution approval. It
narrows the remaining lock work to named materials and preserves the rule that
an upstream resemblance is not enough to assign a license. The reviewed
identity catalog now records an immutable authority, notice locator,
corresponding-source locator, and explicit `review-required` disposition for
each consumed material. Legal review is still required before any
`review-required` entry becomes redistributable.

The current catalog is the ignored evidence artifact
`artifacts/stage-a0/materials-reviewed-final.json`; the schema-valid durable
lock that references it is `build/stage-a0-main.lock.toml`. It contains 17
records: the container, Main source authorities, six policy files, six
prebuilt shared libraries, and the Arm toolchain.

## Main and bundled source

| Material | Observation | Candidate authority | Remaining decision |
| --- | --- | --- | --- |
| Main source | Root `LICENSE` SHA-256 `3972dc9744f6499f0f9b2dbf76696f2ae7ad8af9b23dde66d6af86c9dfb36986`; Main carries GPLv3 notices and mixed third-party subtrees | `DeanoC/Main_MiSTer` commit `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` | Review aggregate GPL obligations and split third-party materials |
| miniz | Local `lib/miniz/LICENSE` SHA-256 `0115478d567121238cf6cc1c0c361926cf07a49d9e4c9e66da97fac6a01646b3`; high-confidence lineage to `richgel999/miniz` 2.1.0, commit `a4264837ae37384b1d7a205a6732db322f0f3769` | Upstream Git history plus local diff | Confirm local amalgamation/diff and preserve the upstream notice |
| libco | Public-domain/custom notice is present in the upstream lineage; local `lib/libco` is retained | Upstream lineage to the libco project | Locate exact notice text and decide the project material record |
| LZMA SDK | Local `lib/lzma` retains the SDK public-domain marker associated with Igor Pavlov, 19.00 lineage | 7-Zip/LZMA SDK 19.00 source | Record exact archive/source locator and local delta |
| libchdr | `libchdr_cdrom.c` and `libchdr_bitstream.c` match `rtissera/libchdr` commit `d6f59e748c286508f535169824006b5813ff9d58`; other files have local edits | `rtissera/libchdr` | Review each local-edited file and its zstd/flac obligations |
| dr_flac | `lib/libchdr/include/dr_libs/dr_flac.h` matches `mackron/dr_libs` commit `39ce69188eab79a913aa23423eef9da5f3dcd142` (v0.12.42) | `mackron/dr_libs` | Confirm public-domain/MIT-0 notice path and redistribution text |
| zstd | Vendored `lib/zstd` is 1.5.5 lineage; upstream archive SHA-256 `9c4396…`, upstream `LICENSE` SHA-256 `705526…`, `COPYING` SHA-256 `f9c375…` | Facebook/Meta zstd 1.5.5 source | Add the BSD-3-Clause/GPL-2.0-or-later notice and exact source material |
| BlueZ headers | `lib/bluetooth` is BlueZ 5.43-era material; the library package is separately linked | BlueZ 5.43 source/package | Resolve GPL-2.0-or-later header/library split and exact source archive |
| Imlib2 header/library | `lib/imlib2/Imlib2.h` and the bundled image libraries are Imlib2 1.4.9-era material | Imlib2 1.4.9 source | Locate the custom MIT-like acknowledgement and source package |

The source-set policy currently assigns all 419 direct records to
`main-fork`; that is an observation of the checkout, not a completed
third-party material decomposition.

## Prebuilt shared libraries in the ELF closure

These are exact bytes observed in the fork and are separate policy material IDs
(`main-fork-lib…`). Their source package, build recipe, license notice, and
corresponding source are still open:

| SONAME | Version clue | SHA-256 |
| --- | --- | --- |
| `libImlib2.so.1` | Imlib2 1.4.9 lineage | `8745821f6936b3ccd75c7e3b546600edc0576093a9b14c7c17d1a322f40e8ae7` |
| `libbluetooth.so.3` | BlueZ 5.43 / Linaro GCC 6.2.1 build-path clues | `9cc252960ae8606d3d77249718126d33cddbad4ff1a82116b2929b88f22ba06a` |
| `libbz2.so.1.0` | bzip2 1.0.6 | `b630fc09d1e2f2072650419d37444b2fd90401cd63c0aac21e27422bf7ee122a` |
| `libfreetype.so.6` | Buildroot sysroot RPATH; exact patch level unresolved | `e9237dd7ebc50df8c17d474da9846c414fdacab2dd2f811f8ebcc0765dad0aaa` |
| `libpng16.so.16` | libpng 1.6.28 | `927e7f4d91b00e59fcf4cfa9802c75cb3a5186d5efdbdea3bebaeb1743aba951` |
| `libz.so.1` | zlib 1.2.11 | `0f1286438b87d7449d67f7d056ea7da83fbb41818f0e5178c39090ce44ed2694` |

The Buildroot path embedded in the FreeType/Imlib2 objects is evidence of
their historical build context, not a corresponding-source locator. It must
not be copied into a public lock as if it were an accessible source path.

## Toolchain and container

- The Arm archive is pinned by URL, size `104607124`, and SHA-256
  `102825ae56c9e00142d06f35d2bdd3299edb6060e84a275a25b095e66fd3fc2a`.
  The archive's package/license and source notices remain to be recorded.
- The durable build image is published by the tracked workflow at
  `ghcr.io/deanoc/fogcast-stage-a0-firstbuild@sha256:ed821006efd42153736b57caf44a4ed571b8949ac6ec47db42fd3fec9cccc1c5`,
  config `sha256:8e94815d34cd5522f5aba74ce5fc47ab3004f79ab27702c2d13b96766a703338`.
  Debian package manifests, licenses, and the relationship between the
  retained local capture image and this published image remain open.

## Remaining legal closure

The remaining legal review must retain one immutable record per material with:

1. exact source/archive/OCI identity and hash;
2. SPDX expression and notice locator;
3. corresponding-source locator or an explicit reason it is not required;
4. redistribution status and reviewer/date; and
5. a backlink from each policy dependency and lock material ID.

Until those dispositions are approved, the correct status is
**Software-tested** and the promotion report must retain
`MATERIAL_LICENSE_REVIEW_REQUIRED`.
