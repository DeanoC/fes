# Task 3 evidence report — package-only FES native integration

Date: 2026-09-13

Worktree: `/home/deano/fes/out/dev/fes-hip-format2/fes`

Branch: `feat/fes-hip-format2`

Base before this Task 3 change: `c1afea2327fe3efb5b0551985c4b82947f1be9ed`

Reviewed producer pin retained: `sources/misteross` at
`e1e6b6c36835b1c2ebb88fce356f44c2fed84b1e`.

Implementation commit: `7cd3d232d1d9586df59944f0849302d89ef4580b`.

The report covers only the changes in this worktree. No parent checkout, other worktree, upstream
checkout, hardware kit, Quartus build, or real image build was used.

## Result

The `native-integration-dev` profile is now an explicit `package-only` lane.
It retains the locked idle RBF and exactly one selected format-2 package,
currently `fes.pong`. The target-image selector still rejects ZX81, Coleco and
multiple package selections; their registry/resolver support remains available
for a later image-selector change.

The historical `native-dev` and `native-source-dev` profiles explicitly carry
`native_image_mode = "format1"`. Their existing generic format-1 behavior is
still behind that selected mode. Generic format-1 misteross targets themselves
were not deleted.

## Parent call graph

Before this change, the normal integration flow selected the four catalog
systems and reached:

```text
scripts/build.py main
  -> selected_cores
  -> build_bundles/build_bundle
  -> misteross fetch-core, rebuild-core, export-core-bundle
  -> image native fetch/install/verify with Mega Drive selection
```

The development flow also passed the four bundle arguments into the target
image. This was the old format-1 path, not merely an output naming choice.

The package-only flow is now:

```text
scripts/build.py main
  -> selected_packages (one fes.pong)
  -> resolve_selected_package / reviewed format-2 producer --cache-root
  -> image fingerprint includes the exact package inputs
  -> image Makefile with NATIVE_RUNTIME_MODE=package-only
  -> locked idle fetch + selected package fetch
  -> Buildroot native image without format-1 inputs
  -> sealed idle/package install and package-only verification
```

For `make dev`, `scripts/native_dev.py` passes only
`NATIVE_RUNTIME_MODE=package-only`, `FES_PONG_PACKAGE_DIR` and
`FES_PONG_PACKAGE_SELECTION` to the image path. Its package selection filename
is obtained from the recipe descriptor rather than being assumed by the
parent. `build_bundles_for_profile` returns an empty bundle set for this mode;
the historical bundle construction call remains available only through the
explicit format-1 branch.

The parent-side mode and dispatch boundary is in
`scripts/build.py:867-879` and `scripts/build.py:907-914,972-981,1001-1085`.
The development argument/output boundary is in
`scripts/native_dev.py:102-130,168-230`.

## Package-only image contract

`profiles/native-integration-dev.toml` now names `native_image_mode =
"package-only"` and selects only `fes.pong`. It no longer carries the
format-1 catalog core list, Quartus production metadata or bundle interface.

The package-only Makefile branch is selected by the explicit mode at
`image/Makefile:5-18,60-128`. It does not export
`NATIVE_RUNTIME_SYSTEMS`, `MEGADRIVE_RBF_*`, `PONG_RBF_BUNDLE`,
`SNES_RBF_BUNDLE` or `NES_RBF_BUNDLE`. The container, fetch, Buildroot
post-build and target verifier pass only the idle input and the current Pong
package.

The package helper preserves the closed package checks and read-only records:
the package directory must contain exactly one package directory with the
sealed manifest and `core.rbf`; the package selection must match byte-for-byte;
the installed selection is mode `0444`; and the installed package directory is
mode `0555`. The package-only rootfs has exactly two RBF files: the locked idle
RBF and the selected package RBF. Its runtime build-input record retains the
existing `format=1` runtime-record schema; that field is not an FPGA
format-1 artifact or a return to the retired bundle lane.

Known old top-level parent artifacts are removed on a successful package-only
publication/development rebuild and cause package-only verification to fail if
they remain. The three registered package selection filenames are handled as a
closed set by package publication; stale non-selected regular records are
removed, while symlink/non-regular destinations fail closed. The relevant
parent helpers are at `scripts/build.py:391-480,534-585`.

