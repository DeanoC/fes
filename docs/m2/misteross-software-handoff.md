# M2 `misteross` Software Handoff

Evidence date: 2026-08-29

Evidence class: **Software-tested**

Hardware status: **Not run**

This handoff freezes the `misteross` side of the Linux mailbox development
flow. It is an input to the separate cross-repository integration plan; it is
not authorization to push, deploy, reboot a target, or perform HIL.

## Authority and source identity

| Item | Identity |
| --- | --- |
| Implementation commit | `8eee3e3b44fe15f1933604f6d26e72f0e5d3e7f4` |
| Implementation tree | `7dc7247700012b2e139e93e273877970c65ef080` |
| Approved-plan base | `6f58c27787c1dc7c74d8141e3e62fe8c7e44668d` |
| Loader design SHA-256 | `fe9f8ecc42f9be44a627863562f80a1c92eeb667d868be0071c858f4d388b514` |
| `misteross` plan SHA-256 | `c8cc2c2e2e8488e47cfadec8a0fe1a8b13f05b9e026e3c375f4737c18b2f1a6b` |

The evidence was generated in a detached clean worktree at the exact
implementation commit. The only extra tree was the pinned repository-local
toolchain under ignored `build/toolchain/`; the Git index and source worktree
remained clean.

## Verification results

| Gate | Result |
| --- | --- |
| Full Python suite | 323 discovered: 322 passed, 1 opt-in fixture skipped |
| Mailbox Verilator simulation | pass |
| OSS build | pass at 50 MHz; achieved 130.82156372070312 MHz |
| OSS determinism | two clean experiment builds produced the same RBF SHA-256 |
| Quartus oracle | pass; exact `17.0.2 Build 602` |
| Semantic comparison | pass; `failures=[]` |
| OSS and oracle development bundles | pass |
| OSS and oracle `dev-load` dry-runs | pass; 17 argv lines each |
| OSS and oracle `dev-preflight` dry-runs | pass; 6 argv lines each |
| OSS `dev-fault-inject` dry-run | pass; 13 argv lines |
| Python `compileall` | pass |
| `010_blinky` sim/OSS/oracle/compare regression | pass |
| Blinky source/manifest/program regression tests | 77 passed |

The dry-run target used an RFC 5737 TEST-NET address and synthetic hashes. The
transport emitted argv plans only; it made no network connection and created
no target or host run trace.

## Build and comparison artifacts

These generated artifacts are ignored and are not committed.

| Artifact | SHA-256 |
| --- | --- |
| OSS `manifest.json` | `cb6bafbbbe2a76f4c22b19f7abf18a924f4b3b664fdf14a2a97509fe4a9a0b6d` |
| OSS `build-summary.json` | `8ec7b7f2b619098d0f77ac43757860326eda1c8803c108b5b9fc7dff1bdfb25c` |
| OSS `top.rbf` | `49b82822a9586d8c55200c023598690147bba27a2fa1f93ab047f8b3fba6b800` |
| Oracle `manifest.json` | `2830eb6d11dc9c87b3a84ddd5f4720b46c43c63d8a710d6459d5412d7b7260e5` |
| Oracle `build-summary.json` | `816ec4058553ee926012cfa5f0acd14e0977e8b4dabfca72d236fc9f52f00a7d` |
| Oracle `top.rbf` | `4ffcb86cd53ed1602456270452462d903f15e174bb6a43a76ff3156532fc3f43` |
| Oracle `top.fit.rpt` | `451b377dce57412e2ec7eb9278060f716524f6bad6ffdc62b6cc94842be26ea9` |
| Oracle `top.sta.rpt` | `4662fba115fb49e6127b2af9f8244753a54006bd75fdb8be9720fa947dd1dde4` |
| Comparison JSON | `cbff5a5cc1f7662c8ac74575f7a0e15667e180f95a6289625647ea7755cf2ba2` |
| Comparison Markdown | `93dda7a2eceeb08eaeeeb3dc1ccba63a2d9fbb80d5ccb50932335585d086caad` |

