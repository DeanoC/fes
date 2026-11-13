# i386 x87: fit spike for the lean datapath, 12–13 November 2026

Slice 1 of the [x87 plan](../2026-11-12-i386-x87-plan/summary.md)
([#1768](https://github.com/DeanoC/fes/issues/1768)) measures the lean 387 datapath
before any of it goes into the core. The spike RTL
([`spike_x87.sv`](../../../sources/misteross/cores/fes-common/sim/i386/spike/spike_x87.sv))
holds the datapath, in variants set by parameter. The runner
([`quartus_x87_spike.py`](../../../sources/misteross/scripts/quartus_x87_spike.py))
fits each variant twice with Quartus 17.0.2 at the seal's settings: on its own, and
inside `pc386_system` as the seal builds it. This record changes no production RTL and
no seal.

**Result.**
- **Option A fits, but at the ceiling.** Option A is all fabric: Goldschmidt divide and
  square root on a 9-DSP multiplier, shifts done as multiplies, and transcendentals in
  microcode.
  - Inside pc386 it adds **+1,687 ALMs, 9 DSP blocks and 17 M10K**, the mean of three
    seeds against a same-flow baseline.
  - That puts the machine at 38,452–38,518 ALMs (91.7–91.9%), just under the 92%
    ceiling of 38,557.
  - Measured blocks came in below the plan's 2,400-ALM estimate (2,000 without the CPU
    side); the CPU side (about 400 ALMs) is not built yet. Adding it puts A at about
    **38,900 (92.8%)**, over the ceiling.
- **Moving work to the HPS saves no ALMs.**
  - D-hps puts divide, square root, the transcendentals, FPREM and BCD on the HPS. It
    adds **+1,799 ALMs** (mean of three seeds), within noise of A.
  - It saves 7 M10K and nothing else. The mailbox's 80-bit operand and result registers,
    and their muxes, cost about what the slow path costs on the fabric datapath.
  - The microcode transcendentals cost ROM, not logic.
- **CPU timing degrades with any x87.**
  - The baseline meets the 61.44 MHz CPU clock on all three seeds (+0.58 to +0.86 ns).
  - With A it meets on two seeds of three (+0.030, +0.548, −0.878 ns); with D-hps on one
    (+0.226, −0.079, −0.102 ns).
  - Every miss is on a CPU path: `cpu_dfwd_ready`, `cpu_frontend_align_start`,
    `cpu_l1d_stall`. Every path through the x87 has +0.85 to +2.4 ns of slack.
  - This is the routing congestion the plan predicted at 92%, not logic depth in the
    x87.
- **The cheaper-DSP and LUT-shifter variants cost more fabric.**
  - LUT barrel shifters add about 315 ALMs and remove the slack.
  - Building the multiply from 3 DSPs or 1 DSP, instead of 9, adds 300–430 ALMs. DSPs
    are plentiful: 74 are free.
  - A radix-2 divider instead of Goldschmidt adds about 135 ALMs.
- **Recommendation.**
  - Build option A in the fabric.
  - Land the area audit's wins before slice 2 adds the CPU side: the HID keyboard
    redesign first (about −650), then the return stack in MLAB (about −250) and OPL2 in
    MLAB (about −150).
  - Deano decides; see [Open questions](#open-questions-for-deano).

## Method

**Device and budget.** The device is a 5CSEBA6U23I7 with 41,910 ALMs. The 92% working
ceiling is 38,557 ALMs, from the [area audit](../2026-11-08-i386-area-audit/summary.md).
The CPU clock is the system clock at 61.44 MHz (`SYSTEM_CLOCK_HZ`). Its TimeQuest name
is `system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk`.

**Toolchain and settings.** The pc386 lock's toolchain slot was `af117562`, the CUDA
slot on chip-30x0-ai-dev, with Yosys `e240a4653` and Quartus 17.0.2 b602. Quartus fit
is host-independent, and Yosys is the lock's. Each fit uses the flow's best settings,
the seal's:
- Yosys runs the seal's program (`yosys_netlist.SEAL`).
- Quartus runs at default optimisation with `NUM_PARALLEL_PROCESSORS ALL`.
- DSP inference is on, with no extra fitter options.

The standalone QSF carries the same global assignments as the sealer's
`project_qsf`, apart from the project paths and the seal's HPS pin.

**Standalone fits.**
- `spike_x87` sits in the timing-spike envelope (`spike.qsf`).
- `spike_stimulus` drives the command and data inputs with LFSR noise, and a rotating
  64-bit signature of the outputs drives LED0, so nothing is trimmed.
- The SDC constrains the one clock to the 61.44 MHz period.

**Machine fits.**
- `--mode machine` builds `pc386_system` through the sealer's own `write_project`,
  firmware ROM, guest rates and Yosys program.
- The runner overlays a copy of `pc386_system.sv` (the checkout is not changed) that
  instantiates `x87s_port` on the CPU's I/O bus at ports F8h–FFh and ORs its read data
  into the IN path.
- The port registers both directions. It loops the HPS mailbox back inside the fabric:
  the result is a function of the operands, so neither side is trimmed.
- `--variant base` fits the unchanged machine through the same flow. It is the
  baseline every delta uses: 36,758–36,838 ALMs. The [#1728 seal](../2026-11-12-i386-ud-1701/seal-summary.json)
  used 36,803 ALMs.

**Timing.** Timing is the multicorner summary's worst setup and hold slack on the CPU
clock. Critical paths are TimeQuest's slow-corner worst path, plus, for the machine,
the worst path into or out of an `x87_*` node.

**Trimming checked.**
- All register-stack, microcode, dispatch, constant and seed ROMs land in M10K, with no
  MLAB.
- Quartus removes only structural bits in the standalone and machine fits:
  - the top bits of five 44-bit partial products;
  - six load-conversion bits that are always zero;
  - two read-index bits that are 0 because port A reads only ST(0) or ST(1);
  - a busy flag merged with its copy.
- The machine has 1,758 more registers than the baseline (seed 1); the standalone unit
  has 1,587.

**Per-block ALMs.** The `--hierarchy` runs drop Yosys's `flatten`, which is the audit's
method. They give each entity's ALMs ([`entities.tsv`](entities.tsv)).

**RTL measured.**
- The A, A-lut, D and machine-A fits ran on `65215aa57`.
- Every other fit ran on `6388b697b` or `d812b8a55`. Those commits differ from
  `65215aa57` only in the seed-select bit of the `DIVSQRT == 1` block, which those
  variants don't build, and in the runner.
- An earlier round of A, A-lut, D and machine A had that bit inside the op field, so
  the reciprocal seed ROM was never selected and Quartus removed it. Those numbers are
  superseded and not used here.

All fit rows are in [`fits.tsv`](fits.tsv); the fits that failed to route are in
[`no-route.tsv`](no-route.tsv).

## The variants

| Variant | MUL | SHIFT | DIVSQRT | TRANS | What it is |
| --- | --- | --- | --- | --- | --- |
| `a` | 0: 9 DSP, 4 clocks | 0: shifts as multiplies | 1: Goldschmidt on the multiplier, seeds in M10K | 1: microcode, 1,024 words, 256 constants | Plan option A, the lean all-fabric unit |
| `a-r2` | 0 | 0 | 2: radix-2, one bit a clock | 1 | A with a digit-recurrence divider |
| `a-lut` | 0 | 1: LUT barrel shifters | 1 | 1 | A with LUT shifters |
| `a-mul3` | 1: 3 DSP, three passes | 1 | 2 | 1 | Fewer DSPs (changes MUL, SHIFT and DIVSQRT together) |
| `a-mul1` | 2: 1 DSP, nine passes | 1 | 2 | 1 | Near plan option B (changes MUL, SHIFT and DIVSQRT together) |
| `d` | 0 | 0 | 1 | 0: HPS, 256 words, 8 constants | Plan option D as written: divide and square root in fabric |
| `d-hps` | 0 | 0 | 0: HPS | 0 | Lean fabric core: divide, square root and transcendentals on the HPS |

All variants share these blocks:
- the eight 80-bit registers in M10K, with two read ports, and the FXCH remap table and
  tags;
- the control, status and tag words, the exception flags and masks;
- the 68-bit add and subtract, and the rounder (24, 53 and 64 bits, four modes, guard
  and sticky);
- leading-zero count, compare, FSCALE;
- load and store conversions (f32, f64, f80, i16, i32, i64);
- the fast operations at issue (FLDCW, FNSTCW, FNSTSW, FNCLEX, FNINIT, FXCH, FFREE,
  FINCSTP, FDECSTP).

`a-mul3` and `a-mul1` change three parameters at once, so they bound the narrow-DSP
designs rather than isolate the multiplier. Only `a-r2` (divider) and `a-lut`
(shifter) differ from A in one parameter.

## Machine fits (inside pc386)

The baseline has 38 DSP blocks and 269 M10K. Δ is against the mean of the three
baseline seeds, 36,808 ALMs.

| Variant | Seed | ALMs | % of device | vs 92% ceiling | DSP | M10K | CPU setup (ns) | CPU hold (ns) | Worst path | Worst x87 path (ns) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| base | 1 | 36,758 | 87.7% | −1,799 | 38 | 269 | +0.582 | +0.094 | `cpu_dfwd_ready` → `_34937_` | — |
| base | 2 | 36,838 | 87.9% | −1,719 | 38 | 269 | +0.860 | +0.091 | `cpu_dfwd_ready` → `_34937_` | — |
| base | 3 | 36,827 | 87.9% | −1,730 | 38 | 269 | +0.743 | +0.122 | `cpu_dfwd_ready` → `cpu_dc0` | — |
| **a** | 1 | 38,452 | 91.7% | −105 | 47 | 286 | +0.030 | +0.093 | `cpu_decode_by` → `_35629_` | +2.242 |
| **a** | 2 | 38,516 | 91.9% | −41 | 47 | 286 | +0.548 | +0.045 | `cpu_decode_by` → `_35629_` | +2.426 |
| **a** | 3 | 38,518 | 91.9% | −39 | 47 | 286 | **−0.878** | +0.077 | `_35745_` → `cpu_l1d_stall` | +0.848 |
| **d-hps** | 1 | 38,722 | 92.4% | **+165** | 47 | 279 | +0.226 | +0.079 | `_35765_` → `cpu_wb_rlin` | +2.181 |
| **d-hps** | 2 | 38,549 | 92.0% | −8 | 47 | 279 | **−0.079** | +0.077 | `cpu_frontend_align_start` (self) | +2.158 |
| **d-hps** | 3 | 38,549 | 92.0% | −8 | 47 | 279 | **−0.102** | +0.071 | `cpu_dfwd_ready` → `cpu_dc0` | +2.215 |

| Variant | Mean ALMs | Δ ALMs | Δ DSP | Δ M10K | Seeds meeting setup |
| --- | ---: | ---: | ---: | ---: | --- |
| base | 36,808 | — | — | — | 3 of 3 |
| a | 38,495 | **+1,687** | +9 | +17 | 2 of 3 |
| d-hps | 38,607 | **+1,799** | +9 | +10 | 1 of 3 |

Notes on these fits:
- Hold is met everywhere, at +0.045 ns or more.
- Each machine fit took 16–40 minutes on chip.
- The other variants weren't fitted in the machine, because the standalone fits rank
  them all above A.

## Standalone fits

These are the unit alone in the envelope at 61.44 MHz. The ALMs include about 90 ALMs
of stimulus and top-level signature, which aren't in the unit.

| Variant | Seed | ALMs | DSP | M10K | Setup (ns) | Hold (ns) | Worst path |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| a | 1 | 2,138 | 9 | 17 | +1.719 | +0.061 | `ua` → `z` |
| a | 2 | 2,125 | 9 | 17 | +1.913 | +0.083 | `ua` → `z` |
| a (hierarchy) | 1 | 2,138 | 9 | 17 | +1.833 | +0.045 | `ub` → `z` |
| a-r2 | 1 | 2,269 | 9 | 15 | +2.394 | +0.036 | `ua` → ucode ROM address |
| a-r2 | 2 | 2,264 | 9 | 15 | +0.668 | +0.093 | `ua` → `mul_b` |
| a-lut | 1 | 2,453 | 9 | 17 | **−0.030** | +0.074 | `ub` → `y` |
| a-lut | 2 | 2,439 | 9 | 17 | +0.213 | +0.097 | `ub` → `z` |
| a-lut | 3 | 2,462 | 9 | 17 | +0.183 | +0.057 | `ub` → `z` |
| a-mul3 | 1 | 2,568 | 3 | 15 | +0.451 | +0.077 | `ua` → `y` |
| a-mul3 | 2 | 2,572 | 3 | 15 | +0.974 | +0.091 | `ua` → `y` |
| a-mul1 | 1 | 2,550 | 1 | 15 | **−0.070** | +0.117 | `ub` → `z` |
| a-mul1 | 2 | no route | | | | | |
| a-mul1 | 3 | 2,549 | 1 | 15 | +0.415 | +0.115 | `ub` → `z` |
| d | 1 | 2,215 | 9 | 12 | +1.452 | +0.077 | `ua` → `z` |
| d | 2 | no route | | | | | |
| d-hps | 1 | no route | | | | | |
| d-hps (hierarchy) | 1 | 2,175 | 9 | 10 | +1.813 | +0.045 | `ua` → ucode ROM address |
| d-hps | 2 | 2,182 | 9 | 10 | +2.515 | +0.116 | `ua` → `mul_b` |
| d-hps | 3 | 2,175 | 9 | 10 | +1.702 | +0.079 | `ua` → `z` |

| Variant | ALMs (mean of fits that routed) | vs A |
| --- | ---: | ---: |
| a | 2,134 | — |
| a-r2 | 2,267 | +133 |
| a-lut | 2,451 | +317 |
| d | 2,215 | +81 |
| d-hps | 2,177 | +43 |
| a-mul1 | 2,550 | +416 (8 fewer DSP) |
| a-mul3 | 2,570 | +436 (6 fewer DSP) |

**Fits that did not route.** Three standalone fits failed. In each, Quartus could not
route one or two LAB inputs ([`no-route.tsv`](no-route.tsv)), on bits of `ub` (the
B-operand register) and a compare (`LessThan2`):
- **Where.** Quartus packs the whole unit into about an 11×11-LAB region, at about 5%
  of the device. In every fit, even the ones that routed, `ub` bits were the signals
  the router retried.
- **Why.** `ub` feeds the adder, the multiplier's operand muxes, compare, the store
  conversions and the seed ROM addresses.
- **Machine fits.** None of the nine machine fits failed to route; the unit is spread
  out there.
- **Settings.** The standalone and machine fits use the same settings, both
  Auto Fit.
- **Conclusion.** This is a property of the spike's one shared operand register. The
  real unit should give the multiplier and the adder their own copies of the operand
  register. The failures aren't a reason to prefer one variant over another.

## Where the ALMs go

These are A and D-hps with Yosys's `flatten` dropped
([`entities.tsv`](entities.tsv)). The plan's estimate is shown for comparison.

| Block | A (measured) | D-hps (measured) | Plan, lean |
| --- | ---: | ---: | ---: |
| 64×64 multiply glue (`x87s_mul`; 9 DSP, 395 registers) | 192 | 170 | 150 |
| Rounder (`x87s_round`) | 51 | 49 | in the row below |
| Rest of the unit: register-stack ports, tags, words, exponent path, 68-bit add, leading-zero count, alignment, conversions, sequencer, seeds or mailbox | 1,802 | 1,840 | 1,850 |
| **Unit total** | **2,045** | **2,058** | **2,000 without the CPU side** |
| M10K: registers ×2 copies, microcode, dispatch, constants, seeds | 4 + 7 + 2 + 2 + 2 = 17 | 4 + 2 + 2 + 2 = 10 | |

Yosys merges the unit's own logic into one entity, so the hierarchy can't split it
further. Without the CPU side the plan's estimate was about 2,000 ALMs, and the fabric
unit measures 2,045 standalone. In the machine it adds 1,687, because it shares packing
with its neighbours.

**Why the HPS variant isn't smaller.**
- Option A's slow path costs only these:
  - ROM words: the microcode is 7 M10K against 2;
  - two seed ROMs;
  - a few sequencer conditions.
- Divide and square root run on the multiplier and adder that the fast path already
  has.
- D-hps removes those and adds the HPS mailbox: two 80-bit operand registers, an 80-bit
  result, the op and the handshake, plus the muxes into and out of them.
- That comes to about 250 registers and their muxes, about what A's slow path costs.
- A real HPS bridge needs more fabric than the spike's loop-back: a lightweight AXI
  slave or a DDR window, and an interrupt. So the spike understates the HPS variant's
  cost.

## Recommendation

1. **Build option A in the fabric:**
   - 9-DSP multiply;
   - shifts as multiplies;
   - Goldschmidt divide and square root with M10K seeds;
   - transcendentals, FPREM and BCD in microcode.

   It is the smallest of the variants measured. It closes timing standalone by +1.7 ns.
   Its own paths in the machine have +0.85 ns or more. The HPS buys nothing in fabric,
   and it would still cost the run-to-run timing, a second implementation and a bench
   model that the plan lists.
2. **Make room before slice 2.**
   - A fits at 91.7–91.9% without its CPU side. With that side (issue, scoreboard,
     operand uops, the IRQ 13 latch and F0h/F1h, about 400 ALMs) it is about 38,900
     ALMs (92.8%), over the ceiling.
   - The machine already misses CPU timing on one seed of three with A, at −0.878 ns.
   - The audit's wins come first: the HID keyboard's vectors (about −650), the return
     stack in MLAB (about −250), OPL2's memories in MLAB (about −150). Together they put
     A with its CPU side at about 37,850 ALMs (90.3%).
3. **Keep the 9-DSP multiply and the multiply-based shifts.** The narrow-DSP variants
   and the LUT shifters each cost 300–430 ALMs to save DSPs, which aren't scarce (74
   free).
4. **Give the operands their own copies in slice 3.** That means one operand register
   for the multiplier and one for the adder, to remove the `ub` routing hot spot seen in
   the standalone fits.

## Open questions for Deano

1. **Fabric or HPS for the slow operations.** The spike finds that HPS offload (D-hps)
   saves no ALMs against the all-fabric unit, only 7 M10K. The HPS round trip is still
   unmeasured. The recommendation is the fabric; this is your call.
2. **What to trim to get under 92%.** In order, the options are:
   - the audit's wins, keyboard first;
   - a second core variant (`fes.pc386-387`);
   - accepting a seal above 92%, where the machine fits already miss CPU setup on one or
     two seeds of three.

   Which first, and is the keyboard redesign in scope before slice 2?
3. **The margin to hold.** Before the x87's CPU side goes in, should the machine meet
   timing on every seed? Or is one passing seed per seal enough, as today? The
   baseline has +0.58 to +0.86 ns; with A it is +0.55 to −0.88 ns.

## Caveats

- **The ROMs hold placeholders.** The microcode, dispatch, constant and seed ROMs hold
  a hash of the address, at their real widths and depths. Their M10K use and the muxes
  their fields steer are real; the unit computes nothing useful, and real microcode
  could leave some fields constant, which Quartus would trim.
- **The CPU side isn't built.** ESC issue, the scoreboard, operand uops, the IRQ 13
  latch and F0h/F1h are the plan's estimate of about 400 ALMs, not a measurement.
- **The tie-in is an I/O-port stand-in.** It is not the CPU's ESC path. The routing
  pressure where the real interface meets the CPU's decode and memory stages isn't
  measured.
- **The HPS mailbox loops back in the fabric.** With no HPS bridge, the D and D-hps
  numbers are a lower bound.
- **Noise.** Fit noise is about ±200 ALMs and about ±0.5 ns at this fill. The three
  seeds per machine variant show −0.878 to +0.548 ns for A. The A and D-hps gap
  (112 ALMs) is inside the noise, so the two are equal on area.
- **The fits ran on chip-30x0-ai-dev's CUDA toolchain slot.** Quartus fits are
  host-independent and the Yosys build is the lock's, but HIP and CUDA toolchain slots
  have produced different pc386 fits before
  ([#1699](https://github.com/DeanoC/fes/pull/1699)). The baseline here reproduces the #1728 seal within 45
  ALMs.

## Reproducing

On a host with Quartus 17.0.2 and the pc386 lock's toolchain slot, from
`sources/misteross`:

```bash
QUARTUS_ROOTDIR=/abs/intelFPGA_lite/17.0/quartus python3 scripts/quartus_x87_spike.py --cache-root /abs/fes-toolchain-cache --gpu-router CUDA --mode machine --variant a --seed 1 --output /abs/scratch/machine-a-s1
```

- `--mode standalone` fits the unit alone, in about 3 minutes.
- `--variant base` fits the unchanged machine.
- `--hierarchy` gives per-entity ALMs.
- Each run writes `summary.json`, the fit, map and timing reports, and `paths.tsv`, and
  deletes the Quartus project unless `--keep` is given.
