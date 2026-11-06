# One bounded initialized-per-CPU DDR trial

The compiled firmware is `718b5c1427cecce2dc64868de8a9df7b599abf3c`.
Its initialized per-CPU array is DDR `0x80036000..0x80036100`, with the first
affinity state at `0x80036018`. The original `DC CIVAC` and all original PSCI
operations are retained. The format-4 private BSS ends at `0x5ddc0`; the native
application and entry remain the exact retained `31d7169577` artifacts. Both
loader/FIT DTBs reserve `0x80000000..0x80200000` as no-map; the compiled loader
requires one exact matching LMB reservation before initial CPU queries.

This is a new input after the consumed `23db3f0d5` trial stopped at
`cache-before`. That stop, its incomplete-call uncertainty and independent
cleanup remain retained. No retry of that input is authorized by this record.
The campaign contains 21 prior procedure stops, eleven before the loader and
three in the post-loader/application-start interval. This new firmware has
zero attempts before admission. One attempt is selected, first failure terminal.

The separate operator checks actual initialized ELF placement, immutable
bytes, both DTB hashes and exact reservation evidence. Its 25 DDR rejection
cases execute the actual admission helper. The existing upload, service,
source, transport and receive-only failure-tail fixtures remain required.
The copied call-tail fixture's first run used the old ARM output directory;
its corrected run consumes the new source-bound ARM console. Both receipts
are retained. Missing/invalid/reordered/fragmented diagnostics grant no boot
permission. Every healthy-service result needs all 48 CALL, 26 LOADER,
31 RUNTIME, both EXIT_CACHE and existing cache/vendor/guard receipts.

Fresh read-only source/claim/UART/exact USB admission precedes any operation.
The user-reported power interruption invalidates prior live state. Use one
qualified recovery, seven byte-identical catalog payloads plus this new FIT,
eight checked Ethernet uploads and eight fresh SD readbacks. Then select the
retained ZX81 ROM, JP-loop ROM and ZX81 ROM through native serial Tenfoot,
requiring matching activation receipts, USB report progress, pause/resume and
owned return/quit. This does not require or claim a physical button press.

All bounds remain: prompt 50s, startup/launch 100s, configuration 20s,
control 5s, native 450s, child 480s, work 740s, host 850s; failure tail 2s/1MiB;
ROM observer 16 bytes/400M ticks/64 polls. The top kit alone is selected by
TRWU8CBL and JTAG `4362C0DD`. The normal-user shared Quartus lock spans the
attempt. Use the private top-only JTAG server, temporarily set 6M, then restore
15M, exact USB permissions, no owned JTAG process and free UART. No fallback,
reset-bit bypass, deadline expansion, factory-card write or bottom-kit access.

The spare remains in the top board and its continuing claim stays held.
Factory restoration is waived. Host cleanup does not prove CPU/FPGA halt.
This trial confers no ordinary profile admission, physical HDMI/native-network
acceptance or DE10-equivalent loading status, even if all three cycles pass.
The full active goal remains open until the normal package-loading path and
its required device/lifecycle behavior are verified.
