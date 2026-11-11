# DE25 scanout reset and MTBF review follow-up — 2026-10-30

This fresh Quartus run covers the scanout reset-domain changes and sticky-fault
synchronizer in producer `26f9b46e6a1e591a30a49583b2b646559cdceb95`. It
supplements the 2026-10-28 scanout record; that record remains bound to its
earlier producer and SOF.

The build used Quartus Prime Pro 26.3.0 Build 114 for `A5EB013BB23BE4SCS` and
the pinned Terasic rev. B archive, SHA-256
`f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`.
Qsys integration and IP generation passed. Quartus completed synthesis, fitting,
Timing Analyzer and assembly with zero errors and 308 warnings total. The SOF
is 1,766,028 bytes, SHA-256
`69f326dfa1f739b8b25dae3c15689e387f0dd15541ff3ec574209930e3d4aeb9`.

The generated SDC found exactly six reset-synchronizer clear pins, with reset
release paths still timed, and exactly one sticky-fault source and one stage-0
D target. The Signoff Design Assistant reports **zero CDC-50011** violations.
The synthesis report shows Quartus accepted
`SYNCHRONIZER_IDENTIFICATION = FORCED` on `clock_fault_meta`; the second stage
remains timed. Across all four metastability corners, Quartus found 84
synchronizer chains, with the shortest at two registers, zero chains excluded
for timing violations, and 29 chains excluded from MTBF analysis. The
2026-10-28 SOF reported 30 excluded chains.

The internal Timing Analyzer summary has nonnegative slack on every reported
setup, hold, recovery, removal, minimum-pulse-width, max-skew and net-delay row;
all setup/hold/recovery/removal/pulse-width TNS values are zero. The minima are
3.192 ns setup, 0.000 ns hold, 7.497 ns recovery, 0.006 ns removal, 0.220 ns
minimum pulse width, 7.561 ns max skew, and 7.590 ns net delay.

This remains a host-only diagnostic build, not whole-board signoff. The
Design Assistant still fails five unwaived rule classes: missing external
output delays (30), missing input delays (2), multiple asynchronous reset
synchronizers (2 groups), reset polarity conflict (1), and intra-clock false
path reset synchronizers (3). The report also marks unconstrained paths as
Fail. Hardware acceptance, production DMA admission, external HDMI timing and
physical CDC/MTBF remain open; no hardware was used.

The local build receipt binds the exact input manifest, generated handoff and
SDC files, reports and SOF. Key SHA-256 values are:

| File | SHA-256 |
| --- | --- |
| `inputs.json` | `30ffcd30abe211c4686cbebae56798e23d559717874e533e1adf73699416b0df` |
| Scanout RTL | `631b93eee79cc791b3418d6d5bc731798985037d9b95a56b740094651cbdd14c` |
| Scanout SDC | `87ba1c4ac381304bfde192d5d334c18e8fc11e6ea7e0838c51bb9df479510ab6` |
| `golden_top.sta.rpt` | `589858fe9aa53e26a62a480256f59008697fd3283ea970375631277b498bbf0b` |
| `golden_top.tq.drc.signoff.rpt` | `97473d81d8b6cc15186a4dc1828a2da5c11d66b8bd33150a7d28f1636f265a81` |
| `golden_top.sof` | `69f326dfa1f739b8b25dae3c15689e387f0dd15541ff3ec574209930e3d4aeb9` |
