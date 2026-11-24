# DE25 pc386 486SX: how fast it clocks, 24 November 2026

Deano, 24 Nov 2026: "lets have a session see what 486sx on de25 can clock to".
Refs [#2135](https://github.com/DeanoC/fes/issues/2135) (the DX plan; its
decision 4 asks for a PLL check of a 2× integer core). This is a measurement
study: no RTL changes, and the registered producer still ships 61.437908 MHz.
The method, [method.md](method.md), was committed and pushed (`c1ca3e31c`)
before any fit, and amended once (`e0f871976`) before any rung was fitted.

## Answers

1. **The full DE25 486SX machine closes every analysis on all four seeds at
   111.904762 MHz** (C0 = 28), the ladder's top rung, with the real board
   shell: window, arbiter, bridge, scanout, audio and the HPS. Every rung's
   seed 1 passed (74.60, 84.68, 92.16, 97.92, 104.44, 111.90 MHz), and seeds
   2–4 at 111.90 MHz pass too. 111.90 MHz is the top rung, so it is **a floor
   on the ceiling, not the ceiling**; the method adds no rung above it. The
   timing analyser's same-clock Fmax estimate for the machine clock at that
   rung is 125.28 MHz (seed 1). The
   four seeds at 111.90 MHz: worst setup +0.954, +1.120, +0.893 and
   +0.846 ns, all on machine-clock paths; hold never below 0.000; ALMs
   41,689.1, 41,728.7, 41,667.6 and 41,664.0 (median 41,678.4, against the
   control's 41,686.9 at 61.44 MHz). The Fmax estimates of those four fits are
   123.61–127.94 MHz.
2. **No rung failed, so no failing limit was measured.** What is tightest at
   every rung, in order: the **CPU's own paths** (CPU→CPU, worst +0.958 ns at
   111.90 MHz, seed 1), then the **shell's window port head → arbiter
   `writes_owed`** (+1.034 ns), then the machine's devices (the ATA pointer →
   AT decoder read data, +1.439 ns). The limit is the CPU, with the shell's
   window→arbiter path close behind it. The fixed-bound crossings (the
   10 ns `set_max_delay` held bundles, +1.27 to +1.60 ns at every rung,
   including the worst setup in #2171) do not move with the machine clock.
3. **A 2× CPU clock is not the next step.** The CPU alone closes 120.513 MHz
   on four seeds and 125.333 MHz on two of four; the CPU with the whole
   machine around it closes 125.333 MHz on four seeds (virtual-pin
   envelopes). So a CPU clock of 2 × 61.44 would be at the CPU's own limit
   and buy about 8–12% over the single 111.90 MHz clock the board already
   closes, at the cost of a clock-crossing CPU interface. The paths that
   would cross (CPU↔machine and CPU↔shell, about 250–400 endpoints in each
   rung's report) are not the problem: their slack projected to a 7.979 ns
   fast period is positive at every rung from 74.60 MHz up. What it needs is
   RTL, not timing: the CPU's bus and I/O handshakes with `pc386_machine`
   assume one clock, and a 2:1 design needs either a clock-enable on the
   machine side or a registered 2:1 boundary there. Not built.
4. **Legal clocks.** The machine PLL keeps its VCO (50 MHz × 188 / 3 =
   3,133.333 MHz) and the audio master (VCO / 255 = 12.287582 MHz, C1 = 255
   at every rung, from the fit reports), so the machine runs at **VCO / C0
   for any integer C0**. AUDIO's N/D becomes C0 / 65280 (1/1280 only at
   C0 = 51). Every guest rate stays an exact rational under 2^31. The
   machine↔audio crossing is bounded by absolute constraints: its setup row
   sits at +8.7 to +8.8 ns at every rung, integer ratio or not. Nothing else
   fixes the ratio. Near each step (MHz): 61.44 (C0 51), 62.67 (50), 63.95
   (49); 72.87 (43), **74.60 (42)**, 76.42 (41); 82.46 (38), **84.68 (37)**,
   87.04 (36); 89.52 (35), **92.16 (34)**, 94.95 (33); **97.92 (32)**,
   101.08 (31); **104.44 (30)**, 108.05 (29); **111.90 (28)**, 116.05 (27),
   120.51 (26), 125.33 (25). A 2:1 pair needs an even C0: 62.67/125.33
   (50/25) or 60.26/120.51 (52/26); 2 × 61.437908 is not on this VCO.

**Recommendation: raise the single machine clock; no separate CPU clock and
no RTL fix yet.** The board closes 111.904762 MHz
(C0 = 28) on four seeds with the CPU as the limit, and a separate CPU clock
would buy little over that for a new crossing. The next speed step is a PR
that moves the DE25 pc386 machine clock to a chosen C0, Deano's choice of
how much margin: 111.90 MHz is measured on four seeds here (worst +0.846 ns);
104.44 MHz (C0 = 30) has one seed (+1.279 ns). That PR needs, beyond the
four lines `make_rung.py` edits:

- AUDIO's ratio written as C0 / 65280 rather than the literal 1/1280, and the
  PLL comment;
- the shared FIFO reset's reviewed `PARENT_SHA256` reviewed again at the new
  clock (the producer refuses until then; see below);
- the producer's own gates at the new clock: package timing admission and the
  reviewed signoff findings, which this study did not run;
- the DE25 system bench and Kit D25 HIL, including the window's DDR
  bandwidth and the governor at a machine clock 1.8× faster.

To find the ceiling rather than a floor, the same method one ladder higher
(116.05, 120.51, 125.33 MHz) is the measurement; the board's Fmax estimates
(123.6–127.9 MHz at 111.90) and the machine envelope (125.33 MHz on four
seeds) put it somewhere there. Past that, the CPU's own decode and L1I paths
(`cpu_decode_by` → the decode stage `cpu_dc`, `cpu_l1i_blk` →
`cpu_l1i_valid`) and the window's `port2|head` → `arbiter|writes_owed` are
the paths to shorten first.

## The producer cannot build any clock but 61.44 MHz

At every rung the registered producer, `build_pc386_package.py`, stopped
before the fitter:

```text
de25-pc386: shared FIFO reset: unreviewed generated input: peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v
```

`boards/de25nano/scanout/shared_resets.py` checks each generated Qsys file
against a reviewed digest; for pc386 the parent's is `PARENT_SHA256` in
`boards/de25nano/pc386/build.py`. Qsys names the window's machine-clock
interconnect module with a hash of its parameters, which include
`EXPLICIT_CLOCK_RATE` (`…mm_interconnect_1920_xi5gepi` at 61.44 MHz,
`…_ruphjby` at 74.60 MHz). The module's body is otherwise byte-identical, and
over every `.v`, `.sv`, `.sdc` and `.vh` of the generated project, the
C0 = 42 project and the control differ only in that name, the PLL IP, the
netlist, the `BUILD_ID` constant and the files the shared-reset derivation
rewrites. Shipping a new clock therefore needs that digest reviewed again at
the new clock. Nothing was changed to get past it here. Amendment 1 of the
method fitted every rung through the control's generated project with only
the rung's PLL IP and netlist swapped in ([`compile_rung.sh`](compile_rung.sh);
each seed's `diff -rq` against the control is the PLL files, the two netlist
copies and, for seeds 2–4, `golden_top.qsf`). So for the rungs **the
producer's post-fit gates did not run**: no package timing admission and no
reviewed-signoff-findings verdict is claimed above 61.44 MHz.

