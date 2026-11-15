# One bounded qualified USB toggle continuation trial

The selected native app/entry and matched firmware were built from
`c8ff50c29` under `out/nano-usb-toggle-continue-20261106`. One compiled native
source differs from the preceding diagnostic chain; 2,098 combined app/entry
input hashes remain unchanged. Current source snapshots, compiled functions,
entry, exact firmware/loader chain and both DTB reservations are bound. No
historical native ELF is substituted.

This input permits raw CHDIS clearing only for the same owned enabled direct
IN packet, paired RX/IRQ toggle notifications and saved first observation,
without a software fault/halt or completion. Channel recipe and exact original
HCTSIZ size/PID/count must match, with fresh PIO/link ownership and a final RX/
status recheck. It cannot issue SETUP, reprogram HCTSIZ/PID/buffer, start another
packet or reset its deadline. Queue space, periodic spacing, real expected-PID
data and channel retirement remain necessary. Every unqualified/foreign/
completed/halting/expired state retains its existing exclusion and containment.
The original 500ms/4096 drain and pre-containment snapshot remain unchanged.

Four model methods cover the captured success, 27 exclusions, new RX/error
races and original deadline/wrong-PID/short-data failures. Complete DWC2, USB
ownership and native-fabric race tests and all six affected steps pass. The
selected native ELF, source inverse, secure objects and matched file-chain
audits pass. Physical forward progress and USB halt remain unqualified before
this attempt.

The previous `17b41b172` chain is consumed after a first-launch USB packet
stop; its raw first-toggle/CHDIS observation, no prior software disable and
failed halt are retained with independent cleanup. The earlier successful
ROM launch/USB-return stop is separate. Campaign procedure stops are23,
including eleven pre-loader, three application-start, one USB-return and one
USB-packet-before-launch stop. This new chain has zero attempts before
admission. Exactly one is selected; its first failure is terminal.

Fresh exact source/claim/USB/UART/recovery/card admission precedes one recovery,
eight checked Ethernet uploads and eight fresh SD readbacks. Seven catalog
payloads remain byte-identical; the eighth upload binds the new FIT. Native
serial Tenfoot requests ZX81 ROM, JP-loop ROM and ZX81 ROM with matching
activation identities, bounded fetch, live USB report progress, pause/resume,
owned return and Quit. Healthy service still needs all48CALL/26LOADER/
31RUNTIME/twoEXIT and prior cache/vendor/guard/ownership receipts. No physical
button or HDMI acceptance is required or claimed. An early loader-tail fixture
ran before its linked ELF existed; the selected corrected run follows the
completed build and its failed log is kept as off-board evidence.

Preserve prompt50/startup100/launch100/config20/control5/native450/child480/
work740/host850 seconds, tail2s/1MiB and ROM16B/400Mticks/64poll. The top kit
alone is selected by TRWU8CBL/JTAG4362C0DD. Hold the shared Quartus lock as
normal user; use only the private top server, temporary6M then restored15M,
exact USB permissions, no owned JTAG process and free UART. No replay,
fallback, widened timeout or unproved buffer release. Factory restoration
remains waived; factory card and bottom kit are untouched. Retain claim/spare;
host cleanup is not proof of CPU/USB/FPGA halt.

This remains a bounded diagnostic. Even three completed cycles do not grant
ordinary package admission, physical display/native-network acceptance or
DE10-equivalent platform status. The full active goal remains incomplete.

## Observed result

The one attempt used operator `7fdaf6358` and native/firmware implementation
`c8ff50c29`. Recovery, eight checked uploads/eight fresh SD readbacks, native
programming, all31RUNTIME/twoEXIT/26LOADER/48CALL receipts and eight initial/late
SMC/cache returns passed. Native Tenfoot became Ready in5.09s.

The first launch key was sent. Packet44 expected PID2, with one RX and one IRQ
toggle notification. The saved first observation had channel`0xe05e8808` and
HCTSIZ`0x40080008`; its current-packet software history contained only arm
sequence232/value`0xa05e8808`. The last continuation record sampled all six
register groups (mask`0x3f`), verified the same transfer, read frame`0xb4e90374`
and reported `reason=queued`. Software sequence233 continued that same packet
with value`0xa05e8808` and PID2. This physically exercises the selected raw-
disable continuation; it is not inferred from model results alone.

The following packet poll failed on channel error`0x200`. The matched vendor
header defines bit9 as frame overrun. The error reported two polls, one
continuation and no received/done/NAK flags. No payload, completed launch,
fetch, master-stop, normal Return or Quit was observed. Containment sequence234
wrote`0xe05e8808`; its unchanged500ms wait did not observe CHENA clear. The
new drain helper was not reached. The first failure was terminal and no
hardware retry occurred. This is a failed hardware candidate, not USB or core-
loading acceptance.

The first-toggle HFNUM was`0xd7a20374` and the continuation HFNUM`0xb4e90374`:
both sampled frame number`0x374`, with the remaining counter decreasing by8889
ticks. The continuation write selected ODDFRM1. Those samples do not establish
the originally intended wire frame, frontend service gap or physical cause of
the overrun. No additional timing claim is derived from the two register values.

Independent raw boot replay and the bounded passive tail are retained.
Independent cleanup verifies15M, exact USB permissions, the owned private JTAG
server gone and free UART. Claim and spare remain retained, factory restoration
waived and factory/bottom untouched. CPU/USB/FPGA halt is unproved. Campaign
procedure stops are now24; the earlier USB-return, packet-timeout-before-launch,
eleven pre-loader and three application-start stops remain separate. The
consumed C8 matched chain will not be replayed.

## Model gap and next boundary

The injected controller currently supplies an IN response synchronously when
CHENA is written; it does not enforce periodic frame eligibility or wire time.
Its success tests prove data/PID/retirement and ownership rules, not the USB
schedule exercised here. The normal direct-IN poll advances one transaction
step, and the frontend drives report polling; its actual hardware service gap
was not measured. Always-next-parity selection also differs from the pinned
[primary frame scheduler](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c#L956),
which accounts for queued work, bus time and the scheduled wire frame. That
comparison identifies missing model coverage; it does not prove the cause.

Next add an independent frame/queue/transfer-time controller model with delayed
periodic completion and real retirement. Vary frontend polling gaps and frame
parity before selecting a timing/service repair. Keep the paired-toggle and
ownership/PID/HCTSIZ/deadline exclusions and all original bounds. Never mask
`0x200`, fabricate NAK/completion, release an unhalted channel or reinterpret
this failed attempt as acceptance.

Exact raw observations, source comparisons and hashes are in
[physical-result.json](physical-result.json) and
[frame-stop-interval.json](frame-stop-interval.json). Full trace/source-pack
retention uses `/home/deano/fes-retained/de25-usb-toggle-stop-20261106-r1`, with
prerequisite `de25-usb-toggle-continue-20261106-r1`. The active full platform
readiness goal remains incomplete.