The OSS RBF is 7,007,204 bytes. Its two-build SHA-256 match is the
reproducibility claim. The different Quartus RBF hash is expected; comparison
acceptance is semantic, not byte equality.

## Immutable development-bundle fixtures

The final clean-worktree fixtures use fixed non-production run IDs. They prove
bundle generation and give the integration plan reproducible software inputs;
the integration plan must allocate fresh run IDs for any target action.

### OSS fixture

Run ID: `0123456789abcdef0123456789abcdef`

| Member | SHA-256 |
| --- | --- |
| `manifest.json` | `81b54ccf2bf6f1c0fb253087eaaada2e21ae1648bff986d33775097271202ec3` |
| `resource_evidence.json` | `19ecb8344fbc5fb48772eb72c7e1ca70dadb656f62d7a6c6366b53ea56a2eb7a` |
| `top.rbf` | `49b82822a9586d8c55200c023598690147bba27a2fa1f93ab047f8b3fba6b800` |
| `bundle.sha256` | `ce19ccca5863db54b1caa2f87392765c699411ea550cccba5d7ee6e4eee6e6ea` |

### Oracle fixture

Run ID: `fedcba9876543210fedcba9876543210`

| Member | SHA-256 |
| --- | --- |
| `manifest.json` | `02fa4330903a2a0c6a29ccf1313b8a6d78f02659573494bcca575c51afb3d954` |
| `resource_evidence.json` | `155b6b6a33853613a7fb6c69fb90ebcf5e50e9b37f1cd687f9a0de70afdbb2c3` |
| `top.rbf` | `4ffcb86cd53ed1602456270452462d903f15e174bb6a43a76ff3156532fc3f43` |
| `bundle.sha256` | `2d80996a9c646ab84525a8502b6beda782381842db9fb556f6842def64107341` |

`bundle.sha256` in each table is the SHA-256 of the checksum file itself. The
file's contents independently bind the other three members.

## Exact software commands

The clean verification used these command shapes:

```bash
python3 -m unittest discover -s tests -v
make sim EXP=020_linux_mailbox
make oss EXP=020_linux_mailbox
sha256sum build/oss/020_linux_mailbox/top.rbf
# remove only build/oss/020_linux_mailbox, then:
make oss EXP=020_linux_mailbox
sha256sum build/oss/020_linux_mailbox/top.rbf
QUARTUS_ROOTDIR=/approved/17.0 make oracle EXP=020_linux_mailbox
make compare EXP=020_linux_mailbox
make dev-bundle EXP=020_linux_mailbox BUILD=oss RUN_ID=0123456789abcdef0123456789abcdef
make dev-bundle EXP=020_linux_mailbox BUILD=oracle RUN_ID=fedcba9876543210fedcba9876543210
python3 -m compileall -q scripts tests
```

Dry-run load, preflight, and fault commands used the same Make targets shown in
`docs/linux-mailbox-development.md`, with `FOGCAST_DEV_DRY_RUN=1`, absolute
OpenSSH client paths, TEST-NET host data, and synthetic expected hashes.

## Evidence limits and next safe action

This handoff proves the host software, compiler outputs, and offline transport
plan. It does not prove:

- HPS GPI/GPO behavior on a Cyclone V device;
- the `OSS FPGA OK\n` payload on Linux;
- live FogCast install, preflight, RBF load, reboot, or recovery;
- HDMI, display, input, audio, storage, or other MiSTer behavior.

No target identity, credential, private host path, generated RBF, bundle,
result, or run trace belongs in Git.

The next safe action is to review this documentation commit together with the
matching FogCast software handoff, update the private PR only after explicit
authorization, and then execute the separately approved integration plan. Any
target contact, installation, reboot, or HIL still requires a fresh explicit
operator gate.

Rollback before HIL is simply to discard ignored build outputs or revert the
documentation/transport commits. Rollback after an authorized deployment is
owned by the FogCast install manager and must follow the integration plan's
verified uninstall procedure.
