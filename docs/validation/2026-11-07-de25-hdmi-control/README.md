# DE25 HDMI interrupt and I2C busy handshake

The captured HDMI wrapper now synchronizes its asynchronous interrupt through
two preserved 50 MHz stages using the existing qualified configuration reset.
Both stages reset inactive. Only the raw pin to the first D pin is exempt;
the second stage and the related divided-clock configuration consumer remain
timed. Missing stages, raw input or first D pin reject during constraint loading.

The initializer also waits for actual busy entry before accepting completion.
It asserts `GO`, waits for `END` to go low, then waits for completion and checks
ACK. Previously a high `END` from reset or a previous transaction could complete
a new request before the byte controller had started. The model exposes this
during NACK retry. The guarded derivation changes that handshake only; the
register table, byte controller and divider retain their captured bytes.

Implementation/compiled source: `6c6dd62b503f143448a65e17ba346e410187e960`.
Package: `03c8fa017f5fcfb40d23d54a449e8464d89fa9919dc6bae301fd8c833955290a`.
Functional build ID: `0792e51a2e51512bfb8fe44e93a69ddd`.

## Control behavior and source binding

The actual wrapper/reset HDL and process-synthesized captured I2C circuit run
against a resolved open-drain slave model. They accept 99 exact register writes
across startup, interrupt-driven reinitialization and reset recovery. The model
injects one NACK, requires exactly one rejected transaction and retries the same
table entry. Reset interrupts a further transfer with the clock parked, releases
the bus and idles the interrupt without inventing a completed write. The run
uses 36,124,469 half-clock ticks; the real divider is not accelerated.

Restoring raw interrupt wiring fails immediately when the input changes without
a clock edge. Restoring the original completion handshake fails on an incomplete
transaction. Both are required negative controls. Every accepted slave address,
byte, ACK and table entry is observed on the modeled bus.

The vendor's clocked blocking processes race under direct event simulation.
Yosys lowers the actual input processes to explicit register next-state logic
before Verilator executes the bus model. This is a digital synthesized-circuit
model with a PLL stub; it is **not Quartus circuit equivalence, metastability,
analog edge timing or physical I2C proof**. Generated-case simulator style
waivers add no production timing exception. Early diagnostic failures and the
final source-bound model remain retained with separate classifications.

All eight control-model source hashes bind either to the exact compiled
wrapper/initializer/original/controller/reset files or to the committed helper,
verifier and probe. The compiler's original vendor hashes match the retained
initializer original and unchanged byte controller. The complete LUT suffix
matches byte-for-byte, independently of the 99 observed writes. The 73 prior
writer/ROM model hash checks remain unchanged; those checks also run in the
fresh affected suite.

## Fresh compilation and fitted paths

The locked normal-user 3090 Quartus Pro 26.3 build passes all ten unchanged
120 s phase bounds in 555.511 s. Its sealed package and
203 source inputs verify, including the new derivation
helper. Native source-context dependencies include that helper. The QSF changes
only its build identity; pin voltages, locations, PLL phases and loads are
unchanged. No CPU register, ROM size, bus master or runtime ABI is added.

Read-only routed queries use only the sealed SDC, with no added constraints.
All four corners retain both stage-to-stage and stage-to-consumer setup/hold
witnesses (16 checks). Their reported minima are:

| Internal IRQ path | Setup, ns | Hold, ns |
| --- | ---: | ---: |
| Stage 0 to stage 1 | 19.565 | 0.110 |
| Stage 1 to configuration consumer | 18.262 | 0.023 |

The first path launches and latches on the same clock; the consumer path latches
on `adv7513_config`. Neither is hidden by the raw input exception. Recovery and
removal remain in the complete-design analysis. Reported multicorner minima
for setup/hold/recovery/removal/pulse width are
`1.803 / 0.000 / 6.773 / 0.174 / 0.220` ns, with zero
constrained TNS. Hold rounds to 0.000 ns, providing no reported global margin.

All 27 HDMI RGB/control pads are checked over four corners, and all 42 clocks
are checked for pad launches (168 rows). Only the pixel clock launches them.
Receiver-reference setup/hold minima are
`3.374/0.013` ns.
This retains the ADV7513 receiver-only limits at symbolic zero PCB skew.
The small hold margin cannot qualify unknown board skew/loading. Original
QSF, QPF, SOF, timing report and sealed SDC hashes remain unchanged by querying.

The unconstrained-path summary falls from two input ports to one: SDA remains,
while raw IRQ ends only at its explicitly exempt first stage. The separate
missing-input-delay rule still reports both SDA and IRQ; those counts must not
be conflated. No fictitious IRQ clock or input arrival delay is added. Three
missing-output-delay findings remain (LED[7], SDA, SCL), along with two reset
domain findings and one intra-clock reset finding. Four categories remain,
zero waivers. System/pixel reset-chain counts remain four/two.

## Software and native admission

The full affected suite passes 1,582 parent cases (three skips), 1,590 FPGA
cases (75 skips), planner/whitespace checks, 48 generated consumers, 37 fixture
copies and the actual Avalon/native integration. All three new control tests
run with the pinned vendor archive, prepared project and selected Yosys. The
actual SDC runs against 14 inventory scenarios, retaining the five prior cuts
and five metadata bounds plus the single justified IRQ first-stage exception.

The new real package and retained 8 KiB ZX81 ROM pass the shared linker,
resource-map generation and race-checked native-loader preflight with one
selection and zero firmware calls. This does not establish SD access, native
boot, physical driver admission or core-load acceptance. A matched native boot
chain for this package has not yet been built.

## Remaining physical work

External SDA/SCL timing, rise/fall/loading and LED classification remain open.
The functional bus model does not replace those checks. The manufacturer's
[ADV7513 datasheet](https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf)
contains the receiver and I2C requirements; actual board timing must account
for the appropriate bus edges and electrical loading. The
[hardware guide](https://www.analog.com/media/en/technical-documentation/user-guides/adv7513_hardware_user_guide.pdf)
describes INT as an open-drain interrupt output; no external source clock is
inferred from it.

All 31 relevant pin locations match the Rev B manual. Its HDMI pin table still
disagrees with the inherited 1.1-V assignment on DJ24, bank 2A_T; the actual
fitter still reports clock load 0 pF. The clock electrical path and PCB skew/load
contract remain unresolved. A schematic/routing-document question is pending.
No voltage, receiver register or clock phase is changed speculatively.

No UART opening, JTAG action, FPGA programming, SD write or flash action took
place. TRWU8CBL's CPU/FPGA state still requires requalification after the user's
power interruption. Historical recovery inputs verify, with no restore attempted;
the top-kit claim remains held, factory restoration remains waived and the
bottom kit is untouched.

Next: prepare a matched native chain and bounded operator for a core/ROM/input/
return trial, while completing external I2C/LED timing and remaining reset
ownership. Any physical test needs fresh board qualification and its exact
source-bound gates. Physical menu/core transitions, media and native networking
remain necessary toward full DE10-style operation.

Raw evidence: `out/nano-hdmi-control-20261107/`; `result.json` binds the receipts.
Retention target: `/home/deano/fes-retained/de25-hdmi-control-20261107-r1`,
dependent on the receiver-timing retention. No push, PR, merge or lane handoff
is implied. No hardware or whole-board timing acceptance is claimed.
