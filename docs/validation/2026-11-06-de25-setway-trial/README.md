# Bounded cold set/way startup trial on TRWU8CBL

This freezes one fresh operator for firmware `0cab5df0c`, selecting only the
final BL31, U-Boot and composition outputs from the sibling
[set/way validation](../2026-11-06-de25-setway/README.md). The native application
and entry remain byte-identical to `31d716957`; the sealed FPGA is unchanged.
The matched FIT is 8,187,904 bytes and chain identity is
`e86db075cff9809a7679ca227bf1a69b6b1d71e39b4891bb1336c846da04615c`.

The candidate proof binds the actual linked helper, mode0/DC ISW selection,
uncached SPL controls, selector checks, exclusive cold ownership, unchanged
20 firmware driver objects and source-bound RAM contract version2. Existing
126 off-board ARM cases and final affected checks are reused for those exact
inputs. They do not establish physical cache coverage or native boot.

The operator requires both ordered CACHE receipts and complete normal logging
restoration, alongside ENTRY/PASS, all15 vendor checkpoints, empty fault
inventories and existing ownership checks. Its actual App acceptance gate
passes the modeled three-session cycle and rejects74 damaged receipts,
including missing, partial, invalid, reordered and duplicate cache evidence.
The actual receive-only failure collector and real decoder pass21 fragmented
cases using retained ARM exception traces and explicit damaged variants.
A canonical delayed-status failure after verified restoration is recorded as
`restored-with-error`; it still rejects boot. Malformed, duplicate or incomplete
terminal evidence remains unproven. Cache progress is diagnostic only.

Source/recovery, fixed-upload/readback, private JTAG, clock, exact-device and
console-bound fixtures pass. All seven catalog files are verified identical
to the prior catalog; the eighth upload binds this final FIT. Offline fixture
preparation failures are retained: a one-line fault had no remaining fragment,
an unterminated line is deliberately ignored by the decoder while the host
failure gate stops, and a clock fixture retained the old campaign count.
Corrections changed no board state. The selected successful receipts and
exact helpers are in [operator-source.json](operator-source.json).

There are16 retained campaign stops: one programmer failure, two host setup
stops, four ROM deadlines and nine pre-native firmware stops. This new input
has zero physical attempts at freeze; consumed predecessors cannot be replayed.
Deano reported another power interruption during host preparation. USB identity
and the serial link are present, but boot/card/CPU/FPGA state must be freshly
established. No programming, upload or SD write was running in this session.

Read-only preflight checks the continuing top-board claim, frozen source,
exact USB identity, free UART, disk and network route. The normal-user shared
Quartus lock surrounds the private JTAG session. Exact TRWU8CBL/chip4362C0DD
admission at6M precedes recovery; the pinned recovery must establish its
checked loader and spare-card/protected-file state before eight fixed uploads
and eight fresh readbacks. Factory card and bottom kit remain untouched.

One attempt, stop first failure, no retry or fallback. Keep prompt50/startup100/
launch100/config20/control5/native450/child480/work740/host850-second limits,
receive-only2-second/1MiB fault tail, and ROM16-byte/400M-tick/64-poll guards.
Only a fully healthy boot may issue `fesboot` and attempt the three Tenfoot
ZX81/JP-loop/ZX81 launch-return-Quit cycles, activation receipts, USB report
and pause/resume checks. No physical input action is required or claimed.

Afterwards restore/read back15M, exact USB permissions, stop the owned private
JTAG server and independently verify free UART. Retain claim/spare; factory
restoration is user-waived. Host cleanup does not prove CPU/FPGA halt.
This freeze precedes live admission and physical evidence; ordinary core-load
acceptance, HDMI, native Ethernet and DE10-equivalent behavior remain open.
