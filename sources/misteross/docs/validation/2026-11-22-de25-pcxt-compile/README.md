# DE25 standard PC/XT: Quartus compile and producer signoff

2026-11-21/22, Powerboat, Quartus Prime Pro 26.3.0 Build 114. Evidence class:
host-only. Nothing was programmed; no package was exported.

Every run is the package producer at the named source, from a shallow clone of
the PR branch:

```sh
python3 boards/de25nano/producers/build_pcxt_package.py \
  --vendor-archive /data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --quartus-root /data/altera_pro/26.3
```

## Result at `02b58c869`

The design fits and every one of the 133 reported timing analyses has
non-negative slack ([`golden_top.sta.summary`](golden_top.sta.summary),
[`signoff-timing.json`](signoff-timing.json)). The asynchronous-input and
audio-clock reviews ran and passed. The producer then refused to package: two
Design Assistant rules fail with no reviewed basis (see Open). The candidate
was retained and no repeat compile ran.

| | |
| --- | --- |
| Build ID | `5e46ffb0281966902fd0b419bdbd29bd` |
| `linux-boot.core.rbf` SHA-256 | `0e022bcdaaf63e37c88800e9d7866bb3987d0a2ff07e68781504ad85a8e69656` |
| `golden_top.sof` SHA-256 | `8e652c005ef4086c25cd1896a1f1a930e5c1c0059bf0838f5229409edf504535` |
| Logic (ALMs) | 12,478 / 46,800 (27%) |
| Registers | 16,279 |
| RAM blocks (M20K) | 145 / 358 (41%) |
| PLLs | 4 / 11 |

| Clock | Worst setup slack | Worst hold slack |
| --- | --- | --- |
| Machine, 100 MHz (`outclk0`) | 3.043 ns | 0.024 ns |
| SDRAM pins (`DRAM_CLK`) | 1.821 ns | 0.264 ns |
| DQ capture (`outclk4`, +4.0 ns) | 2.874 ns | 8.092 ns |
| CAS-3 handoff (`outclk3`, +9.25 ns) | 4.217 ns | 0.626 ns |
| Pixel, 74.25 MHz | 3.844 ns | 0.001 ns |
| Peripheral, 100 MHz | 5.869 ns | 0.000 ns |
| Audio master (`outclk1`) | 8.757 ns | 0.052 ns |

The SDRAM pins are constrained against the ISSI IS42/45VM16320G data sheet at
CAS latency 3 (worst of the -5, -6 and -75 grades: tAC 6.0 ns, tOH 2.1 ns,
setup 2.0 ns, hold 1.0 ns) with zero board flight time, as the Spectrum and ST
constraints assume.

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

The capture phase is a board choice: this board returns the word later than
the DE10's. At +4.0 ns the capture still samples in the same read window,
5.25 ns before the handoff, so the controller's cycle counts are the DE10's.

## The Spectrum and ST are unchanged

The generalised machine builder stages the Spectrum and Atari ST projects as
before. Prepared at `b9dd1ff47` and at this branch, `golden_top.v`,
`golden_top.qsf`, `machine_pll.ip`, `machine-clocks.sdc` and the staged
source digests are byte-identical for both; only output paths and the BUILD_ID
(which hashes `machine/board.py`) differ.

## Open

At `b3c12f2e9` three unreviewed rules remain:

- **CDC-50003, one violation:** the two-bit bank code `published_code` into
  `pixel_code`, recognised as a CE-type bus loaded on `sent_held`. The
  report shows its net and data delay constrained and its max skew
  unconstrained, although `set_max_skew` names the same registers and the ST's
  bundles pass with the same form.
- **CDC-50102, one violation (low):** a two-stage synchronizer after that bus.
  The ZX81 static profile reviews one CDC-50102; the standard-computer gate
  does not.
- **LNT-30010, one violation (low):** `pcxt_system`'s `machine_reset` drives
  the vendored 8253's asynchronous clear (693 asynchronous and 1,351
  synchronous clears in all) and, through synthesis, 248 clock enables across
  the machine, in unchanged DE10 RTL. Enable recognition off on one register
  only moved the example. Candidates: a reset register of their own for the
  vendored 8253, 8259 and 8255 (loaded from the same logic on the same clock,
  as the ST's six reset groups), with enable recognition off where the reset
  still reaches enables, as the DE25 C64 does for its SID; or a reviewed
  finding, which would loosen the gate and is Deano's call.
- No repeat compile, no package, no hardware.
