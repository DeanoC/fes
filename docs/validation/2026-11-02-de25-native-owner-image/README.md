# DE25 native owner test image

Candidate and diagnostic source: `5d839c7f62df4040b9f5a89d7bb41fa53bc9f08f`.
Headless base: `9803d7edb4a757cc7dbb492e0a0f01948fe090cb`.
Kernel/DTB: `59d3b43997b5827dda55125f0f651248a51e6ebf`.

The existing headless image builder and owner-candidate constructor produce
a separate card image for native kernel and unconfigured-owner refusal tests.
Its normal runtime admission remains **closed**. Managed factory/MENU and
positive DMA/display acceptance remain open.

`fes-de25-owner.img` contains 3,984,588,800 bytes; SHA-256:
`429a787525751594f8989ec96c861b27ef46505672cd831e17aa300133ece056`.
The full [candidate receipt](candidate.json) and
[first-boot expectation](first-boot-expectation.json) are copied verbatim from
the actual constructor output. Original images, kernel payloads, diagnostic
binary, captured tools and complete job logs are retained off-board.

## Completed checks

The fresh normal headless build passes in 716.5 s. Its `rootfs.tar` is
byte-identical to the earlier stable-MAC base from `b97a211878c2d76e6499cfe0f45ece3a34bfc5af`.
The new ext4 filesystem and SDK archive have their own hashes; no byte identity
is claimed for them. The current recipe receipt binds every exported artifact.

The owner constructor passes in 23.6 s. Its two separate assembly passes produce
identical image, rootfs, archive and boot-prefix hashes. The matching opt-in
kernel/DTB replace the vendor boot payloads through the existing constructor;
vendor boot geometry, scripts and rollback dependencies remain authenticated.
The independent public verifier passes in 5.4 s using the pinned unprivileged
media-tools image and read-only candidate/source mounts.

The native refusal diagnostic now queries owner ABI3 before its control calls,
rejecting an old or unreadable protocol. It builds as a static AArch64 ELF in
5.2 s using SDK SHA-256
`7912076c08d93c8d8c28c0ef81bd406abf6b1acb9e01ccac37d44307e39759a7`
and the locked builder image. This uses the kernel's locked SDK rather than
the newly exported headless SDK. The binary is 835,312
bytes, SHA-256 `deecc4693f2046bec5b1e33d79bc278ff41a31e66b6f1624618ece1896c8e672`.
Cross-compilation and ELF inspection provide no native execution evidence.

The actual affected software plan passes in 325.9 s: 1,394 parent cases with
45 skips, 62 planner cases and the separate generated consistency check
(40 consumers, 42 fixture copies, zero copied pins). This includes the actual
diagnostic's host compilation and refusal before opening a device on an
ineligible host. [Validation](validation.json) records source, tool and log hashes.
An initial early launch was rejected while the source snapshot was still
checking out; no child test or compiler ran until the committed snapshot was
complete and clean. The failure record is retained.

## Native acceptance still required

Before running the diagnostic, authenticate the candidate's actual boot,
kernel notes/configuration, board/rootfs, owner device and observed boot UUID
against the first-boot expectation. Keep the board exclusively claimed and
unconfigured. The diagnostic holds the existing maintenance lock, checks ABI3,
verifies rejected control/allocation calls, drops the child process's
capabilities and checks refusal again. It publishes no frame and maps no MMIO.

Native boot, Ethernet/SSH, suppliers and strict SID13 translation, these actual
kernel syscalls, and ordinary reboot/recovery remain untested for this image.
The new kernel/Image/DTB and diagnostic are uninstalled. Existing claims,
baselines and the older ABI2 native-health record are unchanged. No kit, card,
FPGA, QSPI or power operation occurs during preparation. Firmware pairing and
the full [normal-kit scope](../../design/de25-linux-platform.md) remain open.
