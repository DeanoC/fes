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
  same gate #1892 used. (Corrected by a later amendment to **every analysis
  that has paths to analyse**: the CPU's resets are synchronous, so recovery
  and removal find zero paths there and are not applicable rather than passed.
  The rule's effect and every recorded verdict are unchanged.) Restricted Fmax at the slow corner is reported
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
M20K blocks, logical block-memory bits, and the fitter's virtual-pin count
where it gives one, plus the per-corner worst slack table and the restricted
Fmax. Each figure carries **the fitter report row it was read from**, and the
device total and percentage **for the rows where Quartus states a denominator**
— ALMs, DSP blocks, RAM blocks and block-memory bits. Quartus reports ALUTs
and dedicated logic registers as a used count with no device total, so those
two are reported as counts alone rather than given a percentage invented here.
Raw fitter and TimeQuest reports are retained.

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

## Amendment, 16 November 2026: the first netlist difference between the sides

*A second difference was found later the same day, before any machine fit had
produced a figure. It is in the "two more differences" amendment below, and
the complete list of what differs between the sides is there.*

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
not claim they do. What they read is the same netlist differing in this
attribute value, whose only purpose is to name the device's block RAM — and,
as the later amendment records, in one more rewrite found after this one. A reader
should treat the block-RAM figures as the weakest part of the comparison: M10K
is 10 Kb and M20K 20 Kb, they are not the same unit either, and the same
logical memory may land in a different number of them.

## Amendment, 16 November 2026: the host, and the virtual pins' own cost

What had been seen: one Agilex 5 synthesis error (the M10K attribute, above),
a passing Agilex 5 synthesis after the rename, and no resource or timing
figure from either device.

**Host.** The method said both sides would run on `chip-30x0-ai-dev`. Both
sides in fact run on **Powerboat**, which carries Quartus Pro 26.3.0 Build 114
*and* Quartus 17.0.2 side by side and was lightly loaded when the fits
started; running both there keeps the two sides on one machine rather than
splitting them. Each fit record names its host. A Quartus fit is determined by
its tool version, inputs and seed rather than by the machine, so this does not
change what the figures mean, but it is recorded rather than left to be
assumed. Note that the `/tmp/lander-quartus-3090.lock` lease is per host, so
on Powerboat it serialises only this session's own compiles.

**Virtual pins are not free.** The fitter builds each virtual pin in the
fabric, so part of a virtual-pin fit's ALMs is envelope rather than design.
`fes_i386_cpu` has **149** ports and the machine **43**, which the fitter
expands to 4,700 and 1,301 virtual pin bits. Each fit reports the fitter's own virtual-pin count
beside its ALMs, and that count is **never subtracted** from them: the ALM
figure is reported as it stands, with the envelope named so a reader can judge
it, and the same envelope is present on both devices.

**Fit C is not fit M's CPU share, and the record will not treat it as one.**
"M is a superset of C" is true of the hierarchy and not of the fitted logic:
C holds `x87_present` as a live input and every `trace_*` and `arch_*` output
alive on a virtual pin, including the 640-bit `arch_x87_regs`, while M ties
`x87_present` to 0 and leaves most of those outputs unconnected for the
fitter to sweep. C is the CPU with its full observability surface held up; the
CPU inside M is smaller than C by an amount this experiment does not measure.

## Amendment, 16 November 2026: two more differences between the sides

What had been seen: fit C had landed on Agilex 5 (29,114 ALMs, timing pass at
61.44 MHz), and the DE10 CPU fit and both machine fits had failed before
producing any figure. No DE10 figure and no machine figure existed.

**The DE10 fit was not the intended experiment, and nearly said so quietly.**
Quartus 17.0.2 rejects a braced node name in a `VIRTUAL_PIN` assignment
(Critical Warning 136021) and leaves that port a real pin. Every one of the CPU's assignments was ignored. The fit failed only because the device has 314
pins; on a device with pins to spare it would have produced a fit of a
completely different envelope and reported it as a result. The assignments are
now unbraced, and **every fit gates on the report's own pin count**: more real
pins than real clocks is a failure. The Agilex fit C already reported `Total
pins 1 / 351`, so its envelope was the intended one.

