# Bounded UART update and recovery-return trace operator

The [trace-aware package and controller](../2026-11-07-de25-recovery-trace-upgrade/README.md)
now have an independent complete operator for one TRWU8CBL attempt. It first
boots the unchanged qualified recovery, then verifies the live spare and uses
UART to save fresh rollback copies and update/read back the root FIT, raw mirror
and manifest. Only a fully accepted update can proceed to a fresh recovery boot
and eight network uploads with eight independent fresh readbacks.

The traced image reuses the exact qualified SOF/SPL/BL31. The native boot chain
is not programmed or executed here. The earlier native FIT is used solely as
opaque test data and as the known existing UART reservation file; it is not
admitted for execution. The consumed earlier operator is untouched.

Host control source: `d0abab607ee1db69fdbe88c63789894adede288b`.
Compiled trace source: `dfd4863c02f571c0875cd5c30b5163d9f58a6eef`.
Trace FIT: `0f15b11576e91a6cb705de1f1b97e0960a03437b1821628abbd74bd4367f9855`.

The first failure stops the phase sequence without retry. The UART controller
checks current package/experimental FIT bytes before any write, keeps the same
owned memory ledger and preserves rollback files. Every TFTP must pass the base
fresh result/prompt checks and all 34 ordered return markers before its SD write.
The marked return still proves neither DMA idle nor general network reliability.

The bounds remain prompt 50 s, each boot 240 s, UART transfer/control 150/5 s,
upgrade 330 s, uploads 300 s, whole experiment 740 s and host 850 s. Console
evidence is limited to 1 MiB per phase. The exact top adapter/device, continuing
claim, free UART, source/receipt hashes and shared Quartus lock are required.
Programming uses 6 MHz; cleanup must restore 15 MHz, exact USB permissions,
owned private JTAG shutdown and UART release. The original factory card and
bottom kit are excluded; the continuing claim is retained.

Five off-board fixture groups pass: actual UART bootstrap/controller over the
real planned bytes (10 cases), update-receipt rejection (16 cases), actual master
try/finally lifecycle (14 cases), source admission (13 rejections), qualified
clock/one-device admission cases, and the actual eight-upload wrapper/session
over a stateful card/memory model (86 cases). The public trace console's marker
and retirement gates are covered by the preceding 1,600-test parent run. The
fixture console models UART transport; no hardware result is inferred.

[operator-source.json](operator-source.json) freezes the exact helpers and
fixture receipts. Raw inputs/operator evidence are under
`out/nano-recovery-return-board-20261107/`. At publication, no device was opened
for this operator. The last recorded board prompt remains unconfirmed; fresh
recovery/card qualification is required before the update. Physical core-load,
HDMI/menu, input/media/native networking and timing acceptance remain open.

## Physical result: UART update passed; trace invalid

The single attempt used operator publication
`18668a2aa3d871054cd2b961d40f12d9d8cfe81a`. Fresh old recovery boot, all three
UART transfers and all five update/readback checks passed. The new FIT,
manifest and prefix were independently verified; both new rollback copies and
the protected package/experimental FIT remained correct. The traced loader then
reached its expected prompt after volatile programming of the unchanged SOF.

The first network transfer reported completion but returned neither a command
result nor a prompt before its unchanged deadline. No network-delivered SD
write occurred. Cleanup independently passed 15 MHz clock readback, exact USB
permissions, private-server shutdown and free UART. The prompt is unconfirmed;
the attempt is consumed and was not retried.

The diagnostic has a confirmed source defect: its helper reads/writes UART0
at `0x10c02014`/`0x10c02000`. The actual compiled device tree selects
`serial0:115200n8`, whose `serial0` alias resolves to UART1,
`/soc/serial1@10c02100`, enabled with four-byte accesses and register shift two.
The console addresses are therefore `0x10c02114`/`0x10c02100`.
The host models and instruction audit checked the same incorrect helper
constants; they did not independently check this board's selected console.
That validation gap invalidates this trace. It does not prove an exact stopped
PC or explain the earlier uninstrumented network stall.

The spare now contains the verified v1 trace FIT
`0f15b11576e91a6cb705de1f1b97e0960a03437b1821628abbd74bd4367f9855`,
its manifest, prefix `371cd116055bc7f458dc8254071f62768dcf7bd3c2498e70e8c3e6e04f62a956`
and `R1107A` rollback copies. A future repair must explicitly bind this current
state; the original-baseline upgrade cannot simply be replayed. The original
factory card and bottom kit remain untouched, and the claim is held.

Next: correct the helper's port, check both compiled U-Boot and FIT device trees
against the selected board console before publishing a trace build, and prepare
one explicit v1-to-corrected UART repair with fresh rollback destinations.
No native boot or core load occurred. See [result.json](result.json) and
`out/nano-recovery-return-board-20261107/physical-result.json` for exact evidence.
