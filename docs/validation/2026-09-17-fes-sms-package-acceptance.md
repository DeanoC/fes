# FES SMS package-only host-library acceptance — 2026-09-17

## Scope

Package-only host-library admission of the sealed `fes.sms` archive on tip
FES, using the isolated/loopback package-acceptance lane (`#62` /
`#64`) without a kit lease, image rebuild, or factory closed-set expansion.

Worktree: `/Users/clawzai/Developer/fes-wt-sms-package-acceptance` on
`feat/fes-sms-package-acceptance`, tracking `origin/main` at
`d4e4f8c7c096ce0a8f3a1a2c444166e4f52f3565` (contains merged FES `#65`
`35f9d8219465e9302c1aa34e2046e33b04c263eb`).

This record does not claim mister, HDMI, controller, media, or exact-image
hardware acceptance. No kit lease was taken.

## Identities

| Item | Value |
| --- | --- |
| FES tip | `d4e4f8c7c096ce0a8f3a1a2c444166e4f52f3565` |
| misteross gitlink | `18c064bb200ee9f58d8e14e98d62cc0d02fe5d19` (misteross `#65`) |
| FogCast gitlink | `2b68cfa77a9fb022477b0aa456490dddc6a015a8` |
| core_id | `fes.sms` |
| package_id | `c9f2f7d71e77ab6153ddf7224d2beabede6258c1265bb3bfcfa2b5641e1de1b4` |
| BUILD_ID | `7088fdb52c3ea75b636d13d07a46587a` |
| archive | `/Users/clawzai/tmp/fes-sms-package/fes.sms.c9f2f7d7.fcore` |
| archive sha256 | `f172d94d64f1c0d9301f244cb7c3c671961948b271895ecc8b3f3223f3bdc4b2` |
| archive bytes | 2579968 |
| payload/RBF sha256 | `b95fb1af1825ed62cb7a71f076f0a7ec7aba4d9547584b9cada7fb9bfb03878e` |
| ABI | `fes.simple-computer` 1.0 |
| producer revision | `9c6dc963b9ad2ce4ab3e6f24df04137821ce9466` |

`package_id` was recomputed from the archive bytes with the format-2 identity
(`FES-CORE-PACKAGE-2` + little-endian lengths + manifest + payload) and
matched the sealed ID. The parent FogCast pin uses the same identity
construction in `corepackage/package.go` and has no named-core allowlist;
`core.id` is an identifier, not a factory-image selector.

Factory profile `native-integration-dev` remains the ordered closed set
`fes.pong`, `fes.zx81`, `fes.coleco`. `fes.sms` stays a recipe-registry /
host-library package.

## Commands

```sh
make package-acceptance PACKAGE_ACCEPTANCE_ARGS='--help'
make package-acceptance-isolated PACKAGE_ACCEPTANCE_ISOLATED_ARGS='--help'
python3 -m unittest tests.test_package_acceptance tests.test_recipes -v
TMPDIR=/private/tmp python3 -m unittest tests.test_package_acceptance_isolated -v
python3 -m unittest tests.test_bundle.BundleTest.test_selected_misteross_pin_enables_shared_toolchain_cache \
  tests.test_image_assembly.ImageAssemblyTest.test_default_package_contract_is_documented_as_the_complete_fes_set -v
```

Local read-only inspect (no host API, no mister):

```sh
fogcast --json core-inspect /Users/clawzai/tmp/fes-sms-package/fes.sms.c9f2f7d7.fcore
```

Loopback import / new-entry select / launch / Stop of this `package_id`
against the tip HTTP fixture (no mister): receipt
`/tmp/fes-sms-package-acceptance/loopback-receipt.json`.

Without `--execute`, the runner refused mutations and wrote no receipt.

## Verification results

| Check | Result |
| --- | --- |
| `fes.sms` recipe on tip | PASS — `scripts/recipes.py` HIP descriptor, producer `scripts/build_fes_sms_oss.py`, lock `cores/fes-sms/toolchain.lock` |
| misteross pin `18c064bb` | PASS — gitlink and `sources/misteross/scripts/build_fes_sms_oss.py` / `cores/fes-sms/toolchain.lock` present |
| factory closed set unchanged | PASS — profile and `test_default_package_contract_is_documented_as_the_complete_fes_set` still `fes.pong` / `fes.zx81` / `fes.coleco` |
| `make package-acceptance --help` | PASS (exit 0) |
| `make package-acceptance-isolated --help` | PASS (exit 0) |
| refuse without `--execute` | PASS — `refusing session mutations without explicit --execute` (exit 2), no receipt |
| `tests.test_package_acceptance` | PASS — 33 tests (FES `#62` loopback import/select/launch/Stop) |
| `tests.test_package_acceptance_isolated` | PASS — 21 tests with `TMPDIR=/private/tmp` (FES `#64`; the persist case is the known macOS `/tmp` vs `/private/tmp` path class without that TMPDIR) |
| `tests.test_recipes` | PASS — four HIP descriptors including `fes.sms` |
| format-2 identity of this archive | PASS — derived `package_id` matches `c9f2f7d7…` |
| `fogcast --json core-inspect` | PASS — admitted `core.id=fes.sms`, `package_id=c9f2f7d7…`, `build.id=7088fdb5…` |
| loopback runner for this `package_id` | PASS — uploaded exact 2579968 bytes; new entry `FES Master System`; compatibility path `/api/v1/core-packages/c9f2f7d7…/compatibility`; launch `sms-new-game`; Stop idle |

Inspect JSON sha256
`94057e89f82274e918900ab6cde9992204dd00f2de4fa8869edb1dbfca58deb0`.
Loopback receipt sha256
`280a67387ab45baa03d0c67da2dd7cbc03d6f091ef6d8e6ea18b74eedcf96f4d`.

Runner/test file sha256 on this tip:

| File | sha256 |
| --- | --- |
| `scripts/package_acceptance.py` | `9cafe9e804652ed59e98819d0c793211b714d77422d9a7a26df073ac2b22d1e4` |
| `scripts/package_acceptance_isolated.py` | `577c0b90f429b209617fe6cf3282cd01c9788de38a1e28892f3dcda0e08ea722` |
| `tests/test_package_acceptance.py` | `2925221c18f91fab5d45ca6953dd225856afc3b7f2b8d37281901ef51ac5f376` |
| `tests/test_package_acceptance_isolated.py` | `2d5036f113245d451a7e79af47c21dff952a62552aaf66a79dd34b620c4dd9bf` |

`core-inspect` used a local FogCast CLI
`revision=54f6cf5baf27efcd00759b1ae630b2a54693da6b`
(sha256 `e144d9a441d9f364fd623e97763888254769f584de447d676a35c7f4e0710141`).
That is not the parent FogCast gitlink; it is local format-2 admission only.
The independently derived package ID matches the pin's identity function.

No Quartus, no `make build` / `make dev`, no FAT / `linux.img` rebuild, no
FogCast allowlist change, no mister `kit.py` lease, no `mister:8182`
mutation, and no persistent Powerboat `fogcast-api.service` enablement.

## Acceptance boundary

Host-library admit for this sealed `fes.sms` package is proven on tip FES:
recipe + misteross pin, local inspect, and the package-acceptance
import/select lifecycle against the loopback fixture, plus the `#64`
isolated-host unit suite.

Physical kit launch/Stop, HDMI/controller/media, and any factory four-pack
image expansion remain separate lanes and were not run.
