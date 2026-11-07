# Bounded serial recovery timer correction

This newly frozen operator follows the [exact-image repair checks](../2026-11-07-de25-recovery-timer-repair/README.md).
One fresh attempt bootstraps the exact currently installed UART1 image solely
for UART repair, verifies the spare partition/prefix, protected native/package
files and both R1107A/R1107B rollback pairs, and saves fresh R1107C backups.
Five fixed writes and independent readbacks update the FAT FIT, raw mirror and
manifest. A separately accepted complete repair receipt is required before
booting the corrected 100-ms recovery image. It supplies no Ethernet download
or native/core activation, and cannot replay an old consumed operator.

The actual wrapper/controller passes 12 modeled cases using actual bytes and
18 receipt rejections; both rollback generations must be preserved. Source,
clock/device and 12 master-lifecycle cases pass. The six listed qualified
transport/clock/boot/log/receipt helpers and seven master control/cleanup
functions remain byte-identical. Boot/programmer/UART operations are modeled
in these fixtures; these checks grant no physical acceptance.

Existing prompt/boot/UART/control/repair/outer/host bounds remain
50/240/150/5/330/740/850 seconds, with one MiB console per phase. Shared JTAG
locking and the continuing TRWU8CBL claim are required. Each boot requires
the exact top adapter/device at 6 MHz; exit restores 15 MHz, exact USB
ownership/permissions, stops only the owned private JTAG server and closes
UART. First failure is terminal, with no replay or automatic recovery.

The latest post-power observation established only a matched serial version,
nonce and prompt. Fresh boot/card checks remain mandatory before any write.
The factory card stays safe aside; factory restoration remains waived, and
the bottom kit remains untouched. The full native upload/launch/return test,
general core-loading acceptance, display/input/media/native network behavior
and four FPGA signoff findings remain open. This freeze is preparation.

Raw operator/evidence: `out/nano-recovery-timer-board-20261107`.

## Physical result

The single attempt passed in 113.1 seconds. A fresh installed-image
recovery boot passed, followed by live spare partition/prefix, protected FIT/
package and both rollback-pair checks before any writes. All three real UART
transfers and five fixed writes/readbacks passed. R1107A and R1107B remained
exact before and after the update; fresh R1107C preserved the UART1 predecessor.
The corrected 100-ms recovery image then booted to its expected serial prompt.

Last verified installed FIT: `3722e24c551a794dd970e665cc4932b96cb62aefa353f39079285458d67d48e5`.
Manifest: `95aca2b3116755247a122a9a4357c1efbcfb106adb4dfa5fe94e489cc1889664`.
Protected prefix: `80e36789a8de046c8e47899bbb6567d5afa58e3325c10d3944c7151a210d595c`.
The consumed repair and older upload operators must never be replayed.

Independent cleanup confirms recorded 15 MHz readback, exact USB permissions,
the owned private server gone and UART free. The continuing top-kit claim is
held at the verified recovery prompt. The factory card remains safe aside,
and the bottom kit is untouched.

This proves this serial update and corrected recovery boot. It does not prove
the Ethernet return/drain correction, idle DMA, native boot, core activation
or full DE10-style readiness. Next is the fresh complete eight-file upload and
native launch/return test. Its cached native artifacts remain unchanged; their
source admission must explicitly account for the recovery-only changes.
