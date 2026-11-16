# XGMAC process-stop acknowledgements

The polling driver's controlled stop now waits for fresh transmit and receive
process-stopped acknowledgements before the independent platform drain callback.
It checks transmit FIFO/read state and receive queue readiness before disabling
MAC traffic, then drains receive FIFO, stops receive DMA and requires both
acknowledgements. Missing, early, stale or faulted status retires the device;
100 ms and finite poll bounds still apply. The DMA window stays pinned.

The vendor [stopping and starting transmission procedure](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/stopping-and-starting-transmission?contentId=aoHUqRdCxX615dKqdJAazA)
requires these process-stopped waits. They were omitted by the matched Terasic
BSP stop routine, which also ignores FIFO loop expiry. The official
[CS/R1 register map](https://docs.altera.com/v/u/resources/864455/agilextm-5-cs-and-r1-suffix-opn-hps-register-map)
locates TPS/RPS at channel status bits 1/8 and defines TRCSTS as two bits.
The BSP's three-bit TRCSTS mask could treat a busy transmitter as ready if its
adjacent reserved bit were set; the driver now uses the documented two bits.
A frozen field-fact fixture checks these definitions without network access.

The downloaded archive is labelled RC24, while its sole HTML member identifies
RC28_B0; both labels are preserved rather than treated as the same version.
Archive SHA-256:
`c4720c039263d5423662994fc41cd1c9a324f9fcf3bc1187a3a742f532537925`.
Member SHA-256:
`871c382b1b390efe7d33d8cfa0dd9cab5f48571296aa71288687d5e20a89d208`.
The programming model is TRM 814346 v26.1.1. The local evidence directory
`out/nano-xgmac-ack-20261110/` retains the archive, extracted map, source hashes,
commands and logs. No downloaded HTML or JavaScript was executed.

The vendor [DMA host interface description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/dma-host-interface?contentId=Z2tcFlrRyHbG_AoYEkmq9A)
describes posted buffer writes. Process-stopped status therefore supplies driver
completion evidence only; this change does not establish an independent
interconnect fence. The explicit trusted platform drain/grant supplier is still
required and is not bound on Nano. A source-bound firmware MAC lease, private
peripheral generation, clock/SMMU ownership and active-core coexistence remain
open. No firmware protocol, shared generated contract or board profile changes.

The focused XGMAC/network race suites pass. They cover delayed/missing/faulted
acknowledgements, early RX stop, lost TX acknowledgement, stale initial flags,
cancellation, reserved-bit masking and the existing readback/frozen-clock paths.
The actual descriptor/IP-stack HTTP test completes three stop/reinitialize
cycles with explicitly modeled platform drain/grant callbacks. These are
software tests, not physical DMA qualification.

All six affected checks pass, including parent Python regressions and generated
consumer consistency. Selected source hashes and the vendor archive/member
match their recorded identities after testing. The full Nano ARM64 launcher
cross-build passes from committed source `2d6f393395d1545da59c072857e7d033fa89ed65`.
All 2121 compiler input hashes match their recorded source.
ELF SHA-256: `d3cd28799cb7733e546ae8070444c94f8c723e67024562042dd79292e78522ec`.
Network remains disabled, SD read-only and preferences volatile. No matched
entry/BL31/FIT/SOF was rebuilt or deployed; this is cross-build evidence only.
The change is based on `5bf212209009babbda1f5d0a4dddc43286130de2`.
No kit, card, programmer or remote builder has been touched. Hardware testing
remains paused until the operator returns; keyboard/HDMI, physical SD writes and
DE10-equivalent normal core-loading acceptance remain open. The next physical
step is the bounded keyboard/monitor launch-and-return test once the operator
is available. This network repair does not enable Ethernet for that test.
