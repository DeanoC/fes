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
