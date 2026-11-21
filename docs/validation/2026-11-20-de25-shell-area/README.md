# DE25 pc386 shell and AT-device area study: results

This study measures only. It changes no RTL and opens no RTL-change PR. Building any cut is a later decision for the coordinator.

The method, with its amendments, is in [method.md](method.md), committed before each set of fits. The ablation patches are in [patches/](patches/). Per-run figures are in [collect.json](collect.json), produced by [area386_collect.py](area386_collect.py) on chip-30x0-ai-dev. All fits use Quartus Prime Pro 26.3 on the Agilex 5 A5EB013BB23BE4SCS (46,800 ALMs, 358 RAM blocks).

## What the results feed

- Deano decided that the first DE25 pc386 shell ships with the 387 off.
- This table feeds the later step that turns the 387 on. That step needs about **3,500 ALMs** beyond today's board: about 50,300 with the 387 on, against 46,800 available. #2059 later measured it more closely at 49,955.5 with its x87 saving; the [conclusion](#conclusion) uses that figure.
- It also needs RAM. Today's 387-off board uses 295 of the 358 RAM blocks.

## Conclusion

The study is closed. Deano decided not to measure S-rsp2 (see [below](#why-s-rsp2-is-not-measured)).

- **Measured cuts:** about **2,220 ALMs and 128 RAM blocks**: S-ocm 696, S-lat 946, A-dma2 412 and A-fifo 170. That is a sum of separate ablations, each against its own baseline. They were never fitted together.
- **The 387-on DE25 board stays over.** #2059 puts the 387-on board, with the measured 7,728-ALM shell and its own 460.5 ALM x87 saving, at 49,955.5 ALMs. Taking this study's 2,220 off leaves about 47,735:
  - about **1,000 ALMs over the device** (46,800);
  - about **4,700 ALMs over the 92% line** (43,056).
- **Shell cuts alone do not reach 387-on.** The bigger levers are machine devices outside this study's scope. Their sizes are entity rows from #2037's fit, not ablation bounds:
  - VGA, about 1,840 ALMs;
  - audio, about 1,333 ALMs, of which OPL2 is 881.
- **Boards:**
  - S-ocm and S-lat are DE25-only. They are settings in the DE25 managed shell's Qsys system, which the DE10 build does not have. They apply to every DE25 core.
  - A-dma2 and A-fifo change AT device RTL shared with the DE10. A-dma2 drops a feature, so it needs a per-board parameter that leaves the DE10 unchanged. A-fifo keeps behaviour, but it moves the DE10's BUILD_ID unless a parameter keeps the register form there.
- **Hardware:** none of the cuts has run on hardware. Every figure comes from fits.

## The shell's cost (reference)

**The controlled pair:**
- Source: #2036 `docs/validation/2026-11-20-de25-pc386-shell-s3/pair.md`, on the slice-3 branch (`abb9e4994`).
- Build `j`: the board median is 42,574.0 ALMs (spread 97.8). The virtual-pin envelope median is 34,845.5 (spread 95; 677 of its ALMs are virtual pins).
- The shell therefore costs **7,728 ALMs, 170 RAM blocks and 1 DSP**. Against a bare machine with no virtual pins, that is about 8,400.

**The entity split, for ranking only:**
- The whole design minus the board's `system` entity gives a median of about 8,717.
- The split is about 1,000 larger than the pair because the machine packs about 300 ALMs smaller inside the board than inside the envelope, and the envelope's virtual pins are counted on the other side.
- The pair is the cost. The split only ranks where the shell's ALMs go.

**Where the shell's ALMs go** (build `j`, seed 1, ALMs needed):

| Entity | ALMs | RAM | Note |
|---|---:|---:|---|
| `mm_interconnect_0` (F2SDRAM agent, two masters) | 2,065 | 0 | About 770 on the stock Spectrum reference, which has one master. The pipeline stages total 1,176. The agent's `read_rsp_fifo` is 350 and its `write_rsp_fifo` 74. |
| `subsys_periph/mm_interconnect_1` | 979 | 0 | The `fes_mem_cdc` agent response FIFO alone is 957 ALMs: 193 deep × 274 bits, in registers. |
| `subsys_hps` (vendor HPS adapters) | 953 | 0 | About 324 of this is the `hps2fpga` adapter, which S-ocm removes. |
| `ocm` (vendor on-chip memory) | 553 | 128 | Removed by #1956. |
| `mm_interconnect_2` (lightweight bridge) | 477 | 0 | |
| `subsys_periph/mm_interconnect_2` | 148 | 0 | |

**Why the shell came out about 300 above the estimate:**
- Spike B2 to build `i` adds about 318.
- `window_control`: +132.
- Control and memory CDC FIFOs: +131.
- `sm_mpfe`: +59.
- Window: +29.
- Arbiter: −68.

**The AT arm's hierarchy-kept fit agrees with the flat fit.** #2037's hierarchy-kept total is within +1.0% of its flat fit, inside the ±5% check, so the per-entity ranking of the AT devices holds.

## Ranked table

Each bound is the baseline median minus the ablation median, both over four seeds, using whole-design `ALMs needed`.
- A bound counts as measured only when it exceeds the larger of the two spreads.
- Bounds are upper bounds: an ablation removes the logic outright.
- Area bands are one-sided.

Baselines:
- **Shell arm (387 off):** median 42,574.0, spread 97.8, RAM 295, DSP 24.
- **AT arm (387 on, envelope):** median 42,576.5, spread 34, RAM 145, DSP 42.

| # | Candidate | ALM bound (ablation spread) | RAM | DSP | Control | Status | Cost | Boards affected |
|---|---|---:|---:|---:|---|---|---|---|
| 1 | **S-ocm**: drop the vendor OCM; `H2F_Width` 0 (#1956) | **696** (49) | **−128** | 0 | Pass: the `ocm` and `hps2fpga_axi4_rl_adp_inst` rows are gone | Measured | No cycles. The full HPS-to-FPGA bridge is removed; nothing in pc386 uses it. #1956 is an open draft, and the case where the HPS-to-FPGA bridge is unused has not run on hardware. | Every DE25 core on the managed shell |
| 2 | **S-lat**: `qsys_top` `maxAdditionalLatency` 4 → 0 | **946** (58.9) | 0 | 0 | Pass: generation gate (`qsys_top` F2SDRAM interconnect has 0 pipeline instances against `j`'s 16); fit: `mm_interconnect_0` 2,065 → 1,272 ALMs, its pipeline rows 1,176 → 0, registers 5,140 → 1,264 | Measured | No cycles in RTL; the Qsys interconnect adds no optional pipeline stages, so F2SDRAM command and response paths lose their extra register stages (lower latency, not higher). Timing passes on all seeds: worst setup +1.294 against base +1.435, on the video pixel clock (`iopll_0_outclk0`), not the F2SDRAM path. Not run on hardware. | Every DE25 core on the managed shell |
| 3 | **A-dma2**: DMA controller 2 removed (reads `FFh`, no hold) | **412** (172) | 0 | 0 | Pass: netlist `dma2` references 326 → 3 (the select wire and its `FFh` mux); registers −365 | Measured | No cycles: controller 2 moves no data, since its DRQs are tied to 0. The audit puts a realistic register-file-only version at about 250–280. That version keeps readback; channels 5–7 software requests would stop counting down and setting TC. No title or test depends on that (`devices_tb.cpp:432-434` checks readback only). | AT device (DE10 too). Can be gated: a new `fes_dma8237` parameter, whose default keeps today's PC/XT and DE10 behaviour |
| 4 | **A-fifo**: PS/2 FIFOs (keyboard and mouse) given one write port and `ramstyle = "MLAB"` | **170** (69) | 0 | 0 | Pass: MLAB bits 1,421 → 1,853; registers −339 | Measured | None: behaviour and cycles are unchanged. | AT device. Same RTL on DE10 would move DE10's BUILD_ID (its Yosys policy puts multi-port memories in registers). Keeping DE10's netlist needs a parameter that selects the form |
| 5 | **S-rsp2**: `responseFifoType` `EMBEDDED_MEMORY_BASED`, applied after integration | ≤ 957 (estimate: entity row, not an ablation) | about +3 to +7 (est.) | — | Generation gate passed: `USE_MEMORY_BLOCKS (1)` | **Not measured.** The shared-reset guard refuses the regenerated file, and Deano chose not to extend it (see below) | Trades ALMs for M20K, and RAM is also short with the 387 on | Every DE25 core on the managed shell |
| 6 | **A-kbcram**: 8042 RAM bytes 1–31 dropped | 98.5 (126) | 0 | 0 | No precommitted control. Registers −138 | Within the spread; not measured | Commands 21h–3Fh and 61h–7Fh would read 0. No title or test uses them. | AT device; can be gated |
| 7 | **A-pitbcd**: KF8253 BCD counting dropped | −1 (114) | 0 | 0 | Pass: `dec_bcd_q` 12 → 0; registers −159 | No saving: the logic packs into ALMs already in use | Breaks `devices_tb.cpp:335-367`; vendor patch | — |
| — | **S-rsp v1** | — | — | — | **Failed**: generated interconnect identical to `j` | Not measured (amendment) | | |

Timing: every measured arm passes on every seed.
- Shell arm, worst over seeds:
  - Baseline: setup +1.435, hold 0.000, removal +0.008, minimum pulse width +0.220.
  - S-ocm: setup +1.408, removal +0.034, minimum pulse width +0.220.
  - S-lat: setup +1.294, hold 0.000, removal +0.005, minimum pulse width +0.220.
- AT arm: all fits report status "pass".
  - Worst setup over seeds: baseline +7.613, dma2 +7.390, fifo +7.574, kbcram +7.458, pitbcd +7.321.
  - Worst hold: +0.013 or better throughout.

**Measured: 696 + 946 + 412 + 170 = about 2,220 ALMs and 128 RAM blocks.** That is a sum of separate ablations, each against its own baseline; no combined fit was run. S-ocm and S-lat touch different interconnects (`hps2fpga` and F2SDRAM), but their sum is not measured. The [conclusion](#conclusion) sets this against the 387-on board.

S-rsp2's ≤ 957 is an estimate from the entity row, it would spend RAM, and it is not measured. The second-F2SDRAM-master bound below was taken against the pipelined interconnect; with S-lat, `mm_interconnect_0` is 1,272 ALMs, so what merging the masters would still save on top of S-lat is smaller and unmeasured. The shell and AT cuts in scope here do not close the 387-on gap on measured evidence.

### Why S-rsp2 is not measured

- The generation gate passed: the regenerated `fes_mem_cdc` response FIFO has `USE_MEMORY_BLOCKS (1)`.
- The board build then exited 1. After generation, `scanout/shared_resets.py:154` adds the shared FIFO reset wrapper to `peripheral_subsys.v` only if the generated file matches a reviewed digest. The knob renames four generated interconnect modules in that file (`mm_interconnect_0`, `_1`, `_3`, `_4`), so the digest no longer matches, and the build refuses with `unreviewed generated input: peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v`.
- The four seeds compiled the unwrapped project anyway, a flaw in the study's run script. Each failed in the fitter with `FIFO peer reset ingress changed` (Error 332000). No figures came from them.
- Getting past this means reviewing the new generated file and adding its digest to the guard. Deano chose not to extend the guard and closed the study, so S-rsp2 stays unmeasured.

## Entity bounds only (not ablated)

| Candidate | Entity bound | Why not ablated | Board |
|---|---:|---|---|
| The second F2SDRAM master: merge the window into the scanout master before Qsys | about 1,284 (2,065 against 770), before S-lat; overlaps S-lat | It is an RTL design change in #2036's files, which the brief rules out | DE25 pc386 |
| The F2SDRAM agent's `read_rsp_fifo` and `write_rsp_fifo` | 350 + 74 | `responseFifoType` does not govern this AXI agent's FIFOs. Its generated HDL is byte-identical to `j`, with `USE_MEMORY_BLOCKS (0)` | Every DE25 core |
| The rest of the vendor HPS adapters (`sm_hps`, `sm_mpfe`) | about 630 left after S-ocm | Vendor IP | Every DE25 core |

## AT audit candidates not ablated

These are estimates from a read-only RTL audit; figures marked (g) are guesses. Each is small, and none adds cycles on a hot path.

| Candidate | Estimate | Class | Gate |
|---|---:|---|---|
| RTC: one sequential converter instead of 13 `to_bcd` instances (`pc386_rtc.sv:97-130`) | 100–150 (g) | Feature: DM or 24/12 flips stop converting a live time (as on a real MC146818), which breaks `devices_tb.cpp:1021-1024` | Possible; needs Deano's call |
| HID set-2 ROM into M20K (`pc386_hid_keyboard.sv:66-154`) | 40–60 (g) | One clock of latency per key scan | Yes |
| Duplicate DMA page registers (`pc386_at_core.sv:251`) | about 40 | Refactor | Yes |
| FDC: byte-enable writes, narrower `wait_us`, shared prescaler | about 50 | Refactor | Yes |
| ATA IDENTIFY strings into a ROM | 30–50 (g) | Refactor | Yes |
| Mouse 2:1 scaling, wrap and remote modes | 30–45 (g) | Feature; nothing uses them | Yes |
| ATA's 5 DSPs, FDC's 1 and HID's 1 | +10–30 ALMs (g) to free them | About 11 clocks once per ATA command, against at least 64 DDR beats per sector | Yes. This frees DSPs, not ALMs |

Do not cut: PIT modes 1 and 5, the RTC periodic interrupt (used by the BIOS INT 15h AH=83h wait), the FDC 82077 extras (Bochs replay), and dma1 auto-init (Sound Blaster).

## Deano's cycle rule

None of the measured cuts adds cycles. The DE10 question is therefore about netlist identity and behaviour, not timing.
- **S-ocm and S-lat:** DE25 managed-shell Qsys settings only. The DE10 build has no `qsys_top`, so neither touches the DE10 or the machine netlist. They apply to every DE25 core, 387 on or off, so they need hardware runs across DE25 cores rather than a per-board parameter.
- **A-dma2 and A-kbcram:** these drop features. Each needs a per-board parameter, defaulting to today's behaviour, to leave the DE10 unchanged.
- **A-fifo:** keeps behaviour, but it moves DE10's BUILD_ID unless a parameter selects the register form there.

There is no `X87_PRESENT` parameter to reuse: `x87_present` is a constant wire (`pc386_system.sv:470`). Any of these gates needs a new `pc386_system` parameter, and none of them is blocked by one RTL path.

## Outside this study's scope (machine-level, from #2037's entity fit)

For the 387-on step's planning only; these were not measured here:

| Entity | ALMs |
|---|---:|
| vga | 1,840 |
| audio (of which OPL2) | 1,333 (881) |
| presenter | 581 |
| storage | 489 |
| port2 | 380 |
| `status_ring` | 222 |
| disk | 191 |
| `gp_mailbox` | 190 |

## Notes on the runs

- **Shell netlist identity:** every shell seed compiles build `j`'s exact machine netlist (`5b80c0cd…`), as amended in method.md.
- **AT baseline:** the AT arm fits its own baseline, elaboration `cc9b0a89…`.
- **Raw fit outputs:** about 12 GB stay on chip-30x0-ai-dev under `/data/projects/fes-de25-area/` (`shell2-*` shell seeds, `fit-*` AT seeds, `area-*-out` board builds, `locked-*.log` run logs). The shell baseline seeds are build `j` and the controlled pair under `/data/projects/fes-de25-pc386/` (`board-j-out`, `pair-j-board-s2` to `s4`). `collect.json` was produced from these.
- **Shared lease:** from 21 November every shell build and seed ran under `flock /tmp/lander-quartus-3090.lock`, released between seeds. S-lat's seeds ran 10:46–13:10Z.
- **Memory kills:** on 20 November, about 21:36Z, three runs were killed by signal 9: S-ocm s3, S-rsp s1 and A-dma2 s1. The 23 GB host was running two shell compiles and an AT fit at once. S-ocm s3 and A-dma2 s1 were rerun one at a time with the same inputs; S-rsp v1 s1 was not rerun.