## Default-graph inspection

The exact dry-run command was:

```sh
FOGCAST_DIR="$PWD/sources/FogCast" \
  make -s -C image -n NATIVE_RUNTIME_MODE=package-only \
  target-image-native-fetch target-image-native-verify
```

It emitted only the lock-container prerequisite, package-only
`target-image-container.sh fetch`, package-only `build-target-image.sh
--fetch native-dev`, and package-only `verify-target-image.sh` commands. It
contained no `MEGADRIVE_RBF_*`, bundle variables, `NATIVE_RUNTIME_SYSTEMS`,
Mega Drive selection argument, or generic misteross command.

The repository search used for the boundary check was:

```sh
rg -n 'build_bundles|build_bundle|validate_bundle|fetch-core|rebuild-core|export-core-bundle|MEGADRIVE_RBF_|PONG_RBF_BUNDLE|SNES_RBF_BUNDLE|NES_RBF_BUNDLE' \
  scripts/build.py scripts/native_dev.py image/Makefile image/scripts \
  image/buildroot/board/fogcast-target/native-post-build.sh
```

The search still finds the definitions and command strings in the retained
historical format-1 implementation. Inspection of the call sites shows the
default `main` image branch is the earlier package-only branch
(`scripts/build.py:1028-1032`), while the direct generic bundle branch is
guarded by `mode == 'format1'` (`scripts/build.py:1033-1051`). The default
Makefile graph above is the independent proof that the image dispatcher does
not expose those commands.

## Tests and results

All commands below ran in the assigned worktree.

Focused parent tests:

```text
python3 -m unittest discover -s tests -p 'test_core_build.py' -v
Ran 37 tests ... OK

python3 -m unittest discover -s tests -p 'test_native_dev.py' -v
Ran 10 tests ... OK
```

The focused coverage includes explicit profile-mode isolation, package-only
bundle-dispatch rejection, all registered package environment scrubbing,
stale selection cleanup, legacy-output rejection/removal, package-only
development arguments, package selection publication and receipt checks.

The direct package-only shell regression passed:

```text
sh image/scripts/tests/native-package-only_test.sh
exit 0
```

The complete image shell suite passed:

```text
FOGCAST_DIR="$PWD/sources/FogCast" make -C image test
exit 0
```

This ran the package-only graph test plus all retained image shell suites
(13 shell test scripts total). The retained suites were invoked explicitly as
`NATIVE_RUNTIME_MODE=format1`, so their green result tests the historical
boundary rather than relying on the new default.

The full Python suite passed:

```text
python3 -m unittest discover -s tests
Ran 271 tests in 49.510s
OK (skipped=36)
```

The syntax and whitespace checks passed:

```text
python3 -m py_compile scripts/build.py scripts/native_dev.py scripts/environment.py
sh -n image/buildroot/board/fogcast-target/native-post-build.sh
sh -n image/scripts/build-target-image.sh
sh -n image/scripts/fetch-native-runtime-inputs.sh
sh -n image/scripts/native-extra-cores.sh
sh -n image/scripts/target-image-container.sh
sh -n image/scripts/verify-native-runtime-inputs.sh
sh -n image/scripts/verify-target-image.sh
sh -n image/scripts/tests/native-package-only_test.sh
sh -n image/scripts/tests/target-image-rootfs_test.sh
sh -n image/scripts/tests/target-image_test.sh
git diff --check
```

All exited successfully. No Quartus, synthesis, full image assembly or
hardware programming was run; these results are host/fixture/build-graph
evidence only.

## Remaining concerns and limits

- The target-image selector is intentionally Pong-only. ZX81 and Coleco are
  resolver-supported descriptors, not image-selected or hardware-accepted
  products in this task.
- Generic format-1 helpers remain for explicitly selected historical profiles.
  The search result for their command strings is therefore expected; the
  package-only mode boundary and profile tests are what prevent default use.
- The image package install path still has a Pong-specific installed selection
  name because the current selector supports one Pong package. A later
  multi-package selector must generalize that target contract before enabling
  ZX81/Coleco image selection.
- No claim is made about Quartus equivalence, HIP timing closure, an assembled
  production image, or physical kit acceptance. Those require the separate
  integration and exact-artifact gates.
