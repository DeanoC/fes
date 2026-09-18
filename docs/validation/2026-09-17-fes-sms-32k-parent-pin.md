# FES SMS 32 KiB parent pin — 2026-09-17

## Scope

Record the sealed 32 KiB `fes.sms` HIP/nextpnr producer identity on the current
FES parent pin. `fes.sms` Format2Recipe registration from the `#65` pin stays.
The factory image closed set is unchanged.

FES `#75` merged the matching misteross gitlink and blob-stream consumers.
This request's remaining unique coverage is documentation and the SMS
`SEED = 1` producer assertion. It does not restore a hardcoded Git-SHA pin
test.

Worktree: `/Users/clawzai/Developer/fes-wt-sms-32k-fes-pin` on
`feat/fes-sms-32k-parent-pin`.

## Rebase after FES #75 (2026-09-18)

- FES `#75` MERGED by DeanoC at `2026-09-18T09:38:37Z`.
- `origin/main` tip `d945e24ab40ac631987ba76229986c8bfe6c2b36`.
- misteross gitlink `0825da5f277648009c2caa21c591e8b8bbca212a` (includes
  merged `#66` `be3b0836fd18fa24dae8f2eaec152c6be4c8f7a3` and sealed tip
  `d6477813923ea894266a3f5c264f71a87dcac8fa`).
- Matching consumers already on main: mister-packages
  `fdc4ece2e1fa87035ddca8cd147c621e7edcce3b`, libmister-runtime
  `a6d658cd305c4a84860afc1f8b00a2798ee6e4f4`, FogCast
  `d9745ed746a1e8d0bde423151d08810248ce8815`.

Sealed `fes.sms` package identity from the HIP/nextpnr producer (seed 1;
Coleco/SG-1000 stay seed 4):

- package_id `6e172ee279a69d6c7326009c7a8a82ab496e56ce2cb5ac2f2e7e3252d7b194d7`
- BUILD_ID `d8d476dacaaa48fe6cb57f45ba8ee61f`
- payload/RBF sha256 `2a245d6e8a5d73b52f86c6b7c8a802019ecfd43d1ccf3ed29eb3d9d6d15d0090`
- media diagnostic sha256 `411c33162658bf0bba55f5745565ee023c6bb6f5190a57f9a3b3ea5e2c484835`
  (`build/diagnostics/fes-sms/graphics-i-hil-32k.rom`)
- `.fcore` sha256 `fd7cce133d3fbbef952eebac9425cb71c2438a0730fd9a425a09838531ec28b6`

Recipe registration is unchanged:

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
core order, and media closed-set wiring are unchanged.

This record is parent pin evidence only. Quartus was not run here.
No kit HIL, media write, tip-image rebuild, FogCast allowlist PR, or
physical acceptance is claimed.

## Verification results

| Check | Result |
|---|---|
| `make check` | PASS — `package YAML valid; 18 generated consumers, 15 fixture copies and 4 copied source pins match` |
| focused parent unittests | PASS for pin/recipe/factory-set — `tests.test_recipes` (four HIP descriptors including `fes.sms`), `tests.test_image_assembly` factory set still `fes.pong`/`fes.zx81`/`fes.coleco`, `tests.test_bundle` selected-source identity plus SMS `SEED = 1` (no hardcoded Git-SHA), `tests.test_core_build` selection/env/`FES_SMS_PACKAGE_*` arguments. Known macOS `test_package_publication_fsyncs_payloads_and_replacement_boundaries` (`/tmp` vs `/private/tmp`) still fails; it is not a pin regression. |
| `git diff --check` | PASS |

No Quartus, no `make build`/`make dev`, no kit lease, no media write.

## Acceptance boundary

The sealed 32 KiB producer is already named by main via `#75`. Package-only
host-library acceptance of this 32 KiB package, a factory-image closed-set
expansion, and physical-target HIL remain separate lanes. Do not merge this
documentation/tests request until creating-team review and Bob GO.
