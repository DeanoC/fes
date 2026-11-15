# pc386: OPL2's register memories in MLAB, 13 November 2026

This record puts six small OPL2 memories into Cyclone V MLABs in the pc386
Quartus seal. It is one of the area wins Deano chose to land before x87
slice 2 ([#1768](https://github.com/DeanoC/fes/issues/1768)): the all-fabric
387 fit spike ([#1786](https://github.com/DeanoC/fes/pull/1786)) came out
over the 92% ceiling. The [area audit](../2026-11-08-i386-area-audit/summary.md)
([#1584](https://github.com/DeanoC/fes/pull/1584)) found that Quartus leaves
eight small OPL2 memories uninferred, so they are built from registers and
LUTs.

**Result.**
- **−160 ALMs and −666 registers** in a seal of this branch, against a seal
  of main 258dffc1d. Both were sealed on chip-30x0-ai-dev with the same
  CUDA toolchain slot and seed 1: 36,864 → 36,704 ALMs (88%).
- **Six of the eight memories are now asynchronous-read MLABs**, each in
  one LAB, with their power-on contents in a MIF. Quartus reports two
  uninferred memories instead of eight. Those two are the 9×1 `cnt` and
  `kon` memories, refused for size (276004), and they stay registers.
- **Timing passes at every corner, with the same clocks.** Setup slack is
  +0.725 ns (main +0.635), hold +0.078 ns (main +0.090), removal +0.274 ns
  (main +0.614).
- **The RTL is unchanged.** The only change is a Quartus attribute on the
  arrays, so every simulation runs the same logic as before. The OPL2
  reference suite and every machine run below check each OPL2 sample
  against Nuked-OPL3 and pass. The kit check compares the HDMI audio of
  the Ultima VI intro under both seals ([below](#kit-check)).

## The memories

The audit's eight, from the main seal's 276014 list
([main-ram-inference.txt](evidence/main-ram-inference.txt)):

| Memory | Size | Main | This branch |
| --- | --- | --- | --- |
| `control_operators.am_vib_egt_ksr_mult_mem` | 22×8 | logic (276007) | MLAB |
| `control_operators.ar_dr_mem` | 22×8 | logic (276007) | MLAB |
| `control_operators.sl_rr_mem` | 22×8 | logic (276007) | MLAB |
| `control_operators.fnum_low_mem` | 9×8 | logic (276007) | MLAB |
| `control_operators.block_fnum_high_mem` | 9×5 | logic (276007) | MLAB |
| `envelope_generator.state_mem` | 18×4 | logic (276007) | MLAB |
| `control_operators.kon_mem` | 9×1 | logic (276004) | logic (276004) |
| `channels.cnt_mem` | 9×1 | logic (276004) | logic (276004) |

The other OPL2 memories were already block RAM. Quartus absorbed the
registers around them, which it can't do for these six because their read
data goes through logic before any register. They are unchanged.
([branch-ram-summary.txt](evidence/branch-ram-summary.txt) is the fit's RAM
summary.)

### Why the 9×1 memories stay registers

An MLAB takes a whole LAB, which is 10 ALMs. Nine flip-flops and their
9-to-1 read mux take fewer, and Quartus refuses an MLAB at that size anyway.
`kon_mem` shares its address and write enable with `block_fnum_high_mem`, so it
could join it. But its reset sweep clears key-on alone, and merging the two
would clear the block and F-number with it, which changes the release after
a reset. That would no longer be bit-exact, so both stay as they are.

## The change

`mem_simple_dual_port_async_read` gets a `RAMSTYLE_MLAB` parameter. When it
is set, the array carries `(* ramstyle = "MLAB, no_rw_check" *)`.
`mem_single_bank` passes the parameter through, and the six instances set
it. The netlist keeps attributes (the seal's `attrmap` removes only `src` and
`hdlname`), so the attribute reaches Quartus unchanged. The Yosys program,
its `program_sha256` and the lockstep identity are unchanged. The read stays
asynchronous and the write synchronous, so the RTL is the same with or
without it. Upstream's `/* synthesis syn_ramstyle = "MLAB, no_rw_check" */`
comment had no effect: Yosys doesn't carry that comment into its netlist,
so it never reached Quartus. It is gone. `source.json` records each changed file.

### Why `no_rw_check`

A test fit of the same Yosys output on chip-30x0-ai-dev (Quartus 17.0.2,
5CSEBA6U23I7) gave these results:

| Array attribute | Quartus |
| --- | --- |
| none | uninferred, 276007 (asynchronous read) |
| `ramstyle = "MLAB"` | uninferred, 276009 (unsupported read-during-write behaviour) |
| `ramstyle = "MLAB, no_rw_check"` | `altdpram` MLAB, asynchronous read, MIF from the initial values |

So `no_rw_check` is the only way Quartus 17.0.2 builds these memories as
MLABs. It doesn't change what the design reads, for the following reasons.
- Each memory has one write port, in one clock domain.
- The write commits at a clock edge. The read is combinational and is only
  captured by registers on later edges of the same clock. A register that
  captures at the write edge sees the old word; one that captures at any
  later edge sees the new word. That holds for the RTL and for the MLAB as
  long as the MLAB's write-to-read path meets timing, and the seal's timing
  analysis covers that path.
- What `no_rw_check` waives is the read value *during* the cycle when the
  write lands. No register samples these memories then, so the waiver changes
  nothing that the design observes.

The CPU writes its OPL2 registers while the operator scan reads them, so
the two addresses do collide. Each collision gives the same samples as
before for the reason above. The kit check is the post-fit evidence for
this.

### Power-on contents

These memories have initial values: zero, except the envelope state, which
is `RELEASE` (4'b1000). Yosys writes them as `initial` blocks, and Quartus
puts them in each MLAB's MIF. The state memory's MIF in the seal holds `1000`
in all 18 entries.

## Seals

Both are `scripts/build_fes_pc386_quartus.py --seed 1 --gpu-router CUDA` on
chip-30x0-ai-dev, from clean worktrees, with the pc386 lock's CUDA slot
`af117562…`.

| | Main 258dffc1d | This branch d8aae9759 |
| --- | ---: | ---: |
| Package | `87f9a24f…` | `2c40b849…` |
| BUILD_ID | `a6d8ed23…` | `21825250…` |
| ALMs | 36,864 | 36,704 (−160) |
| ALMs used for memory | 20 | 80 |
| Registers | 32,785 | 32,119 (−666) |
| Block memory bits / RAM blocks | 1,349,532 / 269 | 1,349,532 / 269 |
| Setup slack | +0.635 ns | +0.725 ns |
| Hold slack | +0.090 ns | +0.078 ns |
| Recovery / removal | +6.786 / +0.614 ns | +7.067 / +0.274 ns |
| System / pixel / audio | 61.44 / 74.27 / 12.29 MHz | 61.44 / 74.27 / 12.29 MHz |

[seal-summary.json](seal-summary.json) is this branch's `build-summary.json`,
and [evidence/main-seal-summary.json](evidence/main-seal-summary.json) is
main's. The branch head after d8aae9759 changes only `source.json`, which is not
a seal input, so this seal covers it.

The audit budgeted about −150 ALMs for the four 22×8 memories. The six give
−160. The other ALMs of the old register files went back into packing:
"ALMs used for LUT logic and registers" fell by 182.

## Simulation

Verilator ignores `ramstyle`, and the generate branch only renames the
array (`mem_bank.mlab.ram`), so the simulated logic is the same as main's.
Every machine run compares each OPL2 sample with Nuked-OPL3 and fails on
any difference (`exact` must equal `samples`). These runs were on
Powerboat, on 13 November. The RTL is the same at every commit of this
branch from d8aae9759 on; the later commits change only `source.json` and
this record:

| Run | Result |
| --- | --- |
| `tests/test_pc386_machine.py` (73 tests, including the import manifest checks) | pass |
| `sim_fes_pc386.py --suite opl2` (register sequences, three random sequences, the log replay; all sample-exact against Nuked-OPL3) | pass |
| `--suite sound --download` (Sound Blaster DSP and mixer) | pass (SB single-cycle and auto-init 1 kHz sections, 9,000 DMA bytes, 6 IRQs) |
| `--suite speaker` (SPKTEST.COM: port 61h, rise, carrier) | pass |
| `--suite game --game ultima6 --cpu rtl` (16 guest seconds of the intro, 0 oracle steps) | pass: OPL2 exact 795,454 of 795,454 samples, 3,055 register writes |
| `--suite game --game wolf3d --cpu rtl` (24 guest seconds, 0 oracle steps) | pass: OPL2 exact 1,193,181 of 1,193,181 samples, 2,327 register writes; SB 12,829 DMA bytes |

## Kit check

Kit B, run `o1`, 12:08–12:14 EET on 13 November. The run was core-only on
the baseline software 7ddb0bfb and image 179affbb: nothing was installed,
and there was no reboot and no host change. Under one kit lease, the
agent loaded main's package `87f9a24f`, then this branch's `2c40b849`. Each
was a fresh library game on the user-supplied `real` drive `a6977555`
(never committed). The run typed the Ultima VI launch, and Powerboat
recorded 92.5 s of the intro's HDMI audio from the 4KPRO
(`hw:CARD=D4KPRO`, 48 kHz).
Both intros reached the same scene, and the music played under both:

| Capture | Active (above floor) | Mean RMS | AC peak |
| --- | ---: | ---: | ---: |
| Main | 88.6 s | 382.3 | 1,440 |
| This branch | 88.6 s | 382.4 | 1,434 |

The two 12.5 ms RMS envelopes correlate at 0.976 at their best lag
(37.5 ms), which is the difference in when the two launches were typed. The
capture goes through the mixer's resampler, the kit's HDMI audio and the
capture card, and the launch time is set by keys typed over the network.
So the capture can't be compared sample for sample. It shows the same music
at the same level, and the simulations above give the sample-exact proof.
The wrapper removed the five data trees the run created. The kit was logged
FREE with its baseline, boot, agent and data paths as found
([evidence/kit-o1](evidence/kit-o1): the commands, wrapper log, per-100 ms
levels, [cmp_audio.py](evidence/kit-o1/cmp_audio.py) and its output).

## Other cores

The OPL2 import (`cores/fes-pc386/vendor/opl2_fpga`) is used only by pc386.
No other core's tests read it.
