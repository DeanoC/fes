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

Both RAM Tester hardware trials PASS at source `e7e73a0c0`. Each completed
the six-pattern 128 MiB scan through halfword address `03ffffff`, all 128 byte
cases / 512 reads, and the three HPS DDR scans with zero errors. See the
per-kit JSON receipts and final captures. Both baseline image/bundle identities,
idle agent/session and ownerless leases were verified after Stop.

The installable PC/XT producer now selects the same CAS-3 path explicitly.
Its normal production build, repeat check and exact-artifact PC/XT hardware
acceptance must be recorded separately from these RAM Tester passes. The RAM
Tester native diagnostic selects the path with `RAMTEST_CAS3_CAPTURE=1`; its
existing OSS catalog producer is a separate compiler path and is unchanged.

Physical flight/load/slew, clock waveform and electrical turnaround limits
remain separate from bounded hardware operation.
