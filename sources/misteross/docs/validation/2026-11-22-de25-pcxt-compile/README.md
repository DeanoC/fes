# DE25 standard PC/XT: Quartus compile and producer signoff

2026-11-21/22, Powerboat, Quartus Prime Pro 26.3.0 Build 114. Evidence class:
host-only. Hardware results are posted on the pull request.

Every run is the package producer at the named source, from a fresh clone of
the PR branch:

```sh
python3 boards/de25nano/producers/build_pcxt_package.py \
  --vendor-archive /data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --quartus-root /data/altera_pro/26.3
```

## Result at `5f8c9a308`

The producer admitted and exported the package (exit 0). The design fits and
every one of the 133 reported timing analyses has non-negative slack
([`golden_top.sta.summary`](golden_top.sta.summary),
[`signoff-timing.json`](signoff-timing.json)). The asynchronous-input and
audio-clock reviews ran and passed. Design Assistant ran 85 rules; the three
that fail (RDC-50003, TMC-20011, TMC-20012) are the reviewed ones, and no
unreviewed rule fails. The producer compiles once: the byte-identical repeat
compile was dropped for every DE25 producer in #2090.

| | |
| --- | --- |
| Build ID | `8c4ed8758be6521877fb8d08c34915b9` |
| Package | `95ab2387a014efbc491ccf3c940ca3eedca7992d534ef285f2055dd481a42cfe.fcore` |
| Package SHA-256 | `4a0cd71f0b174486c9e8e43eaff3a33b4d9d05e91d1fee6167587b7d6a40b0ba` |
| `core.rbf` SHA-256 | `76075555e23ff63e10147c95f332d84824c59c8474a206e4a2fb6af5e1842054` |
| `golden_top.sof` SHA-256 | `4e0d6897bcc01d1f92d216a40f4fdfca04f47f97cc7887ebdc1f844a0e1d3eb0` |
| Logic (ALMs) | 12,637 / 46,800 (27%) ([`golden_top.fit.summary`](golden_top.fit.summary)) |
| Registers | 16,360 |
| RAM blocks (M20K) | 145 / 358 (41%) |

| Clock | Worst setup slack | Worst hold slack |
| --- | --- | --- |
| Machine, 100 MHz (`outclk0`) | 2.716 ns | 0.000 ns |
| SDRAM pins (`DRAM_CLK`) | 1.935 ns | 0.355 ns |
| DQ capture (`outclk4`, +4.0 ns) | 2.915 ns | 8.049 ns |
| CAS-3 handoff (`outclk3`, +9.25 ns) | 4.245 ns | 0.648 ns |
| Pixel, 74.25 MHz | 3.913 ns | 0.004 ns |
| Peripheral, 100 MHz | 5.445 ns | 0.000 ns |
| Audio master (`outclk1`) | 8.783 ns | 0.042 ns |

The SDRAM pins are constrained against the ISSI IS42/45VM16320G data sheet at
CAS latency 3 (worst of the -5, -6 and -75 grades: tAC 6.0 ns, tOH 2.1 ns,
setup 2.0 ns, hold 1.0 ns) with zero board flight time, as the Spectrum and ST
constraints assume.

The DOS simulation (`pcxt/sim.py --dos`) at `4eb5b4175`, the last RTL change,
boots FreeDOS to `A:\>` at 2,778,232,801 machine clocks; the largest SDRAM
refresh gap is 712 clocks.

## How it got here

| Source | Change | Result |
| --- | --- | --- |
| `d3f5c6d92` | first compile | SDC not read: a pattern on a new line inside a bracketed `list` |
| `76659e112` | font: one MLAB copy per display card | synthesis: 119,508 ALMs (255%). The single font array with two asynchronous read ports had been 32,804 registers; with MLABs, synthesis absorbed each card's VRAM read register into the font's read address and built one VRAM copy from 131,000 registers |
| `7f2c0868f` | kept nodes on the font addresses | fits; DQ capture setup -1.004 ns with 12.0 ns of hold slack |
| `2622f2815` | DQ capture clock phase 0 to +4.0 ns | all timing positive; one net-delay bound found no nets |
| `652f21b10` | bank bound by path delay | all 133 analyses pass; signoff: CDC-50002 (5) and LNT-30010 (1) unreviewed |
| `534591a99` | mailbox synchronizers identified | CDC-50002 still 5 |
| `3de378d9d` | `pcxt_video` crossings derived as the ST's are | CDC-50002 down to the two bank bits |
| `02b58c869` | bank copy on its toggle's synchronizer edge; the 8088 cycle divider's enable recognition off | CDC-50002 still the two bank bits; LNT-30010 unchanged in kind |
| `b3c12f2e9` | bank as a preserved code read through a loaded flag, synchronizers power up zero (the DE25 C64's form) | timing still all positive; CDC-50002 cleared; CDC-50003 (1) and CDC-50102 (1, low) on the bank code, LNT-30010 (1) |
| `4eb5b4175` | bank code bounded at 10 ns as the held bundles; its pixel-side copy not identified as a synchronizer; enable recognition off for the whole 8088 bus unit (main merged at `000d60d04`) | signoff passes: all timing positive, no unreviewed rule. The independent repeat compile also passes timing and signoff but its RBF differs (see below) |
| `5f8c9a308` | main merged (#2090: canonical Qsys/qip generation, single signoff compile) | package admitted and exported; see Result |

The capture phase is a board choice: this board returns the word later than
the DE10's. At +4.0 ns the capture still samples in the same read window,
5.25 ns before the handoff, so the controller's cycle counts are the DE10's.

## The Spectrum and ST are unchanged

The generalised machine builder stages the Spectrum and Atari ST projects as
before. Prepared at `b9dd1ff47` and at this branch, `golden_top.v`,
`golden_top.qsf`, `machine_pll.ip`, `machine-clocks.sdc` and the staged
source digests are byte-identical for both; only output paths and the BUILD_ID
(which hashes `machine/board.py`) differ.

## Design Assistant rules traced

At `4eb5b4175` the signoff compile passed the package gate. Each of the three
earlier rules was traced on the compiled netlist by rerunning timing analysis
and Design Assistant at finalize on a copy of the `b3c12f2e9` project:

- CDC-50003: Design Assistant counted the 13.4 ns bank-code skew bound as no
  skew constraint; the 10 ns bound the other held bundles use clears it.
- CDC-50102: Quartus identified `pixel_code`, a load on the synchronized
  toggle whose source is held until the acknowledgement returns, as a
  synchronizer. Its synchronizer identification is now off; CDC-50003 still
  checks the transfer's constraints.
- LNT-30010: the reset register's direct net reaches the 8253, 8259 and 8255
  asynchronous clears, three ISA boundary inputs and exactly 248 clock
  enables, all in the 8088 bus unit (registers its reset branch leaves out).
  Enable recognition is off for the bus unit.

The synthesis report has six 16746 warnings and no 16788: two in the DE10
`pcxt_system.sv` (`disk_storage_ready`/`valid` used on line 72, declared as
plain 1-bit wires on lines 97-98, so the implicit net is the same wire) and
four in Qsys's generated `peripheral_subsys.v`, shared by every DE25 machine.

At `4eb5b4175` the then-required repeat compile produced a different RBF. The
cause was in the shared DE25 shell generation (a Qsys `ADDRESS_MAP` slave-name
form and EMIF `.qip` line order chosen at random, then fitter dependence on
the project path); #2090 made the Qsys/qip generation canonical and dropped
the repeat gate for every DE25 producer.
