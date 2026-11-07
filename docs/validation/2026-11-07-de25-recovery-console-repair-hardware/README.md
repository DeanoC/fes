# Bounded console repair and network return test

The frozen operator follows the [exact UART repair software checks](../2026-11-07-de25-recovery-console-repair/README.md).
Its prior UART0 diagnostic attempt stays terminal. One new attempt is permitted
only after source, continuing claim, exact adapter, complete UART-owner and live
card/rollback checks. This freeze records preparation, not a hardware result.

It bootstraps the exact consumed image solely for UART repair, preserves the
original R1107A rollback pair, saves fresh R1107B copies and verifies five repair
writes/readbacks. Only an independently accepted repair receipt can permit
the corrected loader boot. Two identical current-package uploads then require
34 ordered return markers each, normal command nonce/prompt success, SD write/
readback and fresh reads. Both rollback generations and the protected
experimental FIT receive independent final read/hash checks. No native boot,
core activation, flash write or factory-card operation is supplied.

The original prompt/boot/UART/control/repair/network/outer/host bounds remain
50/240/150/5/330/300/740/850 seconds, with one MiB console per phase. Shared JTAG
ownership and the continuing top-kit claim are mandatory. The qualified private
transport, clock policy and master clock/cleanup functions remain byte-identical.
Programming requires the exact top adapter/device and 6 MHz; exit must restore
15 MHz, exact USB ownership/permissions, stop only the owned private server and
close UART/TFTP resources. First failure is terminal; no replay or fallback.

The actual bootstrap/controller wrapper passes ten modeled cases and sixteen
receipt rejections. Clock/device/source/lifecycle fixtures pass. The actual
network wrapper, upload session and trace decoder pass 37 modeled cases and
sixteen receipt rejections using actual artifact bytes. Boot nonce/device I/O
remain modeled in those fixtures. These checks grant no physical acceptance.

The board's state after the reported power loss remains unverified until the
fresh live gates complete. The factory card is kept aside and the bottom kit
is untouched. Full native core loading, launcher return, HDMI/input/media/
network acceptance and timing remain open.

Raw operator/evidence: `out/nano-recovery-console-repair-board-20261107/`.
