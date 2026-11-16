# Independent tape storage through native CPU and firmware

The earlier [warm-core trial](../2026-11-15-de25-warm-core-and-tape-stop/README.md)
completed warm ZX81 configuration and source retirement, then refused tape
publication into its retained configuration arena. The original FIT was restored
and independently verified after the human reset; the recovery loader remains
parked. This source change connects the separately tested
[arena prototype](../2026-11-15-de25-independent-tape-arena/README.md).

The explicit Nano initial-tape build now reserves the entire 2 MiB range at
`0x95800000`, with a 16 KiB payload and metadata at `0x959ff000`. It keeps the
8 MiB configuration reservation and both 4 MiB MENU slots intact. SDK, CPU entry
and cold firmware bind the same memory contract and digest, require the complete
paired CPU resource map, and exclude the tape reservation from entry staging.
Old or mixed selections without this contract reject. This typed geometry
supplies no physical ownership or activation authority.

The board constructs one pinned independent publisher and maps the full range
as normal uncached, execute-never memory. Every tape SMC receives stable mapping
checks on both sides, covering tape, configuration, both MENU slots, heap and
firmware exclusion. Existing EL3 tables already map this shared DDR as normal
uncached, outer-shareable, non-secure and execute-never; tests now include the
new payload, metadata and end boundary. The strong fixed supplier reads and
hashes the independent addresses. No request can choose an address. Existing
ownership, bridge, USB/SD, held-ROM, frame, CRC/SHA and deadline checks remain.
The old borrowed configuration publisher and its lease fields are removed.

Each publication still requires a new completed private program lease. Tape
completion cannot release that source. Publisher tests cover successful and
uncertain transfers, cancellation, malformed progress, stale generations,
source changes, copied owners and backing aliases. Board-hook tests join the
actual C service while preserving configuration bytes. Strong firmware tests
use the exact vendor SHA implementation and fixed host address mappings, hash
the entire 8 MiB configuration reservation before/after each successful or
failed tape transfer, and retain the original corruption checks at the new
tape address. Mapping tests reject changed/aliased/executable source, MENU or
tape blocks before an SMC or suppress success after an issued operation.

Validation receipts and source hashes accompany this record. The first full
Go race invocation used an insufficient two-minute harness cap and timed out
in an unrelated ROM-preflight test. The completed full run passes with its
explicit ten-minute harness cap; no hardware or firmware deadline changes.
The first strong C run caught a fault injection still mutating the old
configuration address. It was moved to tape storage without weakening the
configuration-preservation assertion. A focused Python invocation initially
omitted the firmware import path; its corrected runner passes.

These are software and model results. No new boot image or physical tape/launch/
return acceptance is claimed. The last physically tested source remains
`82e607ab`; it must not be used as hardware evidence for this replacement.
The next step is a clean matched SDK/entry/BL31/SPL/U-Boot build, composition and
file admission, followed by a fresh bounded procedure tied to the verified
`fbe754ce` restoration. Consumed procedure `16fe97ce` will not be retried.
QSPI, the factory card and bottom kit remain untouched.

The complete affected plan passes: 2,754 parent tests (10 skips), native lifecycle, producer identity and firmware/RTL integrations, and 2,181 FPGA tests (88 skips). The full Go race run passes 342 tests and 1,252 subtests; 29 focused Python/strong-C/MMU tests and 15 final selection/layout/paired-profile tests pass. Generated C and compiled Go geometry agree. Source/model archive `4e525218d7fdc659f295f8d09d0121b8b0a28739846624a86aed877973e50ae6` independently verifies 208 entries. These checks open no board devices and qualify no boot image.
