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
- M+ occupies 4,590 LABs, which hold 45,900 ALM slots, and the two rows give
  two densities — **8.83 of every 10 slots** from the 40,550 ALMs used in
  final placement, **8.12** from the 37,255 ALMs needed. (An earlier version
  of this line paired the 40,550 with the 8.12, which is the same
  mis-attachment the record retracted elsewhere; corrected here.) The gap
  between the rows is the fitter's own estimate that **4,210 ALMs are
  recoverable by dense packing** (9% of the device) with 915 unavailable. The
  LAB row counts a LAB as used when one ALM in it is used.
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

## Amendment, 16 November 2026: a design with asynchronous resets must show them

What had been seen: all sixteen fits recorded; the closure rule corrected to
"every applicable analysis".

That correction opened a hole the previous wording had closed by accident. The
gate labels **any** zero-path recovery or removal check
`not_applicable_synchronous_reset`, and the rule now accepts that as not
applicable. But `pc386_system` **has** asynchronous resets, and all four
machine fits analysed those checks. If a machine fit's asynchronous resets
were swept or mis-elaborated, their paths would vanish, the gate would label
them not applicable, and the rule would accept it — hiding exactly the kind of
loss the gate exists to catch.

So the runner now declares, per design, whether the design has asynchronous
resets — `pc386_system` yes, `fes_i386_cpu` no — and the gate **errors** on a
zero-path recovery or removal check for a design declared to have them,
instead of recording it as not applicable. The guard was **run both ways**
against a retained CPU project rather than assumed: with
`FES_FIT_ASYNC_RESET=0` it passes all four corners, and with
`FES_FIT_ASYNC_RESET=1` on the same project it stops with "this design is
declared to have asynchronous resets: they were swept or mis-elaborated".

No recorded verdict changes: all four machine fits already have analysed
recovery and removal rows at every corner, worst +0.302 ns, and the CPU fits
are declared synchronous, which is what they are.

## Amendment, 16 November 2026: a CI failure that is not this work's

For the record, because a reader of the PR will see a red check in its
history. The `fpga-simulation (de25-x87, sim-fes-de25-x87)` job failed twice on
this branch and then passed, by **crashing Verilator itself** during
elaboration — `corrupted size vs. prev_size in fastbins` on one run and
`Verilator internal fault, sorry` on the next, two different signatures on
byte-identical input, each inside 19 seconds and before any simulation ran.

It is not caused by this work, and that is checkable rather than asserted:
`git diff origin/main...HEAD` over every input that job uses —
`boards/de25nano/x87/`, `sources/misteross/Makefile`, `.github/workflows/` and
`scripts/ci_simulations.py` — is **empty**. This branch's diff is the record
under `docs/validation/2026-11-16-i386-agilex5-fit/`, three new scripts and
their two tests, and nothing else. The job passes on `main`, and passed on
this branch on the following run with the same inputs.

