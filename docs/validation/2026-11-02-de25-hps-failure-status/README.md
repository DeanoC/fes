# DE25 HPS failure-status diagnostic, 2026-11-02

The native transfer still **stopped**, after 20.184137 seconds
on TRWU8CBL. The new retirement path worked and retained every buffer. Its
HPS-client status request reached its polling bound without receiving a reply.
A separate non-programming JTAG management-status query still responded and
reported incomplete configuration. Core-load acceptance remains unavailable.

Native source: `42f2586b24a07dbbf1cfb8fd40dadf07ec7b6e65`. Matched boot: `eff3e21e316e011c`.
The [raw trace](native-console.log), [saved observations](evidence.json) and
[independent retirement audit](retirement.json) preserve the first attempt.

| Observation | Result |
| --- | --- |
| Completed bytes | 786,432 |
| Accepted bytes | 1,048,576 |
| Possible outstanding buffers | 4 |
| Pending bytes held by secure gate | 262,144 |
| Entire source retained | 2,027,520 bytes |
| HPS status request | One FIFO submission, status `-2047`, header 0 |
| HPS response | 0 received/skipped FIFO words after 10,000 polls |
| JTAG management response | Six words; general STATE=IDLE, CONF_DONE=0, INIT_DONE=0 |

The HPS record's zero data words are initialized storage, not an SDM reply.
The JTAG query reports general configuration status; it supplies no HPS
reconfiguration state. The cause of the missing acknowledgements remains open.
The next controlled comparison is larger data blocks, following the
[pinned vendor client's](vendor-reference.json) larger transfers, while
preserving this exact RBF, arena, ownership and 20-second transfer deadline.

## Retirement and ownership

The native probe cancels its caller-side transfer before requesting operation 9
through the generated private owned firmware channel. Firmware checks the
original cold owner/generation, held bridges and supported CPU context, then
permanently retires configuration and bridge authority **before** the status
request. The retirement acknowledgement preserves owner/generation 1 and the
original panic. It grants no completion, drain or buffer-release proof.

The separate reader sends fixed `RECONFIG_STATUS` with HPS client 1 and reserved
job 15. It requires idle local parser/descriptors and an empty command FIFO.
It has six response words, a 64-word FIFO budget and at most 10,000 polling
iterations with 100-microsecond waits. It does not restart/cancel the SDM,
replay data, advance configuration descriptors or open bridges. The host's
separate two-second passive failure tail collects the record without extending
the native transfer deadline. The [command reference](https://docs.altera.com/r/docs/d710186/current/security-technical-reference-manual/reconfig_status-command)
requires the original requesting client for this status operation.

All data paths remained held. No identity probe, active-core launch, return,
DMA reclaim or runtime activation followed this failure. The immutable source
and fixed uncached arena `0x94800000..0x95000000` stay owned until reboot.
One CPU ran; secondary CPU release remains denied. This source-bound chain
is neither signed/measured boot nor production package admission.

## Validation and retained evidence

All 12 [affected software groups](software.json) pass on the exact native
revision, including 1264 parent tests,
1576 FPGA host tests, SD ownership/PIO lifecycle, native
Tenfoot launch/return and identity-failure recovery, firmware/RTL integration
and contract/schema checks. Generated consumers match. The existing mailbox
integration suite runs 144 active parser cases plus 320 retired-status cases
across every response-ring position. Strict target shadowing warnings and
address/undefined-behavior sanitizers apply to the new reader fixture.

The actual secure gate/strong provider tests cover owner, generation, lease,
CPU-context loss, repeated retirement, query timeout and SDM error 2. Go tests
keep failed transfers and their original error unchanged after acknowledged,
failed or uncertain retirement. Private boot/cleanup helpers pass 66 cases;
post-retirement capture checks pass 28 cases. The [linked firmware audit](linked-firmware.json)
verifies eleven strong configuration/digest/status symbols and the unchanged
scalar SHA-384 transform. All six matched components built successfully.

The earlier candidate's compiler warning, parent test-selection failure and
private combined-suite setup failure are retained in the [failed build](failed-build-retention.json)
and [session](failed-session-retention.json) records. The final repair keeps the
compiler warning enabled and all 320 cases in the existing mailbox integration
suite. It changes no test-selection threshold, timing bound or memory permission.

[Ethernet upload and independent SD readback](upload.json) passed, with protected
boot files unchanged. The [native cleanup audit](hardware.json) and later
[JTAG cleanup audit](jtag-audit.json) verify original 15 MHz clock readbacks,
restored USB permissions and a free UART. The spare card and exclusive top-board
claim remain retained under Deano's instruction; its factory card remains safe.
There was one native attempt on this source and no native retry.

The [matched-build archive](build-retention.json) contains 43,681
members, each read back and compared. SHA256 `e469a6edaced8d8f79aa736930078c817b8645ce39930d737b92aeaf0d06758f`.
Its incremental source bundle requires the documented prior shallow retention
chain, including the failed-source predecessor; it is not a standalone full
Git-history backup. The first serial trace and all helper outcomes remain
separate from software/model and production admission evidence.