## The fits

Powerboat, Quartus Pro 26.3, each under a Pro slot of the Quartus lease.
Control: the producer at the method commit, admitted (package
`e8d37ea9…`, netlist `01185e2f…`). Rungs: [`fitrung.sh`](fitrung.sh) generated
each rung's project from its pushed scratch branch, and
[`compile_rung.sh`](compile_rung.sh) compiled it. Every seed's figures are in
[`fits/`](fits) (`rung_summary.py` output: every row of the timing summary by
analysis, clock and corner; ALMs; the PLL as fitted; the path classification);
[`results.py`](results.py) prints these tables from them.

| Rung | C0 | Commit | `make_rung.py` diff SHA-256 | Netlist SHA-256 |
| --- | ---: | --- | --- | --- |
| control | 51 | `c1ca3e31c` (method) | — | `01185e2f…` |
| 1 | 42 | `b63615a7a` | `af2a509a…` | `71873859…` |
| 2 | 37 | `0e1158447` | `fff81c32…` | `0da90f6e…` |
| 3 | 34 | `71b2f18bf` | `bbcd7e1f…` | `3ef0858c…` |
| 4 | 32 | `5e5c8f39b` | `49be2d5e…` | `ffb9ffb4…` |
| 5 | 30 | `7df48e2ac` | `e02559f6…` | `5090f243…` |
| 6 | 28 | `8e9e7268c` | `14596498…` | `0be022e0…` |

The rung branches are `deano/de25-486sx-fmax-cNN`; they are scratch and never
merge.

