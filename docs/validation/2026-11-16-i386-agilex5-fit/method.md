# How large is the 386 on Agilex 5? The method, committed before measuring

**Committed 16 November 2026, before any fit ran.** Amendments are dated and
say what had already been seen when they were made. Tracker: [#1898](https://github.com/DeanoC/fes/issues/1898).

The question (Deano's words): *"we need see the entire size of the 386 on the
agilex 5, it may be bigger or smaller as different fabric and toolchain."*
It gates the DE25 plan, because [#1889](https://github.com/DeanoC/fes/pull/1889)
showed the DE10 cannot hold the machine with the `i386dx+387` identity on
(4,292 LABs needed against the Cyclone V's 4,191, 102.4%: the fitter stops
before placement), and [#1892](https://github.com/DeanoC/fes/pull/1892) fitted
a standalone pipelined x87 arithmetic backend on Agilex 5 at 2,660 ALMs,
9 DSP and 4 M20K at 200 MHz.

## What this measurement is not

**Cyclone V ALMs and Agilex 5 ALMs are different units.** Different fabric,
different packing, different toolchain and different synthesis. This record
will report **raw resources and percentage of each target device**, every
figure labelled with its device, toolchain version, flow and envelope. It will
**not** state that the 386 is some percentage smaller or larger on Agilex 5,
and it will compute no cross-device ALM ratio. The comparable statements it
will make are each device's utilisation and each device's achievable clock.

This lane has been bitten by softer versions of this error before: identical
RTL fitting 14 ALMs apart between two Yosys slots on the *same* device, and a
53% area claim that crossed both scope and host. Where a comparison cannot be
made honestly, this record says so instead of making it.

No board port, no pin assignment, no kit, no seal, no programming artifact and
no hardware acceptance. These are internal virtual-pin characterisation fits,
following the shape [#1892](https://github.com/DeanoC/fes/pull/1892)
established in `sources/misteross/boards/de25nano/x87/`.

## Devices and toolchains

| Side | Device | ALMs on device | Toolchain |
| --- | --- | ---: | --- |
| DE10-Nano | `5CSEBA6U23I7` (Cyclone V) | 41,910 | Quartus 17.0.2, `cores/fes-pc386/quartus.lock` |
| DE25-Nano | `A5EB013BB23BE4SCS` (Agilex 5) | 46,800 | Quartus Pro 26.3.0 Build 114, `boards/de25nano/quartus.lock` |

Both locks authenticate their `quartus_sh` by version line and digest. Quartus
Pro and the Agilex 5 licence live on `chip-30x0-ai-dev`; both sides' fits run
there, under the established `/tmp/lander-quartus-3090.lock` builder lease.

## The front end: one netlist, two fitters

The locked Quartus 17.0.2 SystemVerilog front end rejects the i386 RTL, so the
DE10 lane feeds Quartus a Yosys-elaborated netlist (`scripts/yosys_netlist.py`,
the `SEAL` recipe: `read_verilog -sv; hierarchy -top; proc -norom; flatten;
opt -purge; <pc386 memory policy>; rename -unescape; write_verilog`). That
recipe is **device-neutral** — no `synth_intel_alm`, no technology mapping —
so the same netlist bytes are valid input for both fitters, and each toolchain
does all of its own synthesis, mapping, placement and routing.

**Primary flow, both devices: the same Yosys `SEAL` netlist into each
device's locked Quartus.** This is the one that makes the two sides
comparable in input, and it is the netlist the i386 lockstep verifies.

**Secondary, labelled separately if it runs:** Quartus Pro 26.3 reading the
SystemVerilog directly, which is what a real DE25 flow would do. Its register
count is cross-checked against the primary on the same device; a large gap
means swept or mis-elaborated logic and the secondary is then withdrawn, not
reported as an area result. If Pro rejects the Yosys netlist, this becomes the
primary for the Agilex side and that substitution is recorded here, dated.

## The fits

Four precommitted fits, plus one scratch variant. `flatten` means each fit
reports one entity, so there is no per-module split; that is stated, not
inferred around.

| Fit | Top | x87 | Devices |
| --- | --- | --- | --- |
| **C** | `fes_i386_cpu` | `x87_present` a free virtual input, so the slice-2 387 is alive | both |
| **M** | `pc386_system` | as main ties it: `pc386_system.sv` has `wire x87_present = 1'b0`, so most of the 387 is swept | both |
| **M+** | `pc386_system`, one-character scratch change to `1'b1` | identity on | Agilex 5 |

`pc386_system` **contains** `fes_i386_cpu`, so M is a superset of C. The budget
will not add them. M+ is a documented scratch variant for measurement only,
following the precedent of the 8 November area audit's scratch-commit fits; it
is not merged RTL.

Base revision: main at the commit the PR names. Main's 387 is slice 2 only
(state, interface and conversions, `22b1352a4`); it has no arithmetic.

### Envelopes

**C**: top `fes_i386_cpu`. One real clock input `clk`, fitter-assigned
location; every other port a virtual pin, with input and output delays
explicitly constrained to 0 as #1892 does. No board shell, no PLL, no HPS, no
reset-release IP. Identical on both devices.

**M / M+**: top `pc386_system`. Two real clock inputs, `clk` and `pixel_clk`,
declared as independent clocks and put in asynchronous clock groups, matching
`cores/fes-pc386/clocks.sdc`, where the two PLL domains are not timed against
each other. Every other port a virtual pin. The guest-rate parameters come
from the sealer's own `guest_rates()` at `SYSTEM_CLOCK_HZ`, and the BIOS and
VGA BIOS ROM contents are the firmware `firmware.lock` pins, built before the
fit. A missing `ROM_HEX` would let the fitter sweep the ROM and everything
downstream of it, so the runner refuses to fit without it.

Named exclusions in M: the two PLLs, `fes_hps_ddr` and its guard, the HPS
bridge, the HDMI I2C and audio output, and `top.sv` itself — the DE10 board
shell, which on the DE10 seal accounts for the difference between
`pc386_system` and the whole-core figure. A DE25 board shell is **not**
measured here and enters the budget as an explicit unmeasured line.

### The clock ladder

Reported Fmax at a single constraint is only an estimate, because fitter
effort follows the constraint. So the achievable clock is measured as a
ladder, precommitted here:

- **Headline rung: 61.44 MHz** on both devices — the pc386 seal's system
  clock. The headline area figures come from this rung.
- Further rungs: **75, 90, 110, 130 MHz**.
- Seeds **1–4** at the headline rung; seed **1** at the higher rungs.
- Fit C runs the whole ladder on both devices. Fits M and M+ run the headline
  rung only; a ladder for the machine is left unmeasured and said to be.
- **"Closes at" means the highest rung at which every analysis passes at every
  published corner** — setup, hold, recovery, removal and minimum pulse width,
  across all corners `get_available_operating_conditions -all` returns, the
  same gate #1892 used. Restricted Fmax at the slow corner is reported
  alongside each rung as the fitter's own estimate, clearly labelled as an
  estimate rather than as a closure.
- Runtime caps: 90 minutes per CPU fit, 4 hours per machine fit. A fit that
  hits its cap is reported as "not measured within the cap", not as a failure
  of the design.

No timing check is relaxed anywhere. A rung that misses is reported as
missing.

### What each fit reports

ALMs (the fitter's "Logic utilization (ALMs needed / total ALMs on device)"
line, quoted by name), ALUTs, dedicated logic registers, DSP blocks, M10K or
M20K blocks, logical block-memory bits — each with the device total and the
percentage of that device — plus the per-corner worst slack table and the
restricted Fmax. Raw fitter and TimeQuest reports are retained.

## The budget

The output is a budget, stated as one, not left for a reader to compute. Its
shape on each device:

- what the device has;
- what the machine takes, **measured** (fit M, which already includes the CPU);
- what the CPU alone takes, **measured** (fit C), as context for that line;
- what the identity-on machine takes, **measured** (fit M+ on Agilex 5);
- what is left, and against it these **explicitly unmeasured** lines, listed
  as unmeasured rather than as zero:
  - the DE25 board shell: HPS bridge, EMIF, scanout, audio, reset release;
  - the rest of the 387 beyond #1892's arithmetic backend — stack state and
    forwarding, conversions, retirement, unmasked exception results,
    remainder, scale, round-to-integer, transcendentals and BCD;
  - #1892's backend itself, 2,660 ALMs / 9 DSP / 4 M20K, which **is** an
    Agilex 5 figure from the same device and toolchain and so may be placed
    in the Agilex budget directly, as a floor for the arithmetic part only.

The Agilex 5 has 46,800 ALMs against the Cyclone V's 41,910 — about 12% more
cells by count, where the DE10 machine already sits near 88%. If the budget
comes out tight or negative it will be said plainly and early, because that
would change the DE25 plan again.

## Precommitted rules

- Every figure carries its device, toolchain version, flow, envelope and
  fit identity. No figure is quoted without them.
- No cross-device ALM ratio, in this record or in the PR body.
- A fit that fails is reported with its failure, and the resources it reached
  are labelled with the stage they came from.
- If a planned fit turns out not to be measurable, the record says so
  concretely and names what would be needed — a documented "this cannot be
  measured without X" is an acceptable result here.
- These rules are not replaced after seeing results. An amendment is dated and
  states what had already been seen.

## Amendment, 16 November 2026, still before any fit ran

Nothing had been fitted when this was written: no Quartus run of any kind had
started on either device. Four additions, three of them clarifications that
change no fit, and one correction to how the 387's two pieces may be added.

**1. Fabric and toolchain vary together, and this experiment does not
separate them.** Deano's question has two halves — "different fabric and
toolchain" — and the design varies both at once: Cyclone V through Quartus
17.0.2 against Agilex 5 through Quartus Pro 26.3. A difference between the two
sides is fabric *and* mapping quality together, and nothing here attributes it
between them. The total is what the DE25 decision needs, and the total is what
this measures; but a smaller Agilex figure would **not** establish a denser
fabric. Separating the two would need the same device through two Quartus
versions, which is not available.

**2. Area and clock are not independent, so no area figure is quoted without
its constraint.** Fitter effort follows the constraint, so a design fitted at
61.44 MHz and the same design fitted at 130 MHz are different fits with
different areas. Every ladder rung's area is reported next to that rung's
constraint, and the headline area figure names 61.44 MHz in the same sentence.

**3. That `pc386_system` needs no board shell is a property of the
hierarchy**, worth stating rather than leaving the next reader to assume a
machine fit needs a board: the HPS interface primitives, both PLLs,
`fes_hps_ddr`, the HDMI I2C and the audio output are all instantiated in
`top.sv`, above `pc386_system`, which takes `clk` and `pixel_clk` as plain
inputs and reaches DDR through ordinary Avalon-shaped ports.

**4. What M+ − M measures, and why it may not simply be added to #1892's
backend.** On main every x87 *arithmetic* form decodes to `X87_UNIMPL`; the
arithmetic exists only on #1879 and #1889. So M+ − M is the cost of **slice 2**
— the 387's state, its CPU and AT-board interface, its conversions, its
rounding and the shared 64x64 significand multiplier `fes_i386_x87_mul` — and
not the cost of a 387. It will be reported under that name.

It is tempting to read "M+ − M, plus #1892's 2,660 ALMs" as the 387's cost on
Agilex 5 in two independently measured pieces. **The two pieces are not
disjoint.** `fes_i386_x87_mul` is nine 22x22 DSP products summed over four
clocks, and #1892's backend carries its own nine 22/22/20-bit DSP products for
FMUL; a merged unit would share one multiplier, not build two. The same
applies to rounding, which both pieces implement. So the sum is an **upper
bound on those two pieces and a lower bound on a complete 387**, since neither
piece contains remainder, scale, round-to-integer, transcendentals, BCD or
unmasked exception results. It will be stated as both bounds at once and never
as "the 387's cost".

## Amendment, 16 November 2026: the one netlist difference between the sides

What had been seen when this was written: the first Agilex 5 synthesis of
`fes_i386_cpu` stopped with `Error (272006)` before elaborating anything, and
no fit had produced any resource or timing figure on either device.

The `SEAL` netlist is device-neutral in structure but **not** in one
attribute. The i386 and pc386 RTL asks for Cyclone V block RAM by name —
`(* ramstyle = "M10K" *)`, 30 occurrences in the CPU netlist and 57 in the
machine's — and Agilex 5 has no M10K: its `altera_syncram` refuses
`RAM_BLOCK_TYPE` of `M10K` outright, so synthesis stops before it elaborates.

So the Agilex side reads a netlist with exactly that token renamed to
`ramstyle = "M20K"`, the equivalent block on that device, and **nothing else**.
The DE10 side reads the netlist unchanged. The rename is verified by round
trip: turning `M20K` back into `M10K` must reproduce the original bytes
exactly, which proves both that nothing else changed and that the netlist held
no `M20K` request of its own. The occurrence count and both digests go in each
fit's record. `MLAB` requests are legal on both devices and are left alone.

The two sides therefore no longer read identical bytes, and the record must
not claim they do. What they read is the same netlist differing in one
attribute value whose only purpose is to name the device's block RAM. A reader
should treat the block-RAM figures as the weakest part of the comparison: M10K
is 10 Kb and M20K 20 Kb, they are not the same unit either, and the same
logical memory may land in a different number of them.
