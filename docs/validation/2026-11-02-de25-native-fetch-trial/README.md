# DE25 native fetch trial: upload stops before core execution

Two bounded sequences on the exclusive top TRWU8CBL stop before writing the new
core package or programming the matched native candidate. The changed firmware
FIT uploads and reads back successfully in both sequences. The first sequence
stops at Ethernet PHY startup in its second recovery boot. The second sequence
uses one recovery session, then stops at the existing scratch-memory protection
gate before its second transfer. Neither result is a native boot, new FPGA
configuration or physical fetch observation. The
[matched candidate and software checks](../2026-11-02-de25-native-fetch-observer/README.md)
remain valid off-board evidence; the
[earlier physical command cycle](../2026-11-02-de25-native-computer-control/README.md)
remains the native hardware baseline.

Ordinary runtime package admission, repeated core loads, Tenfoot launch/return,
USB input and video acceptance remain open. The full core-load goal stays active.
Ownership remains `lane:deano`; there is no PR handoff or publication.

## Selected artifacts and preparation

Both sequences use checkout `535bda0d9d2909a4aaff837ce7d69d2cb2c4d67f`, unchanged
native producer `726a16f4e2d979e68fb67e85e97c916e3e94ee4f` and FPGA producer
`c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`. The intended package is
`b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37`, FPGA build
`dda236646bb868e63c72bd0e777f3dd6`, with 1,646,592 core bytes. The 1,649,664-byte
package file has SHA256
`75a2cdfb38f9e61f41ebe2554ca30de6e8e367246d294d3d8f00c514dbf47b73`.
Matched native boot ID is `829865a45111af8d`; the 6,606,660-byte FIT has SHA256
`412ded5471e741cbb43d2a7ccc86a0dfde2ba158364e0ba15bf7b5951faff264`.
The package and native candidate are not loaded by these stopped sequences.

The [first preflight](first-preflight.json) and
[single-session preflight](batch-preflight.json) recheck exact selected source,
all 11 passing software scopes, all 1,289 selected parent IDs, the sealed RTL
model, actual linked startup/firmware proof, candidate identities and recovery
inputs. Each also checks the live claim, exact USB node/permissions and a free
UART. Off-board checks cover
[66 first-run orchestration cases](first-controller-fixtures.json),
[65 single-session cases](batch-controller-fixtures.json), 16 actual FIT
read/hash cases and seven helper/parser/cleanup checks. A missing second recovery
argument label was found and corrected during off-board preparation; its
superseded preflight is preserved in the first archive. No device or attempt
fuse was opened by that superseded preflight.

The private JTAG service filters only TRWU8CBL. Programming uses 6 MHz and exact
device ID `4362C0DD`. Prompt, native storage, observer and actual configuration
bounds remain 50, 100, 45 and 20 seconds respectively. No retry, timeout
relaxation or admission bypass is added.

## First sequence: PHY startup timeout

The [first sequence](first-experiment.json) runs from 16:51:08 to 16:53:28 UTC.
Its [first recovery](first-recovery-boot.json) boots the qualified frozen loader;
the [firmware upload](first-firmware-upload.json) passes download hashing, one
fixed `FES/UPLOAD.BIN` write, independent SD readback/byte comparison, and
protected prefix/root recovery FIT/manifest checks.

The [second recovery](first-recovery-package.json) also boots. The
[package upload receipt](first-package-upload.json) and
[raw console](first-package-console.log) show the expected card and PHY identity,
followed by status samples `7969`, `796D`, `796D`. The subsequent `dhcp` command
reports PHY autonegotiation timeout and `phy_startup() failed: -110`. It returns
failure at a parked loader prompt. No package download or SD write occurs, and
there is no native attempt. Positive pre-command link samples do not establish
that later driver startup will succeed. The underlying PHY timeout cause is
unproved.

The [independent stop audit](first-stop-evidence.json) passes with its
[bounded receipt](first-stop-audit.json). The actual failed output remains
failed; successful cleanup does not qualify the intended core test.

## Single-session sequence: live RAM reservations

After preserving that stop, a separately fused sequence uses one fresh recovery
for both fixed uploads. The [primary vendor source review](transport-source-review.json)
binds the exact qualified U-Boot revision and three retained driver files.
Its Ethernet driver connects/configures the PHY only when no cached PHY exists.
Keeping one session avoids a second recovery initialization; this is a transport
experiment, not proof of the first timeout's cause.

The [single-session sequence](batch-experiment.json) runs from 17:01:58 to
17:03:24 UTC. Its [recovery](batch-recovery.json) and
[firmware upload/readback](batch-firmware-upload.json) pass. The
[second upload](batch-package-upload.json) issues only `version` and `bdinfo`.
The [raw console](batch-package-console.log) reports these live U-Boot LMB
reservations in the uploader's scratch window:

| Inclusive range | Bytes | Flags |
| --- | ---: | --- |
| `82000000..8264cf43` | 6,606,660 | `none` |
| `82800000..82e4cf43` | 6,606,660 | `none` |
| `83000000..830e87f7` | 952,312 | `none` |

The first two match the completed FIT download and readback buffers. The third
matches the protected root recovery FIT load. The existing uploader requires
its scratch window to contain no reservations, so it rejects this live map
before network or SD operations for the package. It has no contract for reusing
reservations owned by its own earlier successful transfer. Flags `none` alone
are insufficient evidence of ownership. The reserved low region and relocated
loader region also remain visible; neither is changed.

The [independent final audit](batch-stop-evidence.json) and
[bounded receipt](batch-stop-audit.json) confirm that stop boundary. This is the
second failed hardware sequence on the same implementation. Further HIL is
halted until an actual uploader ownership fix and focused off-board validation;
a documentation-only commit does not reset that limit. The next implementation
must reproduce these real reservation side effects in its model, track the
session and buffer ownership explicitly, and reject foreign, protected or
unexplained reservations. The overlap gate must not simply be removed.

## Board state and retention

Both independent audits confirm original 15 MHz readback before stopping only
the owned private service, exact USB permission restoration, a free UART and
the continuing owned claim. The board is parked in the qualified recovery
loader. Its retained spare contains the new verified `FES/UPLOAD.BIN`; this work
does not write `FES/PKGTEST.BIN`. Neither sequence issues `fesboot` or native SD
writes. The factory card remains safe, and the user permits leaving the spare
and diagnostic installed without a factory restore. No bottom-kit operation
occurs.

The [first archive check](first-stop-retention.json) independently reads all
71 regular members of the 198,258-byte archive, SHA256
`ce74d6f718691890ffc84c20a4284f9965880516c60c1c98d183fd4e193f745a`.
The [second archive check](batch-stop-retention.json) independently reads all
65 regular members of the 146,045-byte archive, SHA256
`b16e32d031e69bcff98fb6b45badd867cdf1e55d5c610283887a8a9ff9589ca1`.
Both are under `/home/deano/fes-retained/de25-native-fetch-trial-20261102`,
with raw traces, attempt/preflight fuses, private runner sources, fault checks,
cleanup audits and the exact primary driver sources used for the transport
review. Their manifests reference the separately verified complete candidate
archive and source bundle. [SHA256SUMS](SHA256SUMS) covers the copied records
in this directory. No FPGA/internal timing or native execution claim is added.
