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

## Physical result

The single bounded attempt passed in 129.9 seconds. The exact
top adapter returned after power loss, the consumed loader booted, and fresh
card/prefix/FIT/manifest/protected-file/original-rollback checks passed before
the repair writes. All three real UART transfers and all five repair writes
received independent readback verification. The original R1107A pair remained
exact and fresh R1107B copies preserved the consumed UART0 diagnostic.

The corrected loader then booted. Two identical 1,659,392-byte
package uploads each completed all 34 ordered return observations, command
nonce/prompt success, RAM hash, SD write/readback and fresh SD read/hash. The
actual console contains 68 markers. Independent final reads verified R1107A,
R1107B and the protected 8,198,704-byte experimental FIT. The recovery boot
files/manifest and protected prefix passed the upload controller's checks.

Installed FIT: `ccb6a597052c69f982713c327eeaab02adda4bd8ef21905deed2db7e7d47f353`.
Installed manifest: `b63eb77506898bc53f6082e8575fe6387ea2525425754aced31ca7ccbff62fcf`.
Installed prefix: `b8ea5c763ffc9d6ccca750aa476cc0739cf896af995959437fa41f65183a039f`.
These are the new last-verified card identities. The earlier v1 repair operator
must not be replayed against this updated card.

Independent cleanup confirms recorded 15 MHz readback, exact USB permissions,
the owned private server gone and UART free. TRWU8CBL remains claimed at its
verified recovery prompt. Factory restoration was waived by the user; its
original card remains safe aside, and the bottom kit was untouched.

This is exact-artifact UART repair and two repeated recovery-upload evidence.
It does not prove idle XGMAC DMA, identify the original intermittent stall, or
establish native networking, core activation, menu/display/input/media behavior
or timing signoff. Next is refreshing the ordinary-package native chain,
uploading its complete launch inputs through this corrected recovery path and
running a newly frozen native launch/return test. Full DE10-style readiness
remains open.

Full receipts and traces are bound by `result.json` and the retained archive.