**The Agilex side drops one more thing from the netlist.** Yosys writes both a
bare and a valued `parallel_case` on some `casez` statements. A bare Verilog
attribute means the value 1, so the pair says one thing twice; Quartus 17.0.2
warns, Quartus Pro 26.3 stops with `Error (22074)`. The Agilex side removes a
bare `(* name *)` line when another line in the same attribute run gives
`name = ...` — 30 of the machine netlist's 650 bare `parallel_case`
attributes, the other 620 untouched — verified by putting the removed lines
back and requiring the original text exactly. Fork issue:
[DeanoC/yosys#78](https://github.com/DeanoC/yosys/issues/78).

So the Agilex side now reads the netlist with **two** mechanical rewrites: the
M10K block name and these duplicate attribute lines. The DE10 side reads it
exactly as the pc386 seal lane does. Both rewrites are recorded per fit with
their counts, both digests and a round-trip check, and the record will not
claim the two sides read identical bytes.

## Amendment, 16 November 2026: the envelope cuts virtual port paths

**This changes the envelope after a fit had already run under the old one, so
it is set out in full.** What had been seen when it was written:

- Fit C on Agilex 5 had completed under the **zero-delay** envelope: 29,114
  ALMs, timing pass at 61.44 MHz on all four corners, worst setup +6.048 ns,
  worst-corner restricted Fmax 97.77 MHz. In that fit the worst setup path and
  the worst register-to-register path were **the same path at every corner**,
  so the virtual port paths were not binding there.
- Fit C on the DE10 had failed, and not on capacity: 31,103 of 41,910 ALMs
  (74%), 90 of 553 RAM blocks, 1 real pin. It failed in the router —
  `Warning (16618): Fitter routing phase terminated due to routing congestion`,
  with `Info (170089): 2e+04 ns of routing delay ... has been added to meet
  hold timing` and `Info (188005)`: an excessive demand for routing resources
  caused by that hold padding.
- No machine fit had produced a figure on either device.

**Why the envelope was wrong.** A virtual pin is not a board pin: it has no
package, no board trace and no external device, so there is no delay to
constrain it against and no meaningful arrival or required time for it. Giving
one `set_input_delay`/`set_output_delay` of 0, with a `-min` as well, does not
make its path meaningful — it invents a **hold** requirement on a path to a
pin that does not exist, and the fitter pads routing delay to meet it. On the
Cyclone V that padding consumed the routing the design needed. The old form
established no board I/O contract, and neither does the new one.

**The new envelope.** Every path from a virtual input and every path to a
virtual output is cut with `set_false_path`. The clocks remain real inputs and
are not cut. What each fit analyses is the design's **own register-to-register
timing**, which is what this measurement is about.

**Consequences, and what is not being done here.** This is an envelope
correction, not a pass rule being swapped after a failure: no precommitted
pass rule changes, the all-corner setup/hold/recovery/removal/minimum-pulse
gate stands unchanged, and no rung's verdict is being revisited. But the two
sides must share one envelope, so **fit C on Agilex 5 is re-run under the new
envelope and both results are reported** — the zero-delay figures above are
kept in the record, not replaced, with the envelope each came from named. If
the two Agilex figures differ materially, that difference is itself reported
as the envelope's own effect.

## Amendment, 16 November 2026: LABs, and the envelope's own ALMs

What had been seen: fit C on both devices and fit M on Agilex 5 had all passed
at 61.44 MHz. Fit M+ had not run.

**The budget must be stated in LABs as well as ALMs, and the first reading of
it was wrong.** [#1889](https://github.com/DeanoC/fes/pull/1889)'s refusal was
a **LAB** count — 4,292 needed against the Cyclone V's 4,191, 102.4% — and the
first budget set a percentage of **ALMs** against it. LAB occupancy runs well
ahead of ALM occupancy, because ALMs sit sparsely across LABs until the fitter
is under pressure. Both fitters report LAB occupancy, so each fit now records
it with its device's LAB total, and the budget is stated in both units with
each one named. The Agilex 5 has 4,680 LABs against the Cyclone V's 4,191.

**The envelope's own ALMs are reported beside the design's.** Both fitters
count them: `ALMs containing virtual pins`. The CPU fits carry **2,351** such
ALMs on each device — its 149 ports become 4,700 virtual pin bits — and the
machine fits carry **651**, from 43 ports and 1,301 pin bits. (A later
amendment corrects an earlier port count here: a parser bug counted the
flattened module's Verilog function inputs as ports.) Those counts are reported next to each ALM figure and are
**never subtracted** from it, so every ALM figure in this record is the fit's
own total and includes its envelope. A real core would not have the CPU's
2,351, and would replace the machine's 651 with an actual board shell.

**Three labels the budget carries.** Any M+ − M difference is a single-seed
delta and is quoted against this lane's established ±200 ALM fit-to-fit noise.
The ~770 ALM DE10 board-shell figure comes from the 8 November area audit at a
different commit and is cited as that, not measured here. And the per-figure
device percentages come from the fitter's own denominators.

**Removed, not reinterpreted:** the `port_clock` table that constrained
pixel-domain ports against the pixel clock. Under the cut envelope every
virtual port path is cut, so which clock a cut port would have belonged to
changes nothing; the table was dead and is deleted rather than left as a shim.
The pixel domain's own register-to-register paths are analysed as the system
domain's are, and the record says the video output path is **not** analysed —
it is a virtual port, and no board I/O timing is established for it.

## Amendment, 16 November 2026: the secondary front end, and its prediction

What had been seen: fit C on both devices and fit M on Agilex 5 had passed at
61.44 MHz; fit M+ was in the fitter and had produced no figure. No
direct-SystemVerilog fit had been attempted on either device.

The method listed Quartus Pro reading the SystemVerilog directly as a
labelled secondary. It is now implemented, and the reason to run it has
sharpened: the sibling DE25 envelope session fits its x87 work through that
front end, so a figure from it is the only cross-check available between the
two records — without one, their ALM figures and these differ by front end as
well as by scope, which is two confounds rather than one.

**The prediction, stated before the attempt.** `fes_i386_l1i`, `fes_i386_l1d`
and `fes_i386_btb` carry **30** `(* ramstyle = "M10K" *)` requests between
them — 12, 16 and 2 — and Agilex 5 has no M10K, so Quartus Pro would refuse to
synthesise `fes_i386_cpu` from the RTL. That is the same refusal the netlist
flow hit, and it is why the primary works: the `SEAL` recipe's memory policy
and this runner's rename deal with those memories before Quartus sees them.

So the secondary applies **the same M10K-to-M20K rename to the RTL sources**
that the primary applies to the netlist, with the same round-trip check and
the count recorded, and copies every other source byte for byte. That keeps
the secondary a test of the front end rather than a second experiment. What
remains genuinely open, and is what the attempt tests, is whether Pro's
SystemVerilog front end accepts the rest of this RTL where Quartus 17.0.2
rejects it — the genvar scoping and the variable index into a packed struct
member that the DE10 lane documents.

**How its result will be read, fixed before the run.** If it elaborates, its
**register count** is compared with the primary's on the same device; a large
gap means swept or mis-elaborated logic and the fit is **withdrawn as an area
result** rather than reported as a second figure. If it refuses, the record
says so concretely with the error and the construct, as a "not measurable
without X" with X named. Either outcome is reported; neither replaces the
primary.

## Amendment, 16 November 2026: which row is the capacity figure, and the
## secondary's threshold

What had been seen: fits C (both devices), M and M+ had all passed at
61.44 MHz. Fit M on the DE10, the ladder, the extra seeds and the
direct-SystemVerilog secondary had produced nothing. Two budget readings had
already been sent to the coordinating session, the second of which this
amendment retracts.

**`Total LABs: partially or completely used` is not the capacity figure, and
is not what #1889 refused on.** The checks:

- A LAB holds exactly 10 ALMs on **both** devices (46,800 / 4,680 and
  41,910 / 4,191), so a LAB percentage and an ALM percentage are not two views
  of the same occupancy — they differ by how densely the fitter packed.
- M+ places 40,550 ALMs across 4,590 LABs: **8.1 of every 10** ALM slots, and
  the fitter's own estimate is that **4,210 ALMs are recoverable by dense
  packing** (9% of the device) with 915 unavailable. The LAB row counts a LAB
  as used when one ALM in it is used.
- The same CPU netlist used 3,512 LABs under one envelope and 3,626 under the
  other, a 114-LAB swing — larger than the 90 LABs the second budget reading
  called the remaining room.
- [#1889](https://github.com/DeanoC/fes/pull/1889) reports **LABs *required*,
  4,292 of 4,191**, before placement, and states that neither fit placed so
  there is no ALM figure at all. The fit reports in this record have no
  "LABs required" row; the row they have is a post-placement fact. **The two
  are different quantities and this record will not put them on one scale.**

So the capacity figure is Quartus's own: **`ALMs needed [=A-B+C]`** — ALMs
used in final placement, less what dense packing would recover, plus what is
unavailable — which is the row the `Logic utilization` percentage is computed
from. The budget is stated in that row. LAB occupancy is reported beside it as
what it is: how sparsely the fitter spread the design, not how full the device
is.

**What remains comparable with #1889 is not a percentage but an outcome.** On
the DE10 the identity-on machine required more LABs than the device has and
never placed. On Agilex 5 it placed, routed and closed timing at every corner.
That is the comparison this record makes.

**The secondary front end's threshold, fixed before its fit prints anything.**
The comparison quantity is **dedicated logic registers**, against the primary
`cpu-de25` fit on the same device at the same constraint and seed (20,842).
The threshold is **±5%**, that is 19,800 to 21,884. Inside it, the
direct-SystemVerilog fit is reported as a labelled secondary cross-check.
Outside it, it is **withdrawn as an area result** and reported only as a
front-end difference, because a gap that large means swept or mis-elaborated
logic. ±5% is chosen because the primary's own register count moved by 2,182
(10.5%) between the two envelopes on identical input, so a threshold tighter
than the known envelope sensitivity would be meaningless, and one looser than
this would not catch a swept block.

**An addition, not a relaxation:** extra seeds of M and M+ were queued beyond
the precommitted set (which gave the machine the headline rung at seed 1
only), because the budget rests on those two fits. More evidence at the same
rung; no rule changed.

## Amendment, 16 November 2026: M+ is not the design the DE10 refused

What had been seen: fits C (both devices), M and M+ had passed; fit M on the
DE10, the ladder, the extra seeds and the secondary had produced nothing. A
headline reading "the DE25 holds the CPU, the machine and main's 387 together,
where the DE10 does not" had been written into the record and the PR body and
sent to two sessions. This amendment retracts it.

**`main`'s 387 is one the DE10 holds.** It is slice 2 — state, CPU and
AT-board interface, conversions, rounding and the shared significand
multiplier — and **every arithmetic form on main decodes to `X87_UNIMPL`**.
[#1821](https://github.com/DeanoC/fes/pull/1821) sealed exactly that on the
DE10: 36,298 of 41,910 ALMs, every analysis positive over all four corners,
0 ns TNS, with a kit run on the seal.

**[#1889](https://github.com/DeanoC/fes/pull/1889)'s refusal is a different
design.** It is slice 4 stacked on slice 3, whose 387 has divide, square root
and the remainders: 8,073 ALMs for the standalone unit against its merge
base's 4,076 on the DE10. Its identity-on machine required 4,292 LABs of
4,191 and never placed.

So M+ — `main` plus a one-character flip — measures a 387 the DE10 already
carries, and this record establishes **nothing** about whether the DE25 holds
the one the DE10 refused. The headline is withdrawn from the record and the PR
body. What M+ establishes is exactly this: the machine with slice 2's 387
takes 79.6% of the Agilex 5's ALMs and closes 61.44 MHz at every corner.

**The measurement that would settle the DE25 plan is now named as not made:**
`pc386_system` from #1889's head, through this runner, on Agilex 5, with
`x87_present` tied high. It is offered to the coordinating session rather than
run here unasked, because it is a different base from the one this task and
this method named, and because the two sessions working on #1889's lane may
want it on their own terms.

## Amendment, 16 November 2026: the machine netlist's digest was path-dependent

What had been seen: fits C (both devices), M and M+ had passed; the DE10
machine fit, the ladder and the secondary were queued and had produced
nothing. This amendment adds a finding; it changes no figure.

Regenerating the fitted netlists to put their Yosys provenance in the record
showed that **the machine netlist's content depends on the paths given to
Yosys**, not only on its sources. Byte-identical sources and the same locked
Yosys, elaborated at different output directories, gave four different
digests; three runs into the *same* directory gave the same digest every
time, so this is path dependence and not run-to-run nondeterminism. Fixing
only the `write_verilog` argument did not stabilise it — the program file and
the log path matter too.

**The designs are the same.** All four netlists are the same size
(6,024,354 bytes), the same 103,549 statements counted by kind, and carry the
same 151,753 `_NNNNN_` references; they differ only in which numbers those
auto-generated names hold, 4,808 lines of them. That is the phenomenon
[#1821](https://github.com/DeanoC/fes/pull/1821) measured for comment-only
source changes, where it moved a Quartus fit by 25 ALMs — well inside this
lane's ±200 ALM noise, and far inside the 1,763 ALM M+ − M delta.

The CPU netlist is unaffected, because `yosys_netlist.seal_netlist` passes a
bare filename where the machine's caller passes an absolute path. The CPU
netlist reproduced byte for byte (`3e485e23…`).

**What this changes.** The runner now elaborates the machine at a fixed
netlist, script and log path and copies the result into each fit's directory,
so a fit's netlist digest is reproducible: two output directories 24
characters apart in length now give the same digest. Fork issue:
[DeanoC/yosys#80](https://github.com/DeanoC/yosys/issues/80), with the repro.

**What it does not change.** The machine fits already run were fitted before
this, so **a fresh elaboration will not reproduce their digests**, and the
record says so rather than implying a reproducible identity it does not have.
Each fit's own `inputs.json` records the digest of the exact bytes that were
fitted, which is the authority for what those figures came from, and the
equivalence check above is why the figures stand.


## Amendment, 16 November 2026: the port counts were wrong, the envelopes were not

What had been seen: all six fits had passed at 61.44 MHz; the ladder, the extra
seeds and the secondary had produced nothing.

A review finding noted that fit M records 1,991 virtual ports and fit M+ 2,168,
although their only source difference is one internal tie — which would make
the 1,763 ALM M+ − M delta uncontrolled. **The finding was right about the
numbers and the numbers were wrong.**

Yosys writes the flattened module's `$pmux` helpers as Verilog **functions**,
and a function declares its own inputs. The port reader counted those as
ports: about 1,950 of the machine's "ports" and most of the CPU's were
function-local inputs, not nodes. The real counts are **149** ports for
`fes_i386_cpu` and **43** for `pc386_system` — and M and M+ have the **same 43,
with no difference at all**, so the envelope was never different between them.

**The envelopes were identical, and the fitter says so independently.** Both M
and M+ report **2 real pins** and **651 ALMs containing virtual pins**. The
CPU fits report 1 real pin and 2,351 such ALMs on both devices. So no figure
in this record changes: the 1,763 ALM delta is between two fits with the same
envelope, and the pin gate passed on every one of them because the real ports
were always in the assignment list.

What the bug did cost: the extra assignments named nodes that do not exist.
Quartus matched nothing to them and reported nothing about them — zero ignored
assignment warnings in any fit — so they were inert. The published port counts
were simply wrong, and are corrected here and in the summary. The reader is
fixed to skip `function`/`endfunction` blocks, with a test that puts a
function's inputs in a module header and requires them not to appear.

## Amendment, 16 November 2026: the ladder's top rung was not high enough

What had been seen: **every rung of the precommitted ladder closed on
Agilex 5** — 61.44, 75, 90, 110 and 130 MHz, each passing setup, hold,
recovery, removal and minimum pulse width at all four published corners. The
130 MHz rung passed with **+0.426 ns** of setup slack at its worst slow corner
and a 137.63 MHz restricted-Fmax estimate. Area was flat across all five:
29,300 to 29,455 ALMs, a 155 ALM spread. On the DE10, only the 61.44 MHz rung
had run; its higher rungs and the extra seeds were queued.

So the ladder did its job on the pass side and failed to bound the answer: it
establishes that `fes_i386_cpu` **closes at 130 MHz on Agilex 5**, and it does
not establish a ceiling, because its top rung was the last one it had.

**The ladder is extended to 150 and 170 MHz, seed 1, on Agilex 5.** This is
stated as what it is: rungs added **after** seeing the top rung pass. It
changes no pass rule — a rung still closes only by passing every analysis at
every corner, and the rungs already run keep their verdicts — and it adds
evidence in the same direction rather than rescuing a failure. The reason to
do it rather than report "at least 130 MHz" is that the DE25 plan needs to
know where the CPU actually stops, and 130 MHz with +0.426 ns of slack is
plainly not it.

If 170 MHz also closes, the record will say so and say again that the ladder
has not found the ceiling, rather than extending a third time; two additions
chasing a result would stop being a precommitment at all. The DE10's rungs
above 61.44 MHz are unchanged and still expected to miss against that device's
own 63.79 MHz estimate.

## Amendment, 16 November 2026: the right evidence that the DE10 holds slice 2

What had been seen: all fourteen fits were complete and recorded, and the
record and the PR body said the DE10 holds `main`'s 387, citing
[#1821](https://github.com/DeanoC/fes/pull/1821)'s **seal** at 36,298 of
41,910 ALMs. That citation is wrong and is corrected here; the amendment it
corrects is left as it was written.

#1821's sealed package has the identity **off** — `main` ties `x87_present` to
0, which is the same tie fit M has — so 36,298 ALMs is the DE10 equivalent of
**fit M**, not of M+, and it shows nothing about the identity being on. The
right evidence is in the same PR and is stronger: its **identity-on machine
fit ran nine seeds at 38,075–38,257 ALMs, 90.9–91.3% of the device, every one
meeting setup and hold, worst +0.454 ns**. That is what establishes the DE10
holding slice 2's 387, and it is what the record now cites.

Two things that come with it, both from #1821 and both cited rather than
measured here. It brackets the onset of seed-dependence on the DE10 **between
91.3% and 91.7%**, which is a better-sourced version of the 8 November audit's
"anything past about 92% is untested". And its per-entity audit puts the
identity's whole cost on the DE10 at `fes_i386_x87` 1,414 + `fes_i386_x87_mul`
178 + `fes_i386_cpu`'s own logic 143 ALMs, with both x87 entities at 0.0 ALMs
when the identity is off — which is the DE10's own measured version of the
quantity M+ − M measures on Agilex 5. **The two are not divided or compared
here**: different device, different toolchain, and this record's whole rule is
that such a ratio is not available.

## Amendment, 16 November 2026: the gates require the exact clock and pin count

What had been seen: all fourteen fits were complete and recorded, with every
one evidencing one real pin (CPU) or two (machine) and zero illegal and zero
unconstrained clocks.

Two gates accepted **too few** where they should have required exactly the
intended number, and the machine's two-clock envelope is where that matters:

- The pin check required `real_pins <= real clocks`. One real pin on a
  two-clock design means a clock input went virtual or was removed.
- `fit_timing.tcl` required **at least one** clock. So the surviving clock's
  clean coverage rows would have let such a fit be recorded as covered, with
  the other clock never analysed at all.

Both now require the exact count: `real_pins == len(real_clocks)`, and the
gate takes the intended clock list in `FES_FIT_CLOCKS` and errors unless
TimeQuest finds exactly that many. The new gate was **run** against a retained
project rather than assumed: with `FES_FIT_CLOCKS=clk` it passes the CPU fit
across all four corners, and with `FES_FIT_CLOCKS="clk pixel_clk"` on the same
project it stops with `expected exactly 2 clock(s), found 1`.

No recorded figure changes. Every recorded fit already satisfies the stricter
rule — the CPU fits have one real pin against one real clock and the machine
fits two against two — which is why this is a gate correction rather than a
result correction.

## Amendment, 16 November 2026: a sixteenth fit, and its bands before it runs

What had been seen: the fifteen fits above, all recorded, with the budget and
the ladder settled. **And, from the sibling DE25 envelope session, something
that changes this fit's purpose:** its integration fit of `pc386_system` with
`x87_present = 1'b1` **and** `X87_BACKEND = 1` — the machine with a *complete*
DE25 387, the pipelined backend — fits and closes on Agilex 5 across four
seeds at **40,986 / 41,055 / 41,122 / 41,054 ALMs, 87.58–87.87%**, every
corner, hold strictly positive, setup never below +7.58 ns. It is reported to have run on this
runner, this envelope, this SDC and these report rows, so it should be on the
same scale as M and M+.

**Those figures were relayed in a message and are not verified here.** At the
time of writing they appear in no committed artifact that could be found, on
that session's PR [#1907](https://github.com/DeanoC/fes/pull/1907) or
elsewhere, and none of its fit output was read. They are cited as that
session's report. Nothing measured in this record depends on them.

**So the question this record kept naming as unsettled is settled, by a
different design.** "Does the DE25 hold a complete 387" is answered **yes**,
via the pipelined backend — which is what the 16 November decision ships
there. Every sentence in this record that calls #1889's head "the only
measurement that would settle the plan" is wrong as of that result, and the
summary is corrected rather than left standing.

**What the sixteenth fit is for instead.** `pc386_system` from
[#1889](https://github.com/DeanoC/fes/pull/1889)'s head, `x87_present` tied
high, is now the clean **A/B of the two arithmetic implementations on one
device**: the DE10's sequencer design against the pipelined backend, same
device, same envelope, same front end, same rung, with the backend side
already measured at four seeds. Nobody has that comparison, and slice 6 has to
decide what to optimise. It is run for that, and labelled as that.

**What is fitted.** #1889's head `5f901426e` **as it stands, pre-merge**: that
PR targets `deano/i386-x87-s3`, not main, and its base is currently
conflicting. The tree wanted is the head's own, not a merge result, so no
merge is attempted. Consequently the fit describes the **slice-3-plus-slice-4
design on #1879's branch** and no state of main. Envelope, SDC, virtual-pin
set, guest rates, ROM contents, report rows and the M10K rename: **identical
to M and M+**, because being on one scale with the 37,255 is the entire point.

**Interpretation bands, fixed here before any output exists.**

1. The fitter refuses, or ALMs needed exceed 100% → **does not fit on Agilex 5
   either**: a decisive negative.
2. Fits, ALMs needed ≤ 100%, timing **passes every analysis at all four
   corners** → **Agilex 5 holds the design the DE10 refused**: the decisive
   positive.
3. Fits, but timing **fails at any corner** → **"fits but does not close"**,
   which is **not** a pass; the worst slack and its corner are reported.
4. In band 2 or 3 with ALMs needed **≥ 92%** → recorded as **not established,
   pending more seeds**, whatever the timing says. The DE10 lane's practical
   ceiling is near 92%, #1821 brackets seed-dependence onset there between
   91.3% and 91.7%, and nothing above 79.6% of ALMs had been fitted on Agilex
   5 before the envelope session's 87.87%.

**No outcome is predicted.** The inference that would have predicted a refusal
has been withdrawn, and the honest position is that nobody knows. Stating a
prediction now, after a morning spent retracting two of them, would be the
same mistake in a new place.

## Amendment, 16 November 2026: the closure rule means every *applicable* analysis

What had been seen: all sixteen fits recorded, including #1889's head, and the
summary already corrected to say that recovery and removal are inapplicable on
the CPU fits rather than passed.

The closure rule above still reads "every analysis passes at every published
corner — setup, hold, recovery, removal and minimum pulse width". Taken
literally that rule could never be met by `fes_i386_cpu`, whose resets are all
synchronous: `fit_timing.tcl` finds **zero** recovery and removal paths there
and records them `not_applicable_synchronous_reset`. So the rule as written
would have failed every CPU rung, including the 130 MHz closure it was used to
establish.

**The rule, stated as it was actually applied and as the gate implements it:**
a rung closes when **every analysis that has paths to analyse** passes at
every published corner, and an analysis with zero paths is recorded as not
applicable rather than counted as a pass. On `fes_i386_cpu` that is setup,
hold and minimum pulse width; on `pc386_system`, which has asynchronous
resets, it is all five. The gate has always distinguished the two — it writes
a coverage word per check per corner, and `not_applicable_synchronous_reset`
only where the path count is zero — so no verdict in this record changes. What
changes is the method's wording, which overstated what had been required.

This is a wording correction to a precommitted rule, which is worth flagging
as such: the rule's *effect* is unchanged, every recorded verdict stands, and
the coverage words in [timing.tsv](timing.tsv) let a reader check that for
themselves rather than take this paragraph's word for it.
