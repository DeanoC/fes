# PC/XT forwarded-clock and DQ capture investigation, 17 November 2026

The initial bounded audit at `8ff232e21` measured the retained production paths
and a failed input-packing-only candidate. The continuation now adds an
experimental, default-off `SDRAM_CAPTURE_PIPELINE` path at 100 MHz: a separate
forwarding counter, later I/O capture and a falling-edge controller handoff.
The registered producer does not select it. No kit was claimed or programmed;
there is no hardware acceptance or production promotion.

The reference is [#1940 at
70abc256f](https://github.com/DeanoC/fes/tree/70abc256fc2bcc0e99f58a6195f4864b21b63999/docs/validation/2026-11-17-pcxt-sdram-budget),
whose exact-head Codex review and CI passed before handoff to lander. Its
conditional Etron -6 CL2 budget, electrical assumptions and missing board bounds
remain applicable. These are zero-flight reference calculations, not physical
interface acceptance.

## Inputs and reproduction

The audit branch starts at `5f0c70ba1c6ccbaa75dc14f74881a6b79531a964`.
The original artifact source is `85dbb42815a26ef51c490c79a790f861101b96ff`,
BUILD_ID `1d1e4a52fced512afe3c96dc88cc0783`, payload SHA256
`d10aec8b6295b341e688ab12d358f2864cfeca6bffc4417df7bbfdab1e6ae56c`.
All 176 sources captured by the native diagnostic still match this base.
Of the broader 185 build-record inputs, `scripts/fes_build_common.py` has changed;
this experiment uses the retained elaborated Verilog and native QSF/SDCs, not a
fresh elaboration or production build through the current producer.

[run_fit.py](run_fit.py) creates a new private project from those retained inputs,
without copying a fitted database. It appends 16 explicit `FAST_INPUT_REGISTER`
assignments, runs Quartus Lite 17.0.2 Build 602, then runs the unchanged native
internal audit and #1940 conditional collector. The receipt records original and
candidate input hashes, command lines, executable hashes and RBF digest. Every
child has a 1,200-second cap, shared-host/Quartus locks, disk gate and heartbeat.
No new seal or package is produced. An altered diagnostic placement must never
be exported using the retained package identity or BUILD_ID.

```sh
python3 run_fit.py --native "$NATIVE_SEED_1" --work "$NEW_PRIVATE_DIRECTORY" \
  --quartus-root /home/deano/intelFPGA_lite/17.0/quartus \
  --collector "$RETAINED_1940_COLLECTOR"
python3 analyze_read.py "$NEW_PRIVATE_DIRECTORY" --output "$READ_DECOMPOSITION"
```

[analyze_read.py](analyze_read.py) checks all 16 detailed read-setup paths at each
of four corners. Each path's pin, capture endpoint, data delay and slack must
agree with the raw channel table. It reconciles forwarded-clock arrival plus
external tAC plus DQ cell/interconnect delay against data arrival, and required
minus arrival against slack. Removing a path or altering required time is rejected.
The original producer receipts bind the native endpoint tables; #1940 binds the
full control fit databases and conditional reports.

Protected raw evidence lives at `/home/deano/fes-hil/k835-capture-path/`:
`control-seed-{1,2,3}.json`, private fit directories, receipts, detailed reports,
source identity, negative checks and the hash manifest. The native controls
remain under `/home/deano/fes-hil/k835-sdram-budget/bound/seed-{1,2,3}`.

## Measurements

The three controls' worst setup paths are all at slow 1100 mV, 100 °C:

| Seed | Read slack (ns) | Forwarded-clock latency (ns) | DQ interconnect (ns) | DQ cells (ns) | Total DQ delay (ns) |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | -8.717 | 8.443 | 6.199 | 1.266 | 7.465 |
| 2 | -9.558 | 8.622 | 6.677 | 1.634 | 8.311 |
| 3 | -8.811 | 8.631 | 6.177 | 1.358 | 7.535 |

The forwarded-clock number is the actual SDRAM_CLK pin arrival minus its launch
edge; it includes PLL compensation and routing. It is not a sum of positive
insertion delays or an ideal clock placed at the pad. For seed 1 the path
includes 2.809 ns global-clock interconnect to the output DDIO, 1.697 ns DDIO
cell delay and 2.454 ns output-buffer delay. The DQ capture endpoint is a fabric
FF, not an input I/O register. These clock and data terms must both be accounted
for; shortening the ingress alone is not presumed sufficient.

The first request used a braced wildcard that the Quartus QSF reader treated as
an invalid node name (critical warning 136021). That run is preserved as
`fast-input-seed-1`, with `invalid-wildcard-driver.py`. Its RBF, conditional rows
and native endpoint table equal control seed 1 exactly. It is a rejected request,
not an input-packing measurement. The corrected run uses explicit pin names in
`explicit-input-seed-1`.

A useful counterfactual holds the measured clock paths and required time fixed
and sets the entire DQ input delay inside the FPGA to zero. Worst-path slack
would still be -1.252/-1.247/-1.276 ns for seeds 1/2/3 (`slack + data_delay`).
This is arithmetic on the retained paths, not a new timing run or proof that
any particular I/O implementation cannot close: packing can also change the
capture-clock path. It shows why the forwarded-clock and capture phase must be
examined together, rather than attributing the full miss to the long DQ route.

## Corrected candidate result: reject

All 16 pins report `Input Register: yes`, and the read paths terminate in
`DDIOINCELL` locations. The candidate completed compilation, internal audit and
conditional collection in 429.492 seconds. The candidate RBF SHA256 is
`456ca95ff265def8e7c11d612fcda7119998c5482a85ee4946ce4fc5f56edde4`;
it has not been sealed, installed or accepted on hardware.

| Metric | Control seed 1 | Explicit input-register candidate |
| --- | ---: | ---: |
| Worst conditional read setup (ns) | -8.717 | -2.789 |
| Worst read path DQ interconnect (ns) | 6.199 | 0.000 |
| Worst read path total DQ delay (ns) | 7.465 | 2.525 |
| Worst read path forwarded-clock latency (ns) | 8.443 | 8.439 |
| Minimum read hold (ns) | 5.071 | 4.028 |
| Minimum capture-to-controller setup (ns) | 0.908 | -1.151 |
| Minimum capture-to-controller hold (ns) | 4.343 | 5.563 |
| Packed DQ input registers | 0 | 16 |

The worst read bit changes from DQ4 to DQ9, so the table compares worst cases,
not one identical physical path. All 64 candidate read-setup paths were checked
against the raw channel table, bringing the decomposition total to 256 paths.
All 128 candidate handoff rows are present. The candidate has three negative
native setup endpoints at each slow corner; no reported internal hold,
recovery/removal or pulse-width failures. Audio payload/interstage and mailbox
interstage checks remain positive. See [summary.json](summary.json) for exact
corner/endpoint identities and report hashes.

The packing request **does not close the external read budget and breaks the
native internal timing gate**. It moves delay into the half-cycle handoff rather
than solving the end-to-end capture path. No production assignment is adopted,
no timing exception is added and no clock is lowered. The failed candidate and
unchanged control are retained; there is no seed sweep.

The next bounded implementation must coordinate the forwarded-clock path,
capture phase and controller handoff, with a fresh real-controller read-window
proof before fitting at 100 MHz. Moving capture later spends the existing
half-cycle handoff budget; the later rdata retirement from #1940 is not an extra
external capture cycle. Any changed producer/RTL requires its normal fresh
identity, timing/seal and exact-artifact HIL gates before promotion. Board
load/slew/flight, voltage/temperature, pin pulse/jitter, electrical turnaround,
other physical interfaces and ISA containment remain open in #835.

Validation: 256 full-path arithmetic/channel comparisons; retained producer
endpoint receipts; missing-path and changed-required-time rejection; actual
corrected fixed-seed fit with 16 packed inputs and all-corner native/conditional
reports; `make check` and affected parent checks pass. Evidence class is host
proof and diagnostic post-fit timing, **not physical hardware acceptance**.

To regenerate the compact four-fit assessment from the retained raw evidence:

```sh
python3 summarize.py --evidence-root /home/deano/fes-hil/k835-capture-path \
  --controls-root /home/deano/fes-hil/k835-sdram-budget/bound \
  --output summary.json
```

The candidate's complete post-STA `db/` and `incremental_db/` inventory is retained
as `candidate-postfit-database.json`; its digest and both diagnostic receipts are
in the compact assessment. `evidence-manifest.json` binds the protected record,
receipts, inputs, logs and raw reports. Database inventories include every file;
no cache filename is excluded from their identity.

## Coordinated continuation: selected-path reference passes

The default-off `SDRAM_CAPTURE_PIPELINE` implementation uses four 100 MHz PLL
outputs: controller phase 0, I/O capture +9.375 ns, DDR forwarding +1.250 ns and
read-word handoff +8.750 ns. The forwarding pin retains the DDR inversion,
with an ideal rising phase of +6.250 ns. This is an explicit RTL clock/data
pipeline; no regional-clock assignment is required or adopted.

A controller-domain four-cycle READ history enables one handoff per transaction.
The handoff register latches the selected word once and holds it through the
frontend response. A falling-edge controller-domain register lands that retained
word before response. The shared controller's unused legacy rdata cone is removed
in this opt-in mode. Its state/done sequence and the frontend response cycle are
unchanged. The registered DOS producer still selects the original path.

### Logical deadline and input contract

[coordinated/verify_pipeline.py](coordinated/verify_pipeline.py) repeats temporal
induction on the unchanged shared controller. Separately, the active-pipeline
trace performs real initialization and 512 reads covering all 256 byte values
on each lane. It checks one retained-word latch and one landing per READ and
asserts the existing frontend response deadline for every request. Mutations
that bypass capture, hand off on the wrong edge, or remove word retention fail.
The missing-retention mutation reaches the frontend and corrupts its response;
a passing early controller-state observation alone would not detect that bug.

The trace uses a **logical selected-word input contract** and poisons all other
capture edges. It is not a zero-delay SDRAM electrical model and does not claim
that the device's tAC/tOH interval is located at the ideal RTL clock phases.
Physical word validity is the separate all-corner timing obligation below.
Relative to the ideal transaction READ observation, the checked events are:

| Event | Time after READ |
| --- | ---: |
| Selected I/O sample | 33.125 ns |
| Retained-word handoff | 42.500 ns |
| Original controller capture-state transition | 43.750 ns |
| Controller-domain landing | 48.750 ns |
| Frontend response | 73.750 ns |

The experimental response uses the landed retained word, not the unused legacy
controller rdata. Its internal landing is later than the old rdata observation
but still precedes the unchanged consumer deadline. This adds no external
memory capture cycle: it retains the one selected first word. The ordinary
[component regression](../../../sources/misteross/tests/test_pcxt_capture_pipeline.py)
exercises this active path and rejects the missing-retention mutation in CI.
Verilator wrapper/engine and locked Yosys are authenticated for the qualification
run; the ordinary regression follows the component's normal tool selection.

### Fitted timing

The source-frozen fixed-seed-1 diagnostic completed in 281.390 seconds with
experimental ID `5e5d6ccd354bed7d29a93dca7bf09524`. This ID is not a registered
design-v1 package identity. Its raw RBF SHA256 is
`8a8d5b40ecb24ea2bcd6737e5280ef2cff3eddb1e62b16c3fad2ac3be54409ed`.
All four requested clock periods and phases are present in the native report.
All 16 capture inputs are physically packed. The elaborated model binds the
DDR source to PLL counter 2, capture FFs to counter 1, the enabled held-word
register to counter 3, and the landing register to the falling controller edge.

[coordinated/assess_pipeline.py](coordinated/assess_pipeline.py) keeps the raw
periodic-edge report separate from the consumed first-word obligation. The raw
report selects the nearest edge, a 3.125 ns relationship. CAS-2 launches the
consumed word two chip clocks after READ; the selected capture has a 13.125 ns
relationship to that launch. The assessment therefore records an algebraic
+10 ns setup / -10 ns hold adjustment for this transaction, checked against the
trace's selected edge. It retains every raw failure. It adds no multicycle
constraint, false path, timing-allow-fail option or production seal exception.
The actual routed forwarding/capture clock paths and uncertainty remain in the
measurement. This conditional calculation is not production signoff.

| Check, minimum over four corners | Setup (ns) | Hold (ns) |
| --- | ---: | ---: |
| Consumed first-word reference | +0.340 | +0.912 |
| I/O capture to held word, 128 rows | +0.410 | +1.894 |
| Held word to landing, 128 rows | +2.307 | +3.279 |
| Direct connector command/address | +3.578 | +0.869 |
| Conditional second-CS inverter reference | +0.131 | +1.876 |

All native reported setup, hold, recovery, removal and pulse checks pass.
Audio payload/interstage and mailbox interstage minima remain positive.
The generic periodic read report remains -9.660 ns setup / +10.912 ns hold;
it is not relabelled as passing. Generic DQ-write/OE pin-window rows also remain
in the report. The earlier controller-held write proof gives transaction-specific
data allowance, while electrical output-enable turnaround remains unqualified.
See [coordinated/result.json](coordinated/result.json) for identities and margins.

The original input-packing-only candidate, the first coordinated DDR fit, both
ignored regional requests and the slower direct-PLL forwarding candidate remain
retained. The direct-PLL candidate still misses the consumed-word setup by
2.777 ns and native handoff by 0.578 ns; its longer fabric feeder is not adopted.
The complete candidate `db/` and `incremental_db/` inventory, raw project files,
source snapshots and reports are retained. Quartus's project metadata rewrites
are recorded with both raw before/after digests; no compiled input, route-deciding
QSF statement or timing constraint change is accepted by the diagnostic driver.

A final unused-clock lint fix affects only the disabled branch. Fresh opt-in
elaboration at the fitted experimental ID, using the identical source/work paths
inside a private mount namespace, reproduces the fitted Verilog byte for byte:
both SHA256 values are
`c178780411d6ad402c47521b56c2e66959644379e987dec51a08389a3997fed6`.
Neither the original frozen source nor its outputs were modified. A comparison
at different paths first exposed path text inside generated function names;
that failed comparison is retained, not normalized away. The same-path result
and its exact command are retained in the protected evidence.

### Scope, validation and remaining gates

The native Quartus binaries match the original pinned producer identities.
The source-frozen fit and its matching logic proof precede fitting; final source
binding is strengthened by the byte-identical opt-in netlist comparison above.
Focused validation passes 55 Quartus/package/pipeline tests with locked Yosys,
the default memory regression at all mask delays, and whole-system CGA/Direct
cold/warm boots with CPU/DMA activity. The new optional clock is tied explicitly
in default simulation fixtures. `make check` and whitespace checks pass.
The broad affected-check run reported video-part fixture errors and reached its
local 600-second cap. It is not claimed passing or proven to be a baseline
failure. Exact-final-head CI is recorded on the PR separately.

This is **host proof and diagnostic post-fit timing**, not physical hardware
acceptance or production qualification. The positive read reference has only
0.340 ns setup headroom before missing board flight/load/slew bounds. The
inverter result remains conditional on the unverified fitted part and loading.
Pin pulse/jitter, electrical turnaround, other interfaces and physical ISA
containment remain open in #835. Normal production identity, declared-seed
signoff, repeat/seal and exact-artifact HIL are still required before adoption.
No package was sealed/exported/installed and no kit was claimed. The feature
stays default-off and #1954 stays draft in lane:deano.

Protected evidence is under `/home/deano/fes-hil/k835-capture-path/`, including
`coordinated-fit-retained`, `coordinated-proof-retained`, `coordinated-proof-final`,
`final-elaboration-same-path`, the complete database inventory and the verified
`coordinated-evidence-manifest.json`. The retained reference harness is an
unchanged copy of #1940's reviewed helper; the normal CI regression has no
external worktree dependency.
