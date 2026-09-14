# FES multi-package image selection

## Goal

Allow the FES package-only image lane to install and verify an explicit,
ordered set of authenticated format-2 packages. The current integration
profile will select Pong, ZX81 and Coleco; smaller package sets remain valid
for focused development and fixtures.

## Contract

`profiles/native-integration-dev.toml` is the source of truth for the selected
package set. Its `fpga_packages` array contains unique `core_id` values in
image order. The supported values are `fes.pong`, `fes.zx81` and
`fes.coleco`. Selection is rejected for unknown IDs, duplicate IDs, malformed
entries, or a format-2 package in a historical format-1 profile.

The parent resolves each selected recipe independently. A package resolution
result retains its recipe, selection record, package directory and input
digests. The image fingerprint and `inputs.json` retain the ordered list of
package input records, so changing one package or the package order invalidates
only the image receipt while preserving each producer's independent cache.

The parent-to-image interface is explicit and environment-based:

- `FES_PACKAGE_IDS` is a comma-separated ordered list of selected IDs.
- Each selected recipe contributes its existing directory and selection
  variables (`FES_PONG_*`, `FES_ZX81_*`, or `FES_COLECO_*`).
- The container wrapper validates and mounts every selected package source and
  selection file; it never relies on ambient package or toolchain selectors.

The target image contains the locked idle RBF plus one sealed package directory
per selected package under
`/usr/share/mister-runtime/core-packages/<package_id>`. It contains the
corresponding sealed selection records named
`fes-pong.package.toml`, `fes-zx81.package.toml`, and
`fes-coleco.package.toml`. No format-1 RBF or selection is admitted in
package-only mode. The package helper rejects missing, extra, duplicate, or
misidentified package entries and emits build-input evidence in the selected
order.

The default FES producer lane remains HIP/nextpnr. Quartus is not invoked by
this change; it remains an oracle or bring-up check only where the recipe
documents that nextpnr lacks support.

## Boundaries

- This change does not alter the format-2 producer or cache key definitions
  established by PR #39.
- It does not add runtime catalog/UI selection; it only makes the image
  package set explicit and closed.
- Historical format-1 profiles remain isolated and unchanged.
- Hardware acceptance is out of scope. Verification covers exact package
  records, reproducible image structure, QEMU packaging checks and cache
  reuse; any physical target result is recorded separately.

## Acceptance criteria

1. Unit tests prove all three recipe IDs can be selected together, preserve
   order, reject duplicate/unknown/malformed selections, and produce a stable
   multi-package fingerprint.
2. Parent tests prove every selected recipe is resolved, passed to the image,
   published, and revalidated without dropping another package.
3. Shell tests prove the container mount contract, multi-package fetch/install,
   closed-set verification, stale format-1 rejection, and exact RBF counts.
4. `make check`, focused Python tests, and `make -C image test` pass.
5. The default profile contains Pong, ZX81 and Coleco, while no package-only
   path invokes format-1 bundle construction or Quartus.
