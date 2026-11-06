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
