# Slice 3: the first board build (one seed)

Revision `1432fee23` runs through the generalised managed builder:
`boards/de25nano/pc386/build.py build`, Quartus Pro 26.3 on
chip-30x0-ai-dev, the project's seed 1. Platform Designer generation and
every builder check pass, among them:

- the exact scanout+window F2SDRAM handoff;
- the managed-fabric initiator set;
- the window bridge's parameters and routes;
- the pinned peripheral parent;
- the shared FIFO reset derivation.

Every crossing is named in `pc386/pc386-clocks.sdc` against its synchronizer,
and no clock group is cut. This is one seed. The controlled four-seed pair is
still to run.

| | Build `i` |
| --- | ---: |
| ALMs needed, whole design | **42,655** (91.1%) |
| Machine (`system`) | 33,935 |
| Shell (whole − machine) | 8,720 |
| F2SDRAM interconnect | 2,054 |
| Window adapter + arbiter + control block + bridge | 654 + 497 + 132 + 110 |
| Session display | 1,349 |
| RAM blocks | 295 / 358 (82%) |
| DSP | 24 / 188 |
| Worst setup | +1.538 ns (pixel); machine clock +1.713 ns; 100 MHz +3.567 ns |
| Worst hold | 0.000 ns, no failing endpoint |
| Worst recovery / removal / minimum pulse width | +6.910 / +0.074 / +0.220 ns |

The build before it, `h` (`9ddea05eb`), missed the machine clock by 0.058 ns
on 34 endpoints. Those paths ran from the window's grant base through the
full-width rebase adder into the arbiter's lane logic. The grant is 4 KiB
aligned by contract, so the adapter now adds only the page number. The
in-page address no longer depends on the base, and those paths are gone.

The Critical Warnings in the compile are memory-initialisation depth
mismatches in the netlist's inferred memories, the class the DE10 seal shows
too (127004/127005).
