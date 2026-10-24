# Remembered USB and SMB BIOS/media sources

Scope: FogCast storage, host API/client and browser/tenfoot drive settings and
pickers. The FES appliance includes `lsblk` and `ntfs-3g`. Runtime, FPGA source
and shared protocol/schema definitions are unchanged.

Base: `527cf83339735aab7ffb59e11f966d7e44460e0b`.
Host and final managed software bundle source: `4b20a088cb9b5bf55a1f3ffe479dc5c40877218b`.
Native image build selects `a7295164daae31c94dc3ce244e48317d462dd4dc`.
The first candidate (`99c998ac`) passed structural/QEMU checks but was rejected
on inventory inspection: the pinned Buildroot supplies `lsblk` through
`BR2_PACKAGE_UTIL_LINUX_BINARIES`, rather than a standalone LSBLK symbol.
The corrected image selects that set. Its verifier also requires both
`lsblk` and `ntfs-3g` to exist and be executable. The unused first image is
not a deployment candidate.

## Completed host checks

- Focused Go tests: `storage`, `hostclient`, `internal/hostapi`,
  `cmd/fogcast-api`, `ui/tenfoot` pass.
- The final full race suites pass: `go test -race ./storage ./internal/hostapi
  ./ui/tenfoot` (manager, real API/client, remote BIOS picker, paired-kit settings
  and existing regressions).
- Disposable Samba fixture on the development host: guest and NTLM password
  login, wrong-password rejection and exact 14-byte cassette read pass.
  All files and credentials in this fixture are synthetic. No household
  share or kit is involved.
- Persistent drive identity and selected BIOS/media folders survive reopening
  the store. Disconnected USB fixtures remain saved, then read correctly after
  reconnecting. Traversal, escaping symlinks and nonregular files are rejected.
- Real host API/client integration exercises discovery, save, directory browse,
  bounded file streaming, password redaction and forget.
- The paired launcher test can open drive settings without receiving private
  host library settings. The remote BIOS picker imports an exact 8192-byte file
  and explicitly selects the household BIOS through the existing API.
- `make test-changed TEST_CHANGED_ARGS='--base 527cf833'` passes all selected
  lanes: planner/whitespace checks, 1,078 parent regressions, generated consumer
  consistency, Go tests and concurrency-focused race checks, and host UI tests.
  Browser-only follow-ups also pass the dedicated Chromium suite.
- `make check` passes: 21 generated consumers, 37 fixture copies.
- `make deadcode` passes.
- Editing a connection to another already saved share is rejected without
  changing either drive or its credentials; the regression and the API/picker
  paths pass under the race detector.
- After the packaging correction, the native-image package-only shell suite,
  verifier-cleanup suite and five toolchain-cache tests pass.
- `make host` passes. The generated host receipt records:
  - `fogcast`: `a9934539339a0756452f3a6af45598768d0a604964e19044d263b0ea36091e37`
  - `fogcast-api`: `b26d435924713d9801118790e54c460c2bc0124d658aaca32b1306831a2df2c4`
- Chromium runs the assembled production UI: the existing 76 checks pass, and
  the extended core-library suite passes all 10 checks including saved host
  folder browsing, password masking, dialog visibility and reopening.

[Host drive settings screenshot](host-settings.png) shows the actual Chromium
render with synthetic share details. It is host UI evidence, not a kit capture.

## Built artifacts

- Corrected base image: `a7295164daae31c94dc3ce244e48317d462dd4dc`, SHA-256
  `eca3f1b03ef5e412e8ff43beee14674ffaca4872b272e2c33c03c3bff4887277`.
  `make build` and `make verify` pass with one image pass. Structural checks
  and the vexpress-a9 QEMU packaging smoke pass; two-pass reproducibility is
  not run. Inventory and ext4 inspection confirm executable `usr/bin/lsblk`
  and `usr/bin/ntfs-3g`.
- Final managed software bundle: source `4b20a088cb9b5bf55a1f3ffe479dc5c40877218b`,
  ID `4c99f6eb8c602dc760f8ff6f28e7cd832a28def29ad8cc928ffe06c3e4745663`,
  archive SHA-256 `eec0a3c7bd955996a7b346ab1dadbc252e2f353a757ef60e41cb80108580ea07`.
  It is built against the corrected base and contains the final duplicate-drive
  edit protection. The producer records `deployed:false`.
- Linux host source: `4b20a088cb9b5bf55a1f3ffe479dc5c40877218b`, with hashes above.

All three artifacts are required for the complete acceptance path. The base
image's factory software predates the final drive-edit fix; select the final
managed bundle when testing. Matching host and kit Git revisions are not a
compatibility requirement.

Artifacts are retained in this task worktree under
`out/native-integration-dev/`, `out/kit-storage-image-a7295164/` and
`out/kit-storage-bundle-4b20a088/`. Powerboat originals are under
`~/tmp/fes-kit-storage-7c4b5abc/out/native-integration-dev/` and
`~/tmp/fes-kit-storage-bundle-4b20a088/`. The source/image/bundle receipts are
copied beside this record. Build logs are `~/tmp/fes-kit-storage-build-final.log`,
`~/tmp/fes-kit-storage-verify-final.log` and `~/tmp/fes-kit-storage-bundle.log`.

## Image and hardware acceptance

The ordinary source image build on the CUDA development machine stops before
assembly because the production MENU producer needs its authenticated HIP
compiler. The private Powerboat build uses the unchanged production recipe and
normal sealed package cache. It does not alter Powerboat's harness, a shared
host session, a kit image, or a bundle selection.

Physical kit acceptance is outstanding. No kit has been claimed or changed. The disposable Samba container/image and
headless Chromium processes created for these checks were removed or closed.
The remaining check is:

1. Claim an available test kit in its status file and record the exact image,
   software bundle and private host artifacts. An image change requires Deano's
   explicit approval; install acceptable software before changing the image.
2. On the kit and separate host, discover a USB drive and configure a disposable
   SMB share. Choose BIOS/media folders through Settings, browse/import a BIOS
   and appropriate tape/disk, then restart the launcher and verify the saved
   locations. Replug USB and interrupt/reconnect SMB to verify reconnection.
3. Insert/eject media during a supported running session and verify the same
   captured session/package generation and visible machine operation.
4. Restore the exact baseline image and bundle named in the status file, verify
   active bundle, idle state and free lease, then log and release the kit.

A successful host import does not establish FPGA/media compatibility. Evidence
class for this change remains **host-only check** until the exact deployed
artifact completes that kit path. Review is required before merge.
