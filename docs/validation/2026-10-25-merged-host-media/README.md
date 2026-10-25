# Merged host media browser and filename diagnostic

The combined path from merged PRs #1041 and #1045 passes on Kit A at main
`0f3b35c95249aef16cf1403ab91e0b2939220af6`. This change records evidence only;
software, runtime, FPGA and shared contracts are unchanged.

## Exact inputs

- Built and installed userspace bundle:
  `2042b540a43fed43ec0a70b8a3dbff255245e9f2b70b39a58a5be52c926fbaf3`,
  observed selected and active at software generation 446.
- Exact installed ARM launcher SHA-256:
  `e02f16dd490a1c9e8edd10eab632e6c45932a53d12eba43d2c8b5d4589d0503c`.
- Running baseline OS image:
  `4db7486452057d8a77479125c8ee8e9a27db4f378a9d9735900e90f6f3fe7a26`.
  A retained matching image is hash-verified before building the bundle.
  No image or boot-configuration change is needed for this host SMB path.
- Existing sealed C64 package:
  `713b811056616b153683a5f1c7e924c290bd6cdc0008c3cf69d7c27448f5af93`.
  The package is not rebuilt or promoted. Its linked firmware is the repository's
  open diagnostic with vacant cartridge sockets, not a Commodore ROM.
- Private current-main host in a container with a verified empty catalog;
  native host SHA-256
  `e52b68f252654a4dc2567ca45b2b06b5ed107ce5994c46d71dce5e9774f37a81`.
  No shared host or home catalog is used.
- Read-only Samba fixture on loopback, with all service state private to the
  task. A remembered **Merged media fixture** host drive starts in `Media`.
  It contains two distinct synthetic 174,848-byte D64 files, a wrong-system
  `.tap`, and an undersized `.d64`. There are no household shares or game bytes.

The complete build/toolchain provenance is in
[software-receipt.json](software-receipt.json). Independent source, media and
session identities are in [evidence.json](evidence.json).

## Observed HDMI and media behavior

The diagnostic uses an isolated instance of the exact installed launcher,
its paired host listener and a synthetic evdev keyboard. Through the actual
HDMI selector, without entering a kit-local media path:

1. Open **FogCast host · Merged media fixture**. The folder shows the two
   valid D64 files; the wrong-system tape and undersized disk are filtered out.
   Browsing and cancelling leave the active session and empty unit unchanged.
2. Select `01-first.d64`; the unit reports ready and the confirmed filename
   appears after reopening the selector.
3. Browse the host drive again and select `02-second.d64`; the filename changes
   to that disk and survives another close/reopen.
4. Eject and reopen; HDMI shows **No media inserted**, with the observed unit
   empty and no stale filename.

Both imported objects match their exact source SHA-256 hashes. The same target,
host session, game, package and core generation 1 are retained at every step.
No relaunch is requested or observed during browsing, insert, swap or eject.
Actual HDMI captures: [compatible host files](host-files.png),
[second disk after reopening](second-inserted.png), [after eject](empty.png).

Evidence class: **exact-artifact hardware diagnostic** for the host SMB picker,
filename and C64 media-unit path. The open C64 diagnostic is not completed by
this UI test. This does not establish C64 fidelity, disk-program execution,
physical USB-input qualification, every tape/disk core, local USB discovery,
NTFS support or SMB credential/reconnection behavior. Those are separate paths.

## Checks and restoration

The exact software source already has successful
[FES integration CI](https://github.com/DeanoC/fes/actions/runs/40154944142).
Its unchanged software suites are reused. This run verifies the bundle build,
real private-host SMB enumeration and byte reads, the merged HDMI path above,
and documentation/consistency checks for this evidence change.

The raw fixture scripts, logs and private observations remain under
`out/dev/merged-media-hil/` in the task worktree. Kit A is restored and released: the designated baseline bundle `35d58489` is
selected and active at software generation 447, the boot image and boot ID are
unchanged, and idle RBF and launcher configuration hashes match their initial
readings. Runtime idle and free lease are verified. Own launcher/input helpers,
SSH tunnel, host container and Samba server are stopped; the kit fixture directory
is removed and the private SMB port is closed.
Kit B is not contacted; its status file says to wait for Deano's power cycle.
