# FES SG-1000 closed-set pin — 2026-09-16

## Scope

Parent pin of merged misteross `#64` so `native-integration-dev` selects
the ordered closed format-2 set:

```text
fes.pong
fes.zx81
fes.coleco
fes.sg1000
```

Worktree: `/Users/clawzai/Developer/fes-wt-sg1000-fes-pin` on
`chore/fes-sg1000-closed-set-pin` from `origin/main`.
misteross gitlink: `bbbcef4c05b6e863dd89cf3ce65dfa0b8d085de8`.
Sealed `fes.sg1000` package_id from R14:
`da9b5039425ac4780e39d236265aa3f5e9d0978c15ff17c3fd473737ac599d1b`.
BUILD_ID `5c17f297712b6f57b63e4f400a79a2bb`.

FogCast, libmister-runtime and mister-packages gitlinks are unchanged.
This record is parent closed-set/recipe evidence only. Quartus was not run.
No kit HIL, media write, or physical acceptance is claimed.

## Verification results

| Check | Result |
|---|---|
| `make check` | PASS — package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match |
| `python3 -m unittest` closed-set modules | PASS — recipes, image-assembly, target-acceptance, bundle pin, consistency, native-dev, and the ordered four-package core-build cases |
| Linux image tests (Docker bookworm) | PASS — `native-package-only`, `target-image-rootfs`, `target-image`, plus the other non-Go image scripts listed below |
| `git diff --check` | PASS |

Image tests run in `python:3.12-bookworm`:
`native-package-only`, `native-runtime-inputs`,
`native-development-rbf-support-truth`, `kit-init`, `target-image-rootfs`,
`verify-target-image-cleanup`, `target-image`, `target-image-dev`,
`target-image-dev-container`, `target-kernel`.
`target-image-sources` was not run here (needs Go).
`rootfs-package-cleanup` was not claimed (Docker Desktop bind-mount
permissions). Host `stat -c` image tests do not apply on macOS.

`tests.test_core_build.CoreBuildTest.test_package_publication_fsyncs_payloads_and_replacement_boundaries`
and several `tests.test_media` cases still fail on this Mac the same way
against `origin/main` (`/proc/self/fd` and `/tmp` vs `/private/tmp`).
They are not regressions of this pin.

## Acceptance boundary

The parent contract now names `fes.sg1000` in the closed set and recipe
registry. Real cold package production, a release image, and physical-target
acceptance remain separate lanes.
