# DE25 native frame return and fresh SD retention

One bounded TRWU8CBL boot now returns from twelve changing full-frame FPGA
reads to the SD card, freshly reads and retains the same package, and contains
the SD controller again. The frame arena stays pinned. This is a physical
diagnostic of device handoff; normal UI-driven core loading remains open.

Native, entry and secure firmware source is `d0241f1c6510faf1fb2ff49e1dd39dd8cbe32cb3`. The FPGA
producer is unchanged at `df234b524d0e1340b343df411c841bd1dc553817`. The raw native ELF is
14,934,191 bytes, entry 7,307,264 bytes and FIT
7,852,024 bytes, below the unchanged 8 MiB FIT limit.

The opt-in profile binds SD resumption to the original stored-package handoff,
its owner and exact verified window return. The secure provider independently
requires completed configuration, retired reader identity, full master stop,
matching reader request/acknowledgement and all bridges reset. No caller-supplied
policy replaces that proof. Default firmware continues to reject a display
return for SD resumption. No second FPGA configuration is enabled by this test.

The twelve changing 3,686,400-byte frames have complete matching FPGA CRCs and
local drains. Final secure reader/request/ack generation is 13, acknowledgement
cookie 13, status 4 and pending 0. Master stop is generation 6; bridge closure is
generation 7 with reset 0xf, hardware drain mask 8 and quiescent producers.
SD resume is generation 9, and the fresh package containment is generation 10,
both tied to the original owner and return generation 7. Optional fabric-warning
acknowledgement remains unqualified.

| Observed phase | Seconds | Bound |
| --- | ---: | ---: |
| Initial SD package read/retention | 57.350236 | 100 |
| FPGA transfer, frames and owned return | 44.051242 | 45 |
| Fresh SD read/retention and containment | 55.788239 | 100 |

The existing 45-second observer is preserved. The new SD phase has its own
100-second bound; late results fail even when a complete trace arrives. Native
execution remains bounded at 240 seconds and the parent at 800 seconds. The
native process cap is 400 seconds to include off-board source verification and
the added SD operation; this does not extend either existing board phase.
All sixteen startup checkpoints pass. This source has one physical attempt,
zero failures and zero retries; seven prior failed procedures remain separately
retained.

All eleven affected repository commands pass. Fresh checks cover 26 SD return
cases, 19 opt-in actual C/RTL cases plus default refusal, 21 Go store scenarios,
14 entry QEMU guests and full model launch/return/failures. Linked native and
EL3 code confirms the selected private handoff, fresh Store.Retain/Contain and
strong SD callback. Nineteen serial-arrival cases cover the actual decoder and
phase deadlines. Controller and upload failures stop further programming.
The unchanged FPGA and generated-interconnect evidence is verified separately.

[Raw serial capture](native-console.log), [startup](startup.json),
[independent replay](replay.json) and [result](result.json) retain the evidence.
Replay rejects missing frames, closure, SD handoff records and damaged receipts.
Host audit verifies original 15M JTAG clock, exact USB permissions, stopped
private JTAG and a free UART. The exclusive claim and spare card remain held;
the factory card is safe outside and no factory restoration was requested.

Full Tenfoot UI launch/return, ordinary package/ROM/parts/media admission,
reconfiguration after this active-DDR session, physical HDMI, USB hub input and
native Ethernet remain open. The full DE10-like core-loading goal is active.

Retained archive `/home/deano/fes-retained/de25-window-return-20261104/matched-native-chain.tar.gz` has SHA-256
`42f73b65aa76313d21d2f5aaecfa3b490c1c69ffb0edc072dfd38a382103f824`. Its 46,294 members
were read back against the manifest. The standalone source bundle restores
without alternates and passes strict fsck. Expanded files remain available.
