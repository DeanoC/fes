# Core persistence validation — 2026-09-09

Status: software, diagnostic hardware, exact-image persistence checks and
ordinary-launcher handoff passed.

This milestone stores described-core settings and progress, beginning with
standalone FES Pong. It adds no UI presentation changes. See the
[usage and API guide](../core-persistence.md).

## Selected sources

| Component | Revision |
| --- | --- |
| FES integration | `47bf36e` |
| FogCast | `32cc1add8c2aa4172e2feb351f760ff24bcc6da3` |
| libmister-runtime | `27f03f70a3270fc2dc98fd30c24faef31a2c8ab1` |
| misteross | `4a8b8635cf338b22648e9e94272243b1fce42a79` |
| mister-packages | `bfc4b2bc8232c93d67f88bd452223986768bfe4f` |

## Software and FPGA checks

- Shared definitions, codec records and mailbox fixtures passed their canonical
  tests. Parent consistency checks passed for 12 generated files, nine fixture
  copies and four source pins.
- Runtime native, protocol, archive and pinned ARM target builds passed. A real
  production factory regression covers persistence forwarding through the
  production hardware wrapper, including settings preserving existing progress.
- Full FogCast Go tests, affected race tests and `go vet` passed.
- Parent tests ran 192 tests with 36 skips and no failures; host build passed.
- Pong RTL and mapped simulations passed, including restore/commit/freeze,
  partial and invalid writes, speed limits, saturation and rally events.
  The misteross Python suite ran 552 tests with one skip and no failures.
- Independent component and integration reviews completed. Resolved findings
  cover retained recovery ownership, releasing a newly acquired lease after a
  definite rejected settings update, and production hardware forwarding.

Standalone Pong version 1.1.0 was built from the selected source using the
pinned open-source toolchain. Its reported maximum frequency was 79.051 MHz
against a 74.25 MHz target.

| Artifact | SHA-256 / package ID |
| --- | --- |
| Persistent Pong package | `c8a25e682a22cd97a51302b2115d2a810f48e42673a53709331fc60984fe263f` |
| Persistent Pong RBF | `8097d946ee2c883e44184ef913f259083028fc859da5bbda7f9db36ea0147397` |
| Rebuilt original MiSTer Pong | `fdabafdac7db03b62edb4f4ae3ee17f7351971656a81312d8094ff830f4d05b0` |

The original MiSTer Pong wrapper was rebuilt with Quartus 17.0.2 because it
shares the game module. Existing compiler tools and unchanged console artifacts
were reused for diagnostic integration. The stabilized image passed the
separate required two-pass reproducibility check.

## Diagnostic hardware

The designated kit was operated through its lease system. A private host,
catalog and launcher pairing isolated these tests from the UI team's installed
host. Temporary agent/runtime bind mounts selected the diagnostic binaries;
the ordinary image and services were restored after testing.

Diagnostic image: `e6b2279ad707c8d2a231383d375adf55af604a478ec7aef26b6c752437b95efc`.
This is distinct from final cold-build acceptance.

| Check | Observed result |
| --- | --- |
| Missing record | Persistent mode, normal speed, best rally zero, revision `absent`. |
| Settings update | Fast speed saved; stale revision returned 409, invalid enum returned 400; rejected update left lease free. |
| Real game progress | Paired launcher input started Pong and produced a player return; Stop saved fast speed and best rally one. |
| Relaunch | Restored the saved record; active-core settings changes were refused. |
| Compatible versions | Version 1.1.1 and rollback shared the same record, including direct replacement. |
| Incompatible selection | Changed layout and removal of persistence were rejected without changing active generation or input session. |
| Reboot | A shared-kit reboot preserved the exact record bytes; subsequent launches restored them. |
| Storage failure | An obstructed record path caused `SAVE_FAILED`; the active generation survived, input resumed after repair, and Stop retry succeeded. |
| Corruption | Corrupt record blocked persistent launch while existing legacy Pong and its input session remained active; original record restored afterward. |
| Development load | Reported volatile mode and left the library record unchanged after Stop. |
| SNES regression | Super Mario World launched and stopped; all four pre-existing SRAM files remained byte-identical. |

