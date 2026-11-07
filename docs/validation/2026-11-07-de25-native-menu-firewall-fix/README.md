# Native menu firewall and opening-order repair, 2026-11-07

The first [bounded menu attempt](../2026-11-07-de25-native-menu-trial/README.md)
reached native startup and completed FPGA configuration, then rejected the
menu's 8 MiB firewall observation before any register read. The observer still
accepted only the earlier 8 KiB and 4 MiB extents. The repaired observer admits
the exact private 8 MiB menu extent and continues to require complete stable
coverage under the same ownership checks. Unsupported sizes, partial coverage
and unstable or failed reads do not grant DDR access. It has no register-write
or bridge-control callbacks.

The joined C menu-dispatcher regression now uses the actual firewall observer
during readiness and at the data-release gates. A region covering only the
first slot prevents DDR release and remains contained. The observer passes
1,868 cases across all three selected sizes; the joined menu path passes
23 ownership, ABI, coverage and failure cases. The earlier implementation is
rejected by the new positive menu regression.

The native progress marker precedes the firmware's readiness query and initial
route observations. The decoder now checks those records inside `open_begin`,
before requiring control opening. It preserves the original secure record
order and line numbers in the shared boot decoder. The exact saved physical
failure now decodes as complete configuration followed by the zero-read size
rejection, with no accepted frames. Ninety-one focused trace regressions pass;
the misplaced earlier synthetic ordering is rejected.

Fresh BL31, SPL/U-Boot and HPS-first composition use this repair. The sealed
FPGA payload, native SDK, entry image, two-slot layout and CPU exclusion are
unchanged. The actual linked SPL retains its final allocation guard, rejection
branch and private RAM cookie. BSS remains at `0x58000..0x5afc0` within the
unchanged firmware reservation. The FIT remains 8,051,232 bytes within its
8 MiB limit. Exact input/artifact hashes, source revisions and check receipts
are in [result.json](result.json). The deployed implementation is `ec65d9f44`;
`26871f61b` refines only the joined test fixture and prose, with its focused
regression checked separately.

These are repaired build, model and capture-replay results. No physical menu
frame or repaired-chain hardware acceptance follows from them. Full FPGA
timing, board electrical constraints, physical DDR/drain, HDMI and ordinary
menu/core/menu use remain open. The previous procedure and raw stop are
retained and consumed. The next hardware attempt requires its own frozen
operator, fresh top-kit/card verification and complete bounded result.


The parent suite passed 1,633 tests (3 skips), and native runtime, producer and
firmware/RTL integration commands passed. The broad misteross suite ran 1,595
tests (79 skips) and failed one unchanged GPU lease test: its simulated route
had not started before a 0.5-second deadline. The complete 17-test lease module
then passed in isolation, and the failed case passed against an exported clean
main tree. The test and five implementation/dispatcher inputs have identical
Git blobs on main. No timeout or production code was changed for this result.
The original broad command remains recorded as failed; the isolated passes
complete the focused follow-up and do not turn that command green. This host
scheduling-sensitive failure is separate from the passed menu/firmware gates.
