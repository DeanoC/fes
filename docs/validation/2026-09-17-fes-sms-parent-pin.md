# FES SMS parent pin — 2026-09-17

## Scope

Parent pin of merged misteross `#65` so FES can resolve the sealed
`fes.sms` format-2 producer without expanding the factory image closed set.

Worktree: `/Users/clawzai/Developer/fes-wt-sms-fes-pin` on
`feat/fes-sms-parent-pin` from `origin/main`.
misteross gitlink: `18c064bb200ee9f58d8e14e98d62cc0d02fe5d19`
(merge of DeanoC/misteross#65). Sealed `fes.sms` package identity from the
HIP/nextpnr producer at clean `9c6dc963b9ad2ce4ab3e6f24df04137821ce9466`
(tree of the merge includes the later docs commit `c43ef45`):

- package_id `c9f2f7d71e77ab6153ddf7224d2beabede6258c1265bb3bfcfa2b5641e1de1b4`
- BUILD_ID `7088fdb52c3ea75b636d13d07a46587a`
- `.fcore` sha256 `f172d94d64f1c0d9301f244cb7c3c671961948b271895ecc8b3f3223f3bdc4b2`
- payload/RBF sha256 `b95fb1af1825ed62cb7a71f076f0a7ec7aba4d9547584b9cada7fb9bfb03878e`

Recipe registration only:

```text
scripts/recipes.py FORMAT2_RECIPES["fes.sms"]
  producer scripts/build_fes_sms_oss.py
  authenticate _authenticate_sms_tools
  lock cores/fes-sms/toolchain.lock
  selection fes-sms.package-selection.toml
  env FES_SMS_PACKAGE_DIR / FES_SMS_PACKAGE_SELECTION
```

`profiles/native-integration-dev.toml` still selects the ordered factory
set `fes.pong`, `fes.zx81`, `fes.coleco`. Image scripts, target-acceptance
core order, and media closed-set wiring are unchanged. FogCast, libmister-runtime
and mister-packages gitlinks are unchanged.

This record is parent recipe/pin evidence only. Quartus was not run here.
No kit HIL, media write, tip-image rebuild, FogCast allowlist PR, or
physical acceptance is claimed.

## Verification results

| Check | Result |
|---|---|
| `make check` | PASS — package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match |
| focused parent unittests | PASS — `tests.test_recipes` (four HIP descriptors including `fes.sms`), `tests.test_image_assembly` factory set still `fes.pong`/`fes.zx81`/`fes.coleco`, `tests.test_bundle` pin `18c064bb`, `tests.test_core_build` selection/env/`FES_SMS_PACKAGE_*` arguments |
| `git diff --check` | PASS |

Parent `python3 -m unittest discover -s tests` on this Mac: 325 tests, 36 skipped. Remaining failures are the known macOS host class (`/tmp` vs `/private/tmp`, `/proc/self/fd` fsync in `test_core_build` publication, `tests.test_media` lease/path cases) plus delegated container errors. They are not recipe/pin regressions; the focused modules above passed.

No Quartus, no `make build`/`make dev`, no kit lease, no media write.

## Acceptance boundary

The parent contract now names `fes.sms` in the format-2 recipe registry and
pins misteross `#65`. Package-only host-library acceptance, a factory-image
closed-set expansion, and physical-target HIL remain separate lanes.
