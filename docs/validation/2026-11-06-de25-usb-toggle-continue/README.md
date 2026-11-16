# Qualified continuation after an IN toggle notification

Implementation `c8ff50c2989f04e6a1f873923409d0165f94972d` addresses the
[first-launch packet stop](../2026-11-06-de25-usb-drain-trial/README.md).
That trace observed direct-IN toggle notifications, raw CHDIS and an unfinished
transfer before any software disable write for the packet. The previous helper
waited on raw CHDIS before sampling the remaining transfer. The physical cause
of that bit remains unproved.

The selected repair continues the same owned packet only after paired RX/IRQ
toggle notifications and a saved first observation. CHENA and the original
channel recipe must match; software fault/halt, OUT/split, done/received/NAK,
new RX/status and queue/interval exclusions remain in force. The helper freshly
checks PIO/link ownership and requires HCTSIZ to match the full original size,
PID and single-packet count, including no unexpected control bits. It resamples
RX/status before clearing raw CHDIS and queuing one continuation. It writes no
HCTSIZ, setup, buffer, PID or packet deadline; periodic spacing remains intact.

This follows the pinned primary
[PIO continuation](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c#L1496)
and software-halt scheduling distinction, with stricter admission for the
observed paired-notification case. Linux is a protocol reference, not a target
runtime dependency. The previous archive retains source headers, Git-blob and
SHA256 identities. An unqualified disable, expired packet or actual software
halt cannot be cleared by this repair. A notification/continuation cannot
supply input: real expected-PID bytes, completion and channel retirement remain
necessary. Failure still retains the source and all uncertain ownership.

Validation:

- The captured-state regression fails on the previous helper, then passes with
  the repair: it clears only qualified raw CHDIS, keeps the same packet/PID/
  HCTSIZ/deadline and withholds the report until retirement. Device/FIFO state
  independently supplies the actual expected-PID bytes.
- Four new test methods cover that success, 27 exclusions, RX/error races,
  original deadline expiry, wrong PID and short data. Exclusions include
  missing notifications/first observation, fault/ownership/halt/completion,
  changed channel/size/PID/control bits, foreign DMA/interrupt modes, disconnect,
  queue pressure and spacing. New RX or channel status arriving during
  admission prevents another token and is processed by the original step.
- The earlier captured-state test assumed all raw-disable observations must
  wait. It now checks that qualified continuation still cannot admit missing
  data or foreign DMA; the unqualified-disable exclusions remain separate.
  Its original failure and the captured-state red regression are retained.
- Full DWC2, USB ownership and native-fabric packages pass with the race detector.
  All six affected-check steps pass in 264.35 seconds, including 1,568 parent
  tests/three skips and generated-consumer consistency.
- Source inversion recovers the old continuation outside the two admitted
  blocks. The actual new ARM64 ELF links continuation, PIO/link and channel
  write functions. The original drain-snapshot-before-containment call is
  separately checked. Source/ELF hashes and instruction addresses are retained.
- Fresh native app/entry has one changed compiled source among 2,099 combined
  inputs, with 2,098 unchanged; all forty entry-producer records are unchanged.
  Memory mappings, compiled firmware exclusion, FPGA, ROM and configuration
  contracts remain exact. Twenty secure device-driver objects and both vendor
  C startup objects are byte-identical. BL31/loader/composition and prior RAM,
  cache, exception, call, runtime and reservation audits pass. Five fresh ARM
  model cases retain their modeled dispatcher/cache/UART limitations.
- A loader-tail fixture was started before its linked ELF existed. Its selected
  corrected run follows the completed build; the failed off-board log is kept.

Only final native app/entry, BL31, U-Boot and file-composition outputs under
`out/nano-usb-toggle-continue-20261106` are selected. FIT size is 8,194,608
bytes; matched chain identity is `38e2e85c7b7fabef22261a75fe52bfbaddbfede66b20147793699332f5b43144`. Conversion held the shared Quartus
lock as normal user and did not program hardware. This input has zero physical
attempts at this milestone; the previous `17b41b172` chain is consumed.

Next freeze this source-bound native rebuild and qualified-continuation-aware
operator; require every prior 48 CALL/26 LOADER/31 RUNTIME/two EXIT and upload/
service/failure-tail proof, fresh exact kit/recovery/card admission, then one
bounded three-session attempt. Keep prompt50/startup100/launch100/config20/
control5/native450/child480/work740/host850 seconds, tail2s/1MiB and ROM16B/
400Mticks/64poll. Stop first failure, restore15M/exactUSB/privateJTAG/UART and
retain claim/spare. Factory restoration remains waived, factory/bottom untouched.
No model or file audit establishes USB halt, physical cause or ordinary core
admission. Full DE10-equivalent loading remains incomplete.

Exact sources and evidence are in [result.json](result.json). Full build/source
retention uses `/home/deano/fes-retained/de25-usb-toggle-continue-20261106-r1`,
with prerequisite `de25-usb-drain-stop-20261106-r1`.