The storage test obstructed `record.bin`; it was not a physical power-loss or
injected fsync-failure test. Input was sent through the actual paired launcher
stream; no new physical-controller observation is claimed. Diagnostic USB
MJPEG capture had malformed frames, and an uncompressed capture attempt found
the device owned by another process. These captures do not establish visual
acceptance or an FPGA video defect.

## Exact image and final handoff

Both cold build passes produced
`e6b2279ad707c8d2a231383d375adf55af604a478ec7aef26b6c752437b95efc`,
also identical to the diagnostic image. Structural checks and the QEMU packaging
smoke test passed. QEMU does not emulate the FPGA.

The image was installed under a maintenance lease, preserving the previous
`c82c87b6f5e31e00e4f56b9245815b9db77cf9d2c8cc06e829dff88fca8051f5`
image as `fsold4.img`. After reboot, kit identity remained
`73dc9f5f-1a12-4a95-a820-a9b4e600769a`, with new boot ID
`fe04ef7b-0006-4f9c-9dc9-424837dc30b4`.

| Running executable | SHA-256 |
| --- | --- |
| mister-agent | `59781b04b0cf24c33b61fafcc8933565d9b9bc9a68fa70fe5657c4e60bf264c7` |
| mister-runtime | `97a5716ceb9a32a9996fd925f6aaf18755b3833f44109b41bbc75ea06d08a748` |
| fogcast-kit | `f00504dc7d0794faeb5992d5e12c1479cdbf91a06acefae2edf3238d30122c3c` |

The verifier checked running executable hashes, all regular files under
`/usr/share/mister-runtime`, the installed image, launcher pairing, kit identity,
and stable boot/mount state. No diagnostic bind mounts remained. The root
filesystem was read-only on `/dev/loop8`, backed by `/root2/linux/linux.img`.
The loop device itself was writable, as configured by this conventional boot.

The SD image matched the verified bytes exactly. A full comparison of the
mounted loop found exactly two cached journal-header changes: byte 16778283
changed from 0 to 18 (JBD2 64-bit/checksum-v3 flags), and byte 16778320 from 0 to 4
(CRC32C checksum type). Its observed digest was
`e6beae6b478591fb764cc22e14c534bdac56e2f581d2f76d91c7d628048f5841`.
Independent comparison and journal-inode inspection confirmed these fields.
The private verifier allows only this diagnosed transformation of this exact
image; it does not claim the mounted loop is byte-identical or relax SD/file
checks. No image or product code was changed to accommodate this observation.

On the installed image, the previous speed/rally record survived reboot. Settings
changed to slow and back to fast with revision CAS, preserving best rally. Pong
launched in persistent mode, accepted paired launcher input, stopped to idle,
and restored the same record on relaunch. Full boot verification before and
after this sequence confirmed the same boot and files, and the lease was free.
A longer uncompressed capture showed a clean Pong frame; launcher return was
also captured. No new physical-controller observation is claimed.

One initial host launch returned `MISTER_UNAVAILABLE` before any target load
request; the kit remained healthy, idle and free. A fresh health check and launch
succeeded without a restart. The full settings-to-launch sequence then passed.
The transient's cause is unresolved; local code review and ten repeated focused
lease race tests did not establish a settings-release race. No automatic
mutation retry was added, and the failed attempt remains in private evidence.

The current card uses conventional `linux.img` boot. This milestone does not
establish appliance boot-selector automatic fallback; the existing recovery
implementation and its separate validation remain unchanged.

The ordinary launcher pairing was restored. The existing UI-team host binary
was preserved; through that host, legacy Pong launched with ready input and
stopped to idle. Final lease status was free and the ordinary launcher was
captured. The isolated test host was stopped. The verified image and saved Pong
record remain on the kit; the private catalog and evidence remain available for
follow-up. New persistence APIs are in the matched built host; this task did not
replace the UI team's installed host with its own bundle.

Private logs, scripts, package variants and hardware responses are retained
under `.superpowers/sdd/2026-09-09-core-persistence/` in the integration worktree.
Credentials and existing save backups remain outside tracked documentation.