| Rung | MHz | Seed | ALMs | Machine→machine setup | Machine clock setup row | Machine clock hold row | Setup | Hold | Recovery | Removal | Minimum Pulse Width | Max Skew | Net Delay | Setup Data Delay | Fmax est. | Pass |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| control | 61.438 | 1 | 41,686.9 | +4.290 | +1.604 | +0.000 | +1.604 | +0.000 | +7.018 | +0.063 | +0.220 | +6.892 | +3.426 | +3.426 | 83.43 MHz | pass |
| c42 | 74.603 | 1 | 41,644.5 | +2.956 | +1.688 | +0.000 | +1.452 | +0.000 | +6.608 | +0.001 | +0.220 | +6.953 | +3.501 | +3.501 | 95.71 MHz | pass |
| c37 | 84.685 | 1 | 41,648.2 | +2.231 | +1.745 | +0.001 | +1.272 | +0.000 | +7.196 | +0.011 | +0.220 | +7.017 | +3.564 | +3.564 | 104.42 MHz | pass |
| c34 | 92.157 | 1 | 41,650.7 | +2.011 | +1.585 | +0.000 | +1.560 | +0.000 | +7.347 | +0.086 | +0.220 | +6.923 | +3.405 | +3.405 | 113.12 MHz | pass |
| c32 | 97.917 | 1 | 41,651.6 | +1.512 | +1.512 | +0.002 | +1.512 | +0.000 | +7.357 | +0.069 | +0.220 | +7.189 | +3.704 | +3.704 | 114.94 MHz | pass |
| c30 | 104.444 | 1 | 41,613.4 | +1.279 | +1.279 | +0.000 | +1.279 | +0.000 | +7.022 | +0.066 | +0.220 | +7.048 | +3.604 | +3.604 | 120.55 MHz | pass |
| c28 | 111.905 | 1 | 41,689.1 | +0.954 | +0.954 | +0.000 | +0.954 | +0.000 | +6.538 | +0.034 | +0.220 | +6.721 | +3.337 | +3.337 | 125.28 MHz | pass |
| c28 | 111.905 | 2 | 41,728.7 | +1.120 | +1.120 | +0.002 | +1.120 | +0.000 | +6.576 | +0.013 | +0.220 | +6.540 | +3.561 | +3.561 | 127.94 MHz | pass |
| c28 | 111.905 | 3 | 41,667.6 | +0.893 | +0.893 | +0.003 | +0.893 | +0.000 | +6.324 | +0.028 | +0.220 | +6.719 | +3.523 | +3.523 | 124.33 MHz | pass |
| c28 | 111.905 | 4 | 41,664.0 | +0.846 | +0.846 | +0.000 | +0.846 | +0.000 | +6.505 | +0.001 | +0.220 | +6.702 | +3.620 | +3.620 | 123.61 MHz | pass |

| Rung | Seed | Owner pair (resolved): worst machine-to-machine setup slack, Slow fix4 0C (endpoints reported) |
| --- | ---: | --- |
| control | 1 | cpu->cpu +4.290 (14235); shell->shell +6.323 (703); machine->machine +6.835 (3544); machine->anonymous +7.483 (365); anonymous->shell +7.888 (143) |
| c42 | 1 | cpu->cpu +2.956 (13967); shell->shell +4.181 (701); machine->machine +4.693 (3851); machine->anonymous +5.542 (371); cpu->machine +5.661 (210) |
| c37 | 1 | cpu->cpu +2.231 (13906); shell->shell +3.017 (742); machine->machine +3.199 (3836); machine->anonymous +4.297 (340); cpu->machine +4.298 (205) |
| c34 | 1 | cpu->cpu +2.014 (13611); shell->shell +2.549 (702); machine->machine +2.812 (3910); machine->anonymous +3.720 (384); cpu->machine +3.746 (160) |
| c32 | 1 | cpu->cpu +1.512 (13724); shell->shell +1.866 (716); machine->machine +2.324 (3891); cpu->machine +2.868 (178); machine->anonymous +2.888 (425) |
| c30 | 1 | cpu->cpu +1.279 (13475); shell->shell +1.639 (710); machine->machine +2.024 (4137); cpu->machine +2.795 (141); anonymous->anonymous +2.863 (284) |
| c28 | 1 | cpu->cpu +0.958 (13721); shell->shell +1.034 (709); machine->machine +1.439 (3842); machine->anonymous +1.693 (398); cpu->machine +2.244 (179) |
| c28 | 2 | cpu->cpu +1.120 (13578); shell->shell +1.273 (800); machine->machine +1.666 (4059); cpu->anonymous +2.132 (13); anonymous->anonymous +2.148 (575) |
| c28 | 3 | cpu->cpu +0.893 (13973); shell->shell +1.104 (855); machine->machine +1.798 (3686); cpu->machine +1.933 (158); cpu->anonymous +2.048 (4) |
| c28 | 4 | cpu->cpu +0.846 (14135); shell->shell +1.326 (713); cpu->machine +1.838 (140); machine->machine +1.865 (3578); anonymous->anonymous +2.226 (198) |

