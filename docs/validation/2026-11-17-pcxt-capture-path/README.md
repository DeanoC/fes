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

## Coordinated continuation

The default-off path requests three 100 MHz PLL outputs: controller phase 0,
capture +8.125 ns and forwarding +1.250 ns. The forwarded DDR pin retains its
inverted polarity (ideal rising phase +6.250 ns). A falling-edge controller
input stage gives the I/O capture 6.875 ns of ideal handoff time. The controller
state machine and its response deadline are unchanged. The private diagnostic
also requests 16 input I/O registers and regional routing of `clocks[2]`.
The normal producer still selects the original path.

[coordinated/verify_pipeline.py](coordinated/verify_pipeline.py) repeats temporal
induction of the real controller's return deadline. Its phased trace uses the
real `pcxt_memory`, performs real initialization, reads all 256 byte values on
both lanes (512 responses), and poisons every pin time outside the first CL2
word's guaranteed reference interval. Relative to READ, that interval is
26–32.5 ns, the selected capture is 31.875 ns, the staged handoff is 38.750 ns
and controller retirement is 43.750 ns. These are ideal RTL phases, not measured
board delays. Both stage-bypass mutations fail after real initialization. The
old positive-edge handoff passes logically; its physical timing remains a
separate fit obligation. Verilator wrapper/engine and locked Yosys identities
are checked and recorded.

The raw periodic input report chooses the nearest clock edge: 1.875 ns for
this phase pair. The consumed first burst word instead launches two chip clocks
after READ and is captured one cycle later, with an 11.875 ns relationship.
[coordinated/assess_pipeline.py](coordinated/assess_pipeline.py) records that
proved transaction window as an algebraic +10 ns setup / -10 ns hold adjustment.
It retains every raw per-edge failure and adds no multicycle constraint or
production timing exception. This differs from giving the controller an extra
response cycle; the response state/deadline is unchanged. Raw reports, used-word
margins, command margins, stage polarity/connections and all internal gates must
be reported separately. Passing this conditional calculation is not physical
or production signoff.

The first coordinated fit (capture 6.875 ns) passes native internal timing but
still misses the consumed-word read setup by 2.093 ns. Its forwarding path keeps
a 2.848 ns global-clock route to DDIO. The next 8.125 ns attempt used a removed
alias for its regional assignment and is retained as an ignored-route request.
The corrected private fit targets the retained `clocks[2]` net. All attempts
and failures remain protected under `coordinated-fit*`; none is a new seal.
