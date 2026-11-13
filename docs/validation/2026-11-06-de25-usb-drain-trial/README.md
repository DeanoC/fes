# One bounded USB drain snapshot trial

This operator selects the freshly built native app/entry and matched secure
firmware at implementation `17b41b172` under
`out/nano-usb-drain-snapshot-20261106`. The only compiled native source changes
are the polling error construction and its added snapshot helper. All other
2,097 combined app/entry input hashes match. Current source snapshots, native
ELF/entry bytes, both loader/FIT reservations and all producer inputs are bound.
No historical ELF is substituted for this native build.

The previous consumed matched chain completed native startup and one real
ZX81 ROM launch, then failed USB Pause before normal Return. The backend
emergency-parked the exact running core without resuming SD/input. That trace,
its failed channel-halt wait and independent cleanup remain retained. The
campaign has 22 procedure stops, including eleven pre-loader, three native
application-start and one USB-return stop. This new compiled chain has zero
attempts before admission; only one is selected and its first failure is terminal.

The snapshot records the original 500ms/4096-step drain failure before the
original containment call: bound/elapsed, pending/path state, saved continuation
and first-toggle observations, software command history and eleven sequential
non-destructive register reads. It consumes no RX/payload, issues no command,
changes no PID and grants no completion. Failed USB state stays reserved even
if the FPGA emergency Park succeeds. This input diagnoses the failure and
cannot treat an unchanged timeout as a successful Return.

Read-only source admission checks both native/entry metadata hashes, the
complete selected source binding, committed native bytes and the exact matched
chain. Fixture cases reject stale/foreign metadata and bindings. Retain all
existing upload, service, receive-only tail, ownership and transport checks,
including the 25 per-CPU/firmware-reservation rejections. Healthy service needs
all 48 CALL, 26 LOADER, 31 RUNTIME, both EXIT_CACHE and prior cache/vendor/guard
receipts. The helper fixture initially ran before its audit existed; the
selected correction runs after the final audit. This is not a board attempt.

Fresh exact claim/USB/UART/recovery/card admission precedes one recovery,
eight checked Ethernet uploads and eight fresh SD readbacks. The seven catalog
files are unchanged; the eighth file is this fresh FIT. Native serial Tenfoot
then requests ZX81 ROM, JP-loop ROM and ZX81 ROM with matching activation
identities, bounded fetch, USB report progress, pause/resume and owned return/
Quit. No physical button press or HDMI acceptance is required or claimed.

Preserve prompt50/startup100/launch100/config20/control5/native450/child480/
work740/host850 seconds, receive-only tail2s/1MiB and ROM16B/400Mticks/64poll.
Hold the shared Quartus lock as normal user across the attempt; use only top
TRWU8CBL/JTAG4362C0DD and its private server. Temporarily use6M, then restore15M,
exact USB permissions, no owned private JTAG process and free UART. No retry,
fallback, timeout increase, guessed halt, SD ownership release or bottom-kit use.
The original factory card remains safe and restoration is waived. Retain claim
and spare; host cleanup alone cannot prove CPU/USB/FPGA halt.

This bounded diagnostic cannot qualify ordinary package admission or complete
DE10-equivalent loading. The active goal remains incomplete until normal
loading, return and required devices are verified.

The copied candidate gate initially required the historical ELF reuse flag;
it correctly rejected the fresh build. The selected gate requires rebuilt
app/entry inputs and their independent bindings, and explicitly rejects the
reuse claim. That off-board failure is retained and made no board attempt.

## Observed result

The one attempt on operator `f78fd7942414b39da903a32b8d28c3ad52765c3b`
and freshly built native/firmware `17b41b172e89e0df13e66488bfce2b5117ebee92`
completed recovery, eight verified uploads/eight fresh SD readbacks, native
programming and the matched boot. All 31 runtime, two exit-cache, 26 loader and
48 call receipts passed, including all eight initial/late SMC/cache returns.
Native Tenfoot reached Ready in 5.09 seconds.

The first launch key was sent, but USB failed before a launch completion or
core configuration/fetch receipt. The packet timeout reported ten polls,
expected PID2, one RX/IRQ toggle notification and zero continuations. The
saved first observation had channel `0xe05e8808`, HCINT `0x400`, HCTSIZ
`0x40080008`, RX-peek/pop `0xa0000` and frame `0xc28c01e4`. At that point,
the current packet's only recorded software channel write was its arm:
sequence229/packet40/value`0xa05e8808`. No software disable request for that
packet preceded the observation. This does not identify the physical cause of
the disable bit or establish that the device supplied a valid payload.

The last continuation sample stopped at `reason=channel`, mask `0xf` and
channel `0xe05e8808`. Its printed transfer/frame zeroes are unsampled fields,
not measurements of zero remaining transfer or a frozen frame counter. The
later containment write was sequence230/value`0xe05e8808`; its original500ms
wait failed to observe CHENA clear. No USB halt, new snapshot helper execution,
FPGA master-stop receipt, normal Return, second launch or Quit was observed.
Whether this packet error surfaced in frontend polling or the pre-launch
pause is not established by the final stack. The drain snapshot helper was
not reached; the existing first-toggle observation supplies this evidence.

The original failure was terminal and no hardware retry occurred. The passive
two-second tail and independent raw boot replay are retained. Independent
cleanup verifies15M, exact USB permissions, the owned JTAG server gone and
free UART. Claim/spare remain held; the original card and bottom kit are
untouched. CPU/USB/FPGA halt is unproved. Campaign procedure stops are now23;
this first-launch packet stop is distinct from the preceding USB-return stop,
eleven pre-loader stops and three native application-start stops. The consumed
matched chain will not be replayed. Full core-loading acceptance remains open.

## Primary-source comparison and next repair boundary

The pinned Linux v6.12
[PIO continuation](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c#L1496)
clears raw CHDIS while queuing another request for an existing IN transfer;
its scheduler gates this on software halt state. It does not reprogram HCTSIZ.
Its [IN toggle handler](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd_intr.c#L1715)
handles the notification without advancing the requested PID. These references
supply protocol context, not Nano hardware qualification or a linked Linux
implementation. Original licensed source headers and API Git-blob/SHA256
identities are retained. A raw-source download hit429; the successful API fetch
verified each Git blob before retaining it.

Our continuation currently stops on raw CHDIS before reading remaining packet
count. The next model prototype must assess continuation of the same owned
non-split IN packet after an observed toggle, preserving its PID, HCTSIZ,
buffer and deadline. Require actual expected-PID data, completion and channel
retirement; retain the software-halt/cancel/foreign-DMA/RX/completion/error/
queue/empty-transfer exclusions. Do not clear arbitrary disable state, extend
timeouts, fake completion or release ambiguous input. This is a proposed repair
boundary; no controller policy was changed by this test.

Exact observations, references and hashes are in
[physical-result.json](physical-result.json) and
[packet-stop-interval.json](packet-stop-interval.json). Full trace/source-pack
retention uses `/home/deano/fes-retained/de25-usb-drain-stop-20261106-r1`, with
prerequisite `de25-usb-drain-snapshot-20261106-r1`.
