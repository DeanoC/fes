# Slice 3: the shell's cost and seed stability (the controlled pair)

The method is in [method.md](method.md), committed (`65f0c0a4b`) before any of
these fits. Revision `bc0a124ba`. Quartus Pro 26.3 on chip-30x0-ai-dev.

**One netlist.** The board build `j` staged the derived machine as
`fes_mailbox/pc386_system.v`, SHA-256 `5b80c0cd…434454d`. The same checkout's
raw Yosys output, `db3074f4…9ab9ac780`, prepares to exactly those bytes. Each
envelope fit took the raw file, prepared it the same way, and records the
fitted SHA-256 as `5b80c0cd…`. So both arms fitted identical machine bytes.

An earlier run of this pair, around build `i`, was discarded before it
finished. Its envelope arm could not take the board's bytes, because the
board's elaboration ran in a random temporary directory and Yosys's names
follow it (DeanoC/yosys#80, path dependence). Re-elaborating gave
different bytes. The machine now elaborates at one fixed path, and a revision
gives the same bytes every time (`bc0a124ba`). The discarded board seeds are
not used here.

## Results

| Seed | Board ALMs | Board RAM | Board DSP | Envelope ALMs | Envelope RAM | Envelope DSP | Virtual-pin ALMs |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 42,582.0 | 295 | 24 | 34,845 | 125 | 23 | 677 |
| 2 | 42,565.9 | 295 | 24 | 34,846 | 125 | 23 | 677 |
| 3 | 42,647.6 | 295 | 24 | 34,779 | 125 | 23 | 677 |
| 4 | 42,549.8 | 295 | 24 | 34,874 | 125 | 23 | 677 |
| **Median** | **42,574.0** | 295 | 24 | **34,845.5** | 125 | 23 | 677 |
| Spread | 97.8 | 0 | 0 | 95 | 0 | 0 | 0 |

Board timing, worst slack across all clocks and corners. Board seed 1 is
build `j` itself:

| Seed | Setup | Hold | Recovery | Removal | Minimum pulse width |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | +1.552 | 0.000 | +7.574 | +0.161 | +0.220 |
| 2 | +1.476 | 0.000 | +6.643 | +0.050 | +0.220 |
| 3 | +1.522 | 0.000 | +6.527 | +0.008 | +0.220 |
| 4 | +1.435 | 0.000 | +5.705 | +0.008 | +0.220 |

## Against the method

- **The shell's cost is 7,728 ALMs** (42,574.0 − 34,845.5). That is
  *measured*: it is far larger than the larger spread, 97.8. Of the device's
  46,800 ALMs, that is 16.5%. The envelope's 677 virtual-pin ALMs are what
  the shell replaces, so against a bare machine the shell is about 8,400.
  That second figure is an estimate: virtual-pin cells are not strictly
  additive.
- **RAM blocks: the shell takes 170** (295 − 125). DSP: 1.
- **Seed stability: pass.** All four board seeds fit, and every analysis is
  non-negative at every corner. Hold is 0.000 with no failing endpoint.
- **Capacity: pass.** Every board seed is at most 46,800.
- **Against the 92% working line (43,056): under** on all four seeds. The
  closest is 42,647.6, 408 ALMs below. This build is identity-off.

## For D2

A sum, uncontrolled because it joins different trees: the identity-on
machine in #1959's envelope was 42,943–43,010 ALMs, with 651 virtual-pin
ALMs. Plus this shell's 7,728 comes to about 50,700, against 46,800. **As
the shell stands, the 387-on DE25 pc386 does not fit.** It is about 3,900
over the device and about 7,600 over the 92% line.

The RAM blocks are as binding: 295 of 358 here, identity-off. The x87 adds
about 7 M20K (#1959: 145 against M's 138).
