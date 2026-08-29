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
| Implementation commit | `d1706e5660296adf7addc6be8bb92624a6e8a091` |
| Implementation tree | `4aeba4b0356dde252f236b3079a9502db713c85f` |
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
| Full Python suite | 320 discovered: 319 passed, 1 opt-in fixture skipped |
| Mailbox Verilator simulation | pass |
| OSS build | pass at 50 MHz; achieved 130.82156372070312 MHz |
| OSS determinism | two clean experiment builds produced the same RBF SHA-256 |
| Quartus oracle | pass; exact `17.0.2 Build 602` |
| Semantic comparison | pass; `failures=[]` |
| OSS and oracle development bundles | pass |
| OSS and oracle `dev-load` dry-runs | pass; 13 argv lines each |
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
| OSS `manifest.json` | `382b1cfa0646eb24f87d7c303548dd42c76fe479b0b91628beac100d2e95681b` |
| OSS `build-summary.json` | `8ec7b7f2b619098d0f77ac43757860326eda1c8803c108b5b9fc7dff1bdfb25c` |
| OSS `top.rbf` | `49b82822a9586d8c55200c023598690147bba27a2fa1f93ab047f8b3fba6b800` |
| Oracle `manifest.json` | `21d92c8b0ec633c88428b84c2c09c739ba12682fb682802b5ff9ad5ca8ac9189` |
| Oracle `build-summary.json` | `b1c9c78bfdbc53c86d56d262bc359fee27fdf7f240021decf63aaea95c98a21b` |
| Oracle `top.rbf` | `4ffcb86cd53ed1602456270452462d903f15e174bb6a43a76ff3156532fc3f43` |
| Oracle `top.fit.rpt` | `679600ab9a5bcfedfbcf83e4af16b22a3f5747733a770e1b234ea0750f284a78` |
| Oracle `top.sta.rpt` | `bb5d00e1af16a2d3463750254a031ba79aade1671dbc9d2ffb5b72787a402197` |
| Comparison JSON | `bd8f0b86b1358d4046733e9ed6e7481a18dd471d0a74d67cf7a28beae1fdfc46` |
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
| `manifest.json` | `1086d7b587dfd780d8c21d5c6d56b19609862bd5601747db4a8a5ecd703e97b7` |
| `resource_evidence.json` | `9c4d37103c10187c34483f540056d11e81d71b914f5d5e4b76c5dfcb48e1d1a3` |
| `top.rbf` | `49b82822a9586d8c55200c023598690147bba27a2fa1f93ab047f8b3fba6b800` |
| `bundle.sha256` | `48f148b4af1544b7f6eef1b4ad60ee6de72e0daef9b900b523adb1026ef8e2fe` |

### Oracle fixture

Run ID: `fedcba9876543210fedcba9876543210`

| Member | SHA-256 |
| --- | --- |
| `manifest.json` | `c0c8399fa3f6888555decaf6a179647048ba08f3f194ada352f47b987d7513c0` |
| `resource_evidence.json` | `9638d581f4d04c7378b3dacf984a68f5dfe2d4bbecc7e30b76017997963a4983` |
| `top.rbf` | `4ffcb86cd53ed1602456270452462d903f15e174bb6a43a76ff3156532fc3f43` |
| `bundle.sha256` | `c89c41c5e1e637b1406fd017399d3608a24bda5300e0d63af65818c16dec7de1` |

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
