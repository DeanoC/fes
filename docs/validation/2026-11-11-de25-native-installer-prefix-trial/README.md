# Corrected bounded native installer trial

The preceding [attempt](../2026-11-11-de25-native-installer-controller/hardware-result.json)
booted recovery successfully and stopped before any SD write because the host
parser rejected U-Boot's same-line MMC progress/completion record. The captured
response now passes upload and fresh-read regression tests; wrong device,
block, count, duplicate/error completions and trailing output still reject.
The original failed trace, archive and source remain retained without replay.

Source `96c5a5b91` passes all six selected software gates. The same native image
`c491ac67743d3526` and compiled source `49752d8b2` remain selected. Recovery was
regenerated through its normal producer with current host bindings; its FIT,
SPL, SOF, protected manifest and prefix remain byte-identical.

The separately frozen operator admits ten fixed uploads, manifest last, then
an empty native library and ordinary Tenfoot Install, Library, one ZX81 launch
and final owned Quit. Model flow, injected actual UART exchange and saved-wire
receipt replay pass. Full native file admission covers 5,639 files/eight trees.
Existing phase limits, qualified private 3MHz/auto0 JTAG, shared lock, continuing
top-only claim, fresh transport/UART checks and single-use grants remain in
force. First failure stops with no retry; exact clock/USB/server/UART cleanup
is retained. A fresh discovery boot is a separate experiment, so this first
install attempt cannot establish persistence or full appliance acceptance.

Only TRWU8CBL and its installed spare card are selected. No physical keyboard
action is needed. Native Ethernet stays disabled and physical display
verification remains separate. Factory restoration is waived; its original
media stays protected. Cleanup of old output remains paused.
