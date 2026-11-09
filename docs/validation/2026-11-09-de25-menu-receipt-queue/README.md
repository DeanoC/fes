# Menu CSR submission capacity

The preceding board trial loaded the complete 917-byte tape, ran ZX81 and
returned to its menu, then ran ZX80. Its second return failed a menu-frame
consistency check with the preceding receipt still sampled. The recorded status
was running/submit-ready and did not report the FPGA fault bit. The exact
rejecting comparison and intended new descriptor were not logged.

A fresh baseline simulation passes the existing actual Tenfoot/CSR/AXI/video,
firmware-stop and native-presenter tests. An injected gap after readiness is
sampled advances that actual RTL into a fill phase: a subsequent Submit is
rejected and leaves the preceding receipt unchanged. A transport that supplies
no CSR write rejection response reproduces the exact board error shape
(flags6/generation1/sequence1/submitted1/slot0/underflows0), with one command and
permanent retirement. This response-handling assumption is explicit; the model
does not prove it caused the physical failure.

The selected repair makes CSR readiness describe capacity to accept a frame,
rather than the video reader's current phase. After the preceding frame completes,
one new descriptor can be latched and held until video can consume it. Capacity
stays available through repeat-frame prefetch. Acceptance updates the submitted
receipt; completion still requires the exact displayed generation/sequence/slot.
A second outstanding descriptor rejects. Staging writes cannot alter the held
descriptor. Stop, reset and ownership loss discard the pending handoff without
releasing either pinned buffer. The native driver, single-command submission,
existing ownership checks and all time/poll bounds remain unchanged.

The new capacity regression fails on the retained old control, as expected.
The prototype control passes it and all existing actual-RTL presenter cases.
Fresh normal verification also passes with the selected production source and
integrated regression. All nine selected checks pass, including the fresh full FPGA Python suite,
parent regressions, generated consistency and native producer integration.
The planned menu check is satisfied by the fresh normal selected-source run,
with only an explicit output directory added. The actual fixtures and source/command/log hashes are retained in
`out/nano-menu-receipt-timing-20261109`; the standalone injected tests are archived
here as text. The prototype is not a second production path.

The selected RTL requires a new source-bound Quartus menu artifact, identity,
matched native chain and fresh bounded board trial. Previous physical evidence
qualifies only its original source/image and does not qualify this repair.
Ordinary appliance acceptance and physical display/input remain open. Old-file
cleanup remains paused.
