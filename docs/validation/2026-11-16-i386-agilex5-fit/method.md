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