Filed as [#1925](https://github.com/DeanoC/fes/issues/1925) with the run links
and both signatures. **One claim in the first version of that issue and of this
amendment was wrong and is corrected here:** I wrote that this lane's Verilator
is workflow-provisioned rather than pinned. It **is** pinned —
`scripts/ci_verilator.sh` fixes it to commit
`8ff77e9d47351b0a59114929880687839a51840b`, Verilator v5.032. That makes the
crash *more* reportable, not less: it is a specific, known version faulting on
specific input, which is what an upstream report needs. Recorded here rather
than left for someone to attribute to this PR.

## Amendment, 2026-11-16: `unknown` is not a revision

Written after every fit in this record had been collected and the figures
published, so it changes no figure here and could not: it closes a hole that
this record happens not to fall into.

`fes_revision()` returns `{"commit": "unknown"}` when `git rev-parse` fails,
which is what a tree copied to another host without a resolvable `.git`
produces. `"unknown"` is a truthy string, so the collector counted it as a
recorded revision: a record whose fits all came from such a copy would have
reported `fes_revision: "unknown"` under the note *every fit was prepared from
one revision*, which reads as agreement about provenance where there is none.

The hole was reported to me by the sibling session fitting the x87 backend,
which hit it directly — all four of its seeds recorded `unknown`, from a tree
copied to the chip host, and the "fatal: not a git repository" line in the log
was read past as cosmetic. I built the counterexample before changing anything:
one fit with `revision="unknown"`, which the collector reported as the agreed
revision. The collector now normalises `"unknown"` to absent, so such fits land
in `fits_with_no_recorded_revision` and set no aggregate.

This record's own `host.json` is unchanged by it. Its ten fit revisions are
real commits; the four fits listed in `fits_with_no_recorded_revision`
(`cpu-de10`, `cpu-de25`, `cpu-de25-zerodelay-envelope`, `machine-de25`) predate
the field entirely and carried no `fes_revision` key, which is a different and
already-reported absence.

## Amendment, 2026-11-16: the second half of the same hole, and one definition

Written after the previous amendment was published, and about that amendment's
own incompleteness rather than about any figure here.

The previous fix taught *one* consumer that `"unknown"` is the absence of a
revision and left the other checking truthiness. `--netlist-source self` stores
`fes_revision()` whole, so on a copied checkout the reused-netlist gate saw the
truthy dict `{"commit": "unknown", "clean": None}`, recorded no
`netlist_source_revision` error, and would have published the fit as having
stated where its netlist's bytes came from when it had stated nothing. The same
failure the first fix was for, reached by a second path. Found by Codex review
on `87321ade8`, not by me.

Counterexample first, again: a reused netlist whose `source_revision` is that
dict, and one whose `source_revision` is the bare string, both published with
no validation error before the change. A paired test fixes the gate as a
revision check rather than a shape check — `--netlist-source self` on a real
checkout stores a dict too, and that must still pass.

The fix is one definition instead of two tests. `revision_commit()` is now the
only place that decides whether a recorded revision names a commit, and both
consumers call it. The producer's note is qualified to match: a `self`
declaration from a checkout that cannot name its own revision now says so
rather than reading "declared as elaborated from this checkout" unqualified.

The lesson recorded rather than the patch: two independent sites treating
`"unknown"` as present was not two bugs, it was one missing definition. A
truthiness check replaced by a slightly different truthiness check would have
looked fixed.

## Amendment, 2026-11-16: per-clock timing coverage, and a relayed signal

Both items were found by Codex review on `0b4820867`, after every figure in
this record was published. The first bears on those figures and is resolved
against them below; the second does not and is operational.

### A clock object is not a clocked domain

`fit_timing.tcl` required that the SDC's clock *count* match
`FES_FIT_CLOCKS`, and that design-wide setup and hold queries return paths at
every corner. Neither notices a design that keeps both clock *ports* -- so both
`create_clock` objects exist and the count gate is satisfied -- while one
domain's registers are swept or mis-elaborated: the design-wide queries are
answered by the surviving domain, and the fit publishes as analysed at every
corner having analysed one domain. The script's own comment claimed the count
gate prevented exactly this. It did not.

Counterexample first, as with the revision hole. `fit_timing.tcl` runs against
a stubbed STA API (`tests/quartus_sta_stub.tcl`), which describes a design
rather than fitting one; the alternative is sweeping a domain out of
`pc386_system` and spending forty minutes watching the script wave it through.
Presented with two clocks and one clocked domain, the published script exited 0
and announced *setup/hold/minimum_pulse_width across all 2 published corners*.
It now errors, naming the clock and saying the design-wide analysis was
covering it from another domain. Nine tests hold the gate's claims, including
the refusal on a hold-only corner (where it must come from hold, there being no
setup model) and the single-clock CPU case.

**The fits already published did analyse both domains**, confirmed from
retained evidence and not assumed: every machine fit reports a finite
restricted Fmax for both `clk_clock` and `pixel_clk_clock` at every published
corner (8 corners on the Cyclone V, 4 on each Agilex 5 fit), and the fitter
derives a clock's Fmax only from paths within that clock's domain. Recorded in
the summary beside the Fmax table, where a reader meets the figures.

### A lease released while the work it protects is still running

The runner starts each Quartus stage with `start_new_session=True`, which is
what lets it kill the whole compiler group deliberately -- and also means that
group never receives a signal the runner gets. Python's default action for
SIGTERM and SIGHUP exits without raising, so the `except BaseException` cleanup
never ran: a job timeout or a disconnected session killed the runner, the
builder lease died with it, and Quartus kept compiling. The next fit then took
the lock and overlapped an orphaned compile, which presents as inexplicable
figures rather than as an error -- and the sibling x87 session fits through
this same runner, so it was exposed too.

Counterexample first again, and kept as a test in both directions: a stage
whose child outlives its SIGTERM'd parent, and the same child not outliving it
once `terminating_signals()` turns the signal into `FitTerminated` and the
existing cleanup unwinds. The bare case is retained as a test precisely so the
fix is shown to be what changed the outcome rather than asserted to be.

This is the same shape as the lease findings in
[#1850](https://github.com/DeanoC/fes/pull/1850) -- a lease released while the
work it protects continues -- now found independently in two places in this
repository. That it took a reviewer rather than the lease's author to see it
here is the pattern, not the exception.

## Amendment, 2026-11-16: a narrowed window is not a closed one

The third round on one question, and the second time in this repository that a
fix for a lease finding introduced the next one. Found by Codex review on
`d8764bb91`, after every figure here was published; no figure moves.

Relaying SIGTERM and SIGHUP did not close the hole, it shrank it. A guard
cannot be entered before the child it guards exists, so `Popen` returning and
`try` being entered are separated by a few instructions, and a signal landing
there raises outside the cleanup: the runner exits, the builder lease dies with
it, and the Quartus group lives on. Identical in kind to the failure the relay
was written for.

Counterexample first, and this one is not raced for. Rather than firing signals
and hoping one lands in a two-instruction window, the signal is raised *at* that
point deterministically -- after `Popen` returns and before the `try` -- which
is the window's definition rather than an approximation of it. Unblocked: the
child survives its parent. Blocked: no survivor, and the cleanup runs. Both
directions are kept as tests.

The fix is not a smaller window. `signal.pthread_sigmask` makes the signals
undeliverable across the launch, and a blocked signal stays **pending**, so it
is raised by the release inside the region that owns the pid. There is no
interval in which it can be lost, as against an interval made short enough to
be unlikely. The alternative shape -- moving `Popen` inside the `try` -- was
rejected for that reason: it moves the window rather than removing it, since the
`except` would still have to name a `process` that may not exist yet.

Two things noted rather than left as anecdote:

**The hang was the evidence.** The first attempt at this counterexample appeared
to hang. It had not: the orphaned child inherited the pipe, so `tail` never saw
end-of-file. The symptom of the bug was mistaken for a failure to reproduce it,
which is how an orphan-holding-a-handle bug hides in any harness that reads a
pipe.

**On the family, not the instance.** This is the same pattern as
[#1850](https://github.com/DeanoC/fes/pull/1850), where five consecutive rounds
went on one question and the fourth finding was introduced by the fix for the
third. What ended it there was not a better guard: the lease handling came out
of the cleanup path entirely, leaving less code than the guards it replaced.
That account is the coordinating session's, and this repository's commits on
that PR are consistent with it. The transferable rule is the one this round
earns: **a fix that narrows a window is not a fix that closes it**, and the
question to ask of the second version is whether a structure exists in which the
window cannot be, not whether the window is now small.

## Amendment, 2026-11-16: the child's mask, and where this family ends

The third finding in the lease family, from Codex review on `904bddcb2`, and a
consequence of the previous fix rather than an independent fault. No figure in
this record moves.

Blocking the terminating signals across the launch closed the parent's window
and handed the blocked mask to the child, because a child inherits its
launching thread's mask across fork and exec. Quartus therefore started with
SIGTERM blocked: the cleanup's polite SIGTERM could not be received, so every
termination waited out the full 15-second grace and ended in SIGKILL mid
compile. Quieter than an orphan, and not what the cleanup was designed to do.

This counterexample needed no race at all, which is worth noting beside the
previous one: the property is readable directly from the kernel. `SigBlk` in
`/proc/<child>/status` showed bits set for SIGTERM and SIGHUP. Both are now
clear there, **while the parent is still blocked at the same moment** -- the two
assertions are made in one test, because either alone is satisfiable by
breaking the other. A behavioural test sits beside them: a stage dies on the
cleanup's SIGTERM in under ten seconds rather than after the grace.

The reset happens in the child as part of creating it -- `preexec_fn`, between
fork and exec -- rather than afterwards, which is too late by definition. That
runs in the forked child where only async-signal-safe work is sound, so it is
one `pthread_sigmask` call and nothing else. It rests on the runner being
single-threaded at launch, which it is: stages start from the main thread and
the elaboration heartbeat is stopped and joined before any compiler stage. The
code checks that rather than trusting it, and says so in the log if it ever
stops holding. `os.posix_spawn` with `setsigmask` would avoid the caveat
without Python running in the child, but it cannot set the child's working
directory, which every Quartus stage needs.

**Where this family ends.** Three findings, each from the fix before it, is the
#1850 pattern, and the test of whether it has ended is structural rather than a
count of rounds. It has: the parent's mask and the child's are now set at the
same moment, by one call, as two halves of creating a process -- not one
patched after the other. There is no third party left to inherit anything, no
interval between the two settings, and nothing deferred to a later step that a
signal could arrive before. The rule from the previous round still stands as
the one to apply next time: a fix that narrows a window is not a fix that
closes it.

## Amendment, 2026-11-16: the invariant, and every path that releases the lease

A fourth finding in the signal family, from Codex review on `16476dd16`, in the
cleanup rather than the launch. No figure in this record moves.

The previous amendment said the family "ends here" and that a fourth finding
would mean the claim was wrong. Scoped precisely: the claim made was about the
launch -- both masks set by one call -- and that still holds and its tests still
pass. What was wrong was treating the launch as the whole of the problem. The
invariant is wider and is now stated as the invariant, which it should have been
from the first round:

> **The builder lease is never released while a process group this runner
> started has a live member.**

The launch was one of the places it could be violated. The cleanup was another:
with a stage that ignores or is slow to honour SIGTERM, a second SIGTERM or
SIGHUP raised `FitTerminated` again inside the grace wait, the exception left
before SIGKILL was sent, and the runner exited over a live compile.

Counterexample first, deterministic, and judged by surviving PIDs rather than
by a pipe reaching end-of-file: a stage that traps SIGTERM and SIGHUP, the
runner sent SIGTERM, then SIGHUP during the grace. Runner gone, stage alive.
The old cleanup is kept in the tests as the failing shape.

Two changes, of different kinds:

1. **The reap cannot be left early.** `reap_group` blocks the terminating
   signals (and SIGINT) for its whole duration, so new ones stay pending;
   `pthread_sigmask` itself runs any handler tripped before the block took
   effect, and any exception that still arrives is caught and the reap resumed.
   A flag making the handler a no-op was not used: it has a window between its
   check and its set. The polite SIGTERM is sent once and the grace measured
   from the first attempt, so interruptions cannot extend it. Emptiness is the
   *group's*, not the leader's.
2. **The invariant is checked where the lease is released**, not trusted to
   each path. Every started group is registered while the launch still has the
   signals blocked and deregistered only when seen empty; `compiler_lease`'s
   release empties anything still registered, loudly, before unlocking. This is
   the part that does not depend on having enumerated the paths correctly.

The release paths, and the test that holds each (`tests/test_fit_i386_device.py`):

| Path to the lease's release | Test |
| --- | --- |
| Normal exit, group already empty | `test_a_reaped_group_is_no_longer_registered_and_the_mask_is_restored` |
| Normal exit, leader gone but members left behind | `test_a_stage_that_leaves_members_behind_is_emptied_on_normal_exit` |
| A stage fails (exception) | `test_a_reaped_group_is_no_longer_registered_and_the_mask_is_restored` |
| The runtime cap expires on a stage ignoring SIGTERM | `test_the_runtime_cap_empties_the_group_before_the_runner_leaves` |
| A termination signal during a stage | `test_a_relayed_sigterm_takes_the_stage_group_with_it` |
| A signal in the launch window | `test_a_blocked_launch_has_no_window` |
| A second and third signal during the grace | `test_a_second_signal_during_the_grace_cannot_leave_the_reap` |
| A path that performs no cleanup at all | `test_the_lease_release_empties_a_group_a_path_left_alive` |

**Not covered, and not coverable from inside the runner:** SIGKILL of the
runner itself, or the host going down. Nothing runs, the kernel drops the
`flock`, and the stage's group lives on. Only something outside the runner --
the next lease holder refusing to start while a previous group is alive, or the
operator's stop order of remote group first -- can hold the invariant there.
Stated as a limit rather than left to be found.

**Also not tested here:** the machine elaboration's own lock. Yosys is started
by `scripts/yosys_netlist.py` through `subprocess.run`, which kills its direct
child when an exception passes through it, and the relayed signals are now
exceptions; that is the standard library's behaviour and this record has not
constructed a counterexample against it.

No claim that the family has ended is made this time. What is claimed is
narrower and checkable: the invariant is written down, it is enforced at the
single point of release rather than at each path, and each path listed has a
test. A fifth finding would be either a path that reaches the release with an
unregistered group, or a release that is not `compiler_lease`'s.

## Amendment, 2026-11-17: zombies, an unkillable member, and a stale literal

Two items from review of `728cfc64f`, after every figure here was published.
No figure moves.

**A CI failure in this PR's own test, not a flake.** A test asserted that the
direct-SystemVerilog project renames exactly 30 block-RAM requests. `main`
gained two more in `fes_i386_x87.sv` when the x87 work merged, so CI, which
tests the merge, counted 32 while the branch still counted 30. `main` is merged
in and the test now counts the requests from the sources it is given. The
published direct-SV fit is unaffected: it was taken from the 30-request
sources and its record carries its own source digests.

**Zombies.** `killpg(group, 0)` reports a group alive while its only member is
a zombie. Under a parent that never reaps (a container's PID 1 often does not)
a killed stage therefore looked alive, and the reap stalled and then raised
out through the lease's release. Counterexample first: a subreaper that never
reaps, a stage that leaves an orphan, SIGKILL; the only member was in state
`Z` and the liveness check said alive. Liveness is now read from
`/proc/<pid>/stat` and a zombie is not a running member, since it holds no
file, licence or core and cannot overlap a fit. `killpg` remains only as the
fallback where `/proc` is unavailable.

**A member that survives SIGKILL.** The previous version raised after 60
seconds, which unwound through `compiler_lease` and released the lock over a
live group: a breach of the invariant stated above, by the code written to
hold it. `flock` cannot be kept by a process that exits, so the choice is
between staying and releasing. The runner now **stays**: it holds the lease
and reports the surviving PIDs every 30 seconds until they are gone. A held
lease is visible to everyone waiting on it; an overlapped compile is visible
to nobody and shows up as figures nobody can explain. The cost is that a
process stuck in the kernel blocks the shared builder until an operator kills
the runner, and that is the operator's decision to make, not the runner's.
This joins the stated limits: killing the runner in that state releases the
lease over whatever is still running.

Two rows join the release-path table:

| Path to the lease's release | Test |
| --- | --- |
| A killed stage whose member is an unreaped zombie | `test_a_zombie_member_does_not_keep_the_group_alive` |
| A member that survives SIGKILL (the reap stays and reports) | `test_a_group_that_survives_sigkill_holds_the_reap_and_says_so` |

The second of those tests simulates the surviving member, because a process
that survives SIGKILL cannot be made on demand; it shows the reap does not
return or raise while members are reported, not that the kernel behaves so.
