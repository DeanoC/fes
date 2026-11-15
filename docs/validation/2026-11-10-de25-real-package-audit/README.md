# Exact-package native launch and Tenfoot audit

Base is `1a242969d`; test implementation is `6e2b90383`, followed by one stronger
failure-boundary assertion at `79fab5060`. Only
`nativefabric/built_distinct_rom_test.go` changes. No production source, shared
contract, FPGA source or dependency changes. FES owns this integration evidence.
All work is local: no kit, card, JTAG/UART session, remote builder or cleanup.
Physical testing still waits for the user to return.

The bounded SD selector already accepts up to 16 program entries. Owned install
and fresh-boot discovery have separate model/FAT coverage. The two selected
hardware packages are the current examples in this diagnostic path; the
selector is not hardcoded to two UI commands. Native support still requires
compatible platform/ABI/resource maps and cannot load a Cyclone V artifact on
Agilex merely because it appears in a catalogue.

This audit executes the opt-in artifact tests against the current SDK and exact
sealed ZX81/ZX80 catalogues, retained 8/4 KiB ROMs and 917-byte ZX81 tape. The
original cycle failed during tape loading because the artifact fixture supplied
only computer mailbox replies. Its failure log/source are retained. The same
original fixture passed with a separately copied no-tape selector. An overlay
adding the existing tape mailbox model passed against the unchanged real tape
selection. These controls isolate the fixture gap; they do not establish a
production or physical input failure.

The canonical fixture now supplies those same tape responses. Assertions check
exact delivered bytes and running tape receipts, no stale tape receipt on ZX80,
and one media delivery before each Run. The real host-client/store/loader/arena/
configuration/computer layers complete ZX81 -> ZX80 -> ZX81 and owned returns.
Every actual ROM descriptor publication passes the real C decoder. Stale
sessions cannot control their replacements.

The actual Tenfoot App additionally completes three launch/input/return cycles
through the ordinary mailbox path and three through a modeled initial-tape
platform callback. Each uses the real current archive and ROM/tape selection,
software rendering, and modeled keyboard press/release. Four real-tape failure
cases cover rejected commit, uncertain platform completion, wrong CRC and changed
owner. They must reach exactly one held media start/commit, perform no Run,
retire ownership, keep SD contained and reject replay. Four corrupt-content and
three configuration/identity/return failures also pass. The no-tape cycle still
passes after the fixture change.

The full nativefabric Go race suite and initial extended artifact cases pass on
`6e2b90383`; the final held-media assertion is separately race-tested on
`79fab5060`. The seven affected commands pass, plus a final committed-diff check
through that final test head. The operator helper paths are refreshed and all
45 staged Python model checks plus the Go real-package tape preflight pass.
The original fixture failure and an intermediate test compile error remain in
the private evidence rather than being overwritten.

All 2125 native SDK input hashes, entry inputs, firmware producer inputs and
composition inputs still match current bytes. The changed test is absent from
the native SDK compiler inputs. Therefore boot candidate `a02d808ea11eb54f` is
reused with its original ELF/FIT/SOF bytes; no boot rebuild is needed for this
test-only repair. The secure MAC supplier remains unbound, network hook unselected,
Ethernet disabled, SD read-only and preferences volatile.

Evidence and the refreshed offline bundle are under
`out/nano-real-package-audit-20261110/`. After this documentation checkpoint,
`source.json` is rebound to the exact checkout. The complete file-only native
and recovery validator creates `native-file-admission-final.json` and
`admission-proof-final.json`, including the changed test and this audit record.
`offline-preparation.json` retains the finished checks and input inventory.
Those records must pass before the preparation is treated as complete. No live
operator record, preflight, programming grant or lease refresh is created here.

The next physical attempt still requires the continuing top-only TRWU8CBL lease,
current unique stable connection, free UART, current hashes and the user's
keyboard/monitor availability. Then perform the existing single bounded
menu/ZX81/keyboard-Stop/menu/ZX80/Quit/menu/Park cycle, with unchanged timeouts,
first-failure stop and owned cleanup. Preserve the original factory card;
factory restore remains waived, old-output cleanup paused, and the other kit
excluded. No current board state is inferred from these files.

Secure calls, device ownership and mailbox responses are modeled in these tests;
only the descriptor decoder is actual C execution in this particular layer.
Separate suites retain their own C/RTL scope. Software pixels and scripted input
are not HDMI or physical USB acceptance. Native boot/keyboard/HDMI on this image,
qualified physical MAC/drain/coexistence, writable SD and full DE10-equivalent
core-loading acceptance remain open. The goal is incomplete.
