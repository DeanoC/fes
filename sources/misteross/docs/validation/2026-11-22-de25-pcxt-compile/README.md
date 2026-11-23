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

## Result at `cef5ef7e9`

The producer admitted and exported the package (exit 0). The design fits and
every one of the 133 reported timing analyses has non-negative slack
([`golden_top.sta.summary`](golden_top.sta.summary),
[`signoff-timing.json`](signoff-timing.json)). But the DQ capture's hold was
checked 10 ns before the edge the handoff consumes; at that edge it is about
-1.97 ns at the fast corner, so this package does not meet timing there
([below](#the-dq-capture-hold)). The source after `3393cd822` corrects the
check and the phase and has not been compiled. The asynchronous-input and
audio-clock reviews ran and passed. Design Assistant ran 85 rules; the three
that fail (RDC-50003, TMC-20011, TMC-20012) are the reviewed ones, and no
unreviewed rule fails. The producer compiles once: the byte-identical repeat
compile was dropped for every DE25 producer in #2090.

| | |
| --- | --- |
| Build ID | `55ed093fb2a75dad9cc1b75598b719d7` |
| Package | `c9b8a60538fcc37609d4657921e2d966c1db48f35cc66076f7a7116e3c2609f5.fcore` |
| Package SHA-256 | `26ab637e7d28b4d9cc7c5e0be2be406057b62d4f5e326d6f2439fb1cc97e3f40` |
| `core.rbf` SHA-256 | `87a153011906271ee1254e537a60049d53d2ec7c843be908edd7b8e5ff83a6cf` |
| `golden_top.sof` SHA-256 | `273f6dd3b2b9f4ca3307cbdb4408f10cfbe378121105a2e5c68bba627d959b74` |
| Logic (ALMs) | 12,632 / 46,800 (27%) ([`golden_top.fit.summary`](golden_top.fit.summary)) |
| Registers | 16,351 |
| RAM blocks (M20K) | 145 / 358 (41%) |

| Clock | Worst setup slack | Worst hold slack |
| --- | --- | --- |
| Machine, 100 MHz (`outclk0`) | 3.014 ns | 0.012 ns |
| SDRAM pins (`DRAM_CLK`) | 1.944 ns | 0.396 ns |
| DQ capture (`outclk4`, +4.0 ns) | 2.869 ns | 8.028 ns at the nearest edge; about -1.972 ns at the consumed edge |
| CAS-3 handoff (`outclk3`, +9.25 ns) | 4.212 ns | 0.646 ns |
| Pixel, 74.25 MHz | 3.872 ns | 0.015 ns |
| Peripheral, 100 MHz | 5.270 ns | 0.001 ns |
| Audio master (`outclk1`) | 8.788 ns | 0.041 ns |

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
| `7f2c0868f` | kept nodes on the font addresses | fits; DQ capture setup -1.004 ns, hold 12.0 ns at the nearest edge (about 2.0 ns at the consumed edge) |
| `2622f2815` | DQ capture clock phase 0 to +4.0 ns | all reported timing positive, the DQ hold at the nearest edge (about -2.0 ns at the consumed edge); one net-delay bound found no nets |
| `652f21b10` | bank bound by path delay | all 133 analyses pass; signoff: CDC-50002 (5) and LNT-30010 (1) unreviewed |
| `534591a99` | mailbox synchronizers identified | CDC-50002 still 5 |
| `3de378d9d` | `pcxt_video` crossings derived as the ST's are | CDC-50002 down to the two bank bits |
| `02b58c869` | bank copy on its toggle's synchronizer edge; the 8088 cycle divider's enable recognition off | CDC-50002 still the two bank bits; LNT-30010 unchanged in kind |
| `b3c12f2e9` | bank as a preserved code read through a loaded flag, synchronizers power up zero (the DE25 C64's form) | timing still all positive; CDC-50002 cleared; CDC-50003 (1) and CDC-50102 (1, low) on the bank code, LNT-30010 (1) |
| `4eb5b4175` | bank code bounded at 10 ns as the held bundles; its pixel-side copy not identified as a synchronizer; enable recognition off for the whole 8088 bus unit (main merged at `000d60d04`) | signoff passes: all timing positive, no unreviewed rule. The independent repeat compile also passes timing and signoff but its RBF differs (see below) |
| `5f8c9a308` | main merged (#2090: canonical Qsys/qip generation, single signoff compile) | package admitted and exported (build `8c4ed875`) |
| `cef5ef7e9` | main merged (#2126, #2097; shared build-identity and Quartus tool changes move the build ID, no PC/XT RTL change) | package admitted and exported; see Result |
| after `3393cd822` | DQ hold checked at the consumed edge; capture +4.0 to +1.5 ns | not compiled; see below |

## The DQ capture hold

The handoff consumes the capture registers' sample from the second capture
edge after the chip launches a word: 17.75 ns after the launch at +4.0 ns.
The setup multicycle of 2 checks setup there. The hold multicycle of 1
beside it moved the hold check back to the nearest-edge relationship, 10 ns
earlier, at a capture edge whose sample nothing reads, so the next chip
edge's word was never checked against the consumed edge. As in the DE10
[CAS-3 record](../../../../../docs/validation/2026-11-18-pcxt-cas3-capture/README.md),
the hold at the consumed edge is the nearest-edge hold less 10 ns. Setup and
hold there each move one for one with the capture phase, so the two compiles
that report them bound the phase:

| Compile | Capture | Setup | Hold, nearest edge | Hold, consumed edge | Phase window |
| --- | --- | --- | --- | --- | --- |
| `7f2c0868f` | phase 0 | -1.004 ns | 12.0 ns | about 2.0 ns | +1.0 to +2.0 ns |
| `cef5ef7e9` | +4.0 ns | 2.869 ns | 8.028 ns | -1.972 ns | +1.131 to +2.028 ns |

The source after `3393cd822` drops the hold multicycle, so the setup
multicycle alone puts the hold check at the consumed edge, and captures at
+1.5 ns: 15.25 ns after the launch, still in the same read window, 7.75 ns
before the handoff, so the controller's cycle counts are the DE10's. These
compiles predict 0.369 to 0.496 ns of setup and 0.5 to 0.528 ns of hold.
That is a prediction from two fits at other phases, not a result. The
package must be resealed, with the IP generation's achieved phase and every
timing check passing at the consumed edge, and run on a kit again. The
standard-shell simulation's capture clock moves to +1.5 ns with it; its
logical SDRAM holds a read's word from the third chip edge after the command
to the fourth, which contains both phases' consumed edges. The simulation
has not been rerun at +1.5 ns.

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
