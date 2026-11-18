# Shared CAS-3 capture: two-kit qualification

Deano authorized enabling the experimental path for RAM Tester and PC/XT
trials on both kits, followed by default adoption only if both pass. This
replaces the earlier stop before any hardware trial; it does not grant a
physical board timing or whole-board signoff claim.

The shared `sdram_cas3_capture` module contains the exact READ history,
once-per-read word retention and falling controller-edge landing used by both
consumers. RAM Tester's opt-in native 100 MHz build uses the same phases and
CAS-3 controller profile, a 700-clock refresh schedule, input I/O capture and
registered command/address outputs. Its full-span scanner and byte preflight
consume the landed word; neither may silently read legacy controller rdata.
Memory/capture/forwarding/handoff clocks remain related in TimeQuest.

Builds and proof sources are frozen before tests. New artifact identities,
native timing, raw reports and SHA256 digests are retained under
`/home/deano/fes-hil/k835-cas3-kits/`. Each kit requires its status-file claim,
target lease, a clean full memory scan and PC/XT operation, followed by verified
restoration of the named baseline image/bundle and idle, ownerless state.
Stop after two failures on the same head and fix. No other owner's lease or
process may be displaced. Default adoption is conditional on both kit passes;
fresh normal producer/seal checks and applicable exact-artifact acceptance
remain required for the adopted bytes.

HIL is **NOT STARTED** until a kit is claimed and the test artifact loaded.
Physical flight/load/slew, clock waveform and electrical turnaround limits
remain separate from bounded hardware operation.