"Machine→machine setup" is the worst period-scaled path (launch and latch the
machine clock) from `paths.tcl` over every corner. The machine clock's summary
rows also hold the fixed-bound crossings it latches, so at 61.44–92.16 MHz
the summary's machine setup row is a 10 ns bundle, not a machine path. Hold
is 0.000 on the machine clock at the fast corner at every rung, never
negative. The worst removal is always `adv7513_config` (the HDMI
transmitter's I2C configuration clock, +0.001 to +0.086 ns), which no rung
changes.

**Ownership.** Path ends are owned by the CPU (`system|cpu_*`), the machine
(`system|machine_*`, `pixel_*`, `frame_*`), the shell (outside `system|`) or
are anonymous (`system|_N_`). Many anonymous registers are registers Yosys
merged, followed in the netlist by `assign {cpu_dc[…], …} = _35174_;`
(`_35174_` is the CPU's decode stage). `rung_summary.py`'s `resolver` gives
such a register to the CPU or the machine when every name it drives belongs
to one of them. That resolution was added after the method and is labelled
"resolved" wherever it is used; the unresolved counts are in each JSON's
`machine_pairs`.

## Q3 in numbers

| Envelope fit (`fit_i386_device.py`, chip-30x0-ai-dev) | Clock | Seeds passing | Worst setup per seed (ns) | ALMs |
| --- | ---: | ---: | --- | ---: |
| `fes_i386_cpu`, I486 = 1 | 125.333 MHz | 2 of 4 | +0.006, −0.189, −0.353, +0.019 | 35,004–35,071 |
| `fes_i386_cpu`, I486 = 1 | 120.513 MHz | 4 of 4 | +0.009, +0.031, +0.020, +0.054 | 35,023–35,090 |
| `pc386_system`, I486 = 1 | 125.333 MHz | 4 of 4 | +0.365, +0.789, +0.495, +0.732 | 35,096–35,157 |

Both netlists were elaborated on Powerboat by the pc386 lock's Yosys (HIP
slot) from the method commit with [`envelope-i486.patch`](envelope-i486.patch)
applied (`fes_i386_cpu.netlist.v` `dc4eb486…`, `pc386_system.v` `cc36f031…`,
both with a 22-bit `eflags`), and fitted on chip with `--netlist`, because
chip has no install of that Yosys. The CPU alone carries 2,352 ALMs of virtual
pins, which is why it is no smaller than the machine. Each fit's `fit.json`,
`inputs.json`, `netlist.json` and `timing.json` are in
[`fits/envelope/`](fits/envelope).

From the board fits (resolved owners, Slow fix4 0C, seed 1), the machine-clock
paths with exactly one end in the CPU, and their slack if the relationship
were cut from the machine period to 7.979 ns with everything else equal:

| Rung | MHz | CPU-boundary endpoints reported | Worst boundary slack at 7.979 ns | Boundary endpoints negative | Worst CPU-internal slack at 7.979 ns | CPU-internal endpoints negative |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| control | 61.438 | 388 | −0.260 | 39 | −4.007 | 4181 |
| c42 | 74.603 | 392 | +0.236 | 0 | −2.469 | 2877 |
| c37 | 84.685 | 255 | +0.469 | 0 | −1.598 | 2413 |
| c34 | 92.157 | 360 | +0.874 | 0 | −0.858 | 1869 |
| c32 | 97.917 | 386 | +0.635 | 0 | −0.721 | 943 |
| c30 | 104.444 | 393 | +1.200 | 0 | −0.316 | 55 |
| c28 | 111.905 | 398 | +1.287 | 0 | +0.001 | 0 |

The worst boundary path at every rung is the CPU's bus interface head
(`cpu_biu_head`, `cpu_biu_e_line`) into the EMS board's memory read data
(`machine_ems_mem_rdata`, `machine_ems_p2_write`). The counts are of the
20,000 worst machine-clock endpoints `paths.tcl` reports, so "negative" is a
lower bound when the window is full. The projection keeps each path's
placement at its rung: a fitter asked for the fast clock would place
differently. What it shows is that the crossing is not where a 2× clock would
fail first: the CPU's internal paths are, which the CPU-alone envelope
(two of four seeds at 125.33 MHz) agrees with.

## What this does not establish

- **Admission above 61.44 MHz.** No package was built at any rung; the
  producer's timing admission and signoff findings review did not run.
- **The ceiling.** 111.90 MHz is the top rung that was fitted.
- **Function at a raised clock.** No simulation or kit run at any rung. The
  guest rates are exact at every rung by construction, but the window's
  DDR bandwidth per machine clock, the governor's headroom and the scanout's
  frame pacing at a faster machine are not examined here.
- **A clock-crossing CPU.** Not built (question 3 asked for timing evidence
  only).
