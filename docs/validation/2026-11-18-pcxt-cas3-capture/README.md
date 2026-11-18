# PC/XT CAS-3 capture at 100 MHz, 18 November 2026

The default-off capture path now selects CAS-3 at 100 MHz and the correct first
word. Its fixed-seed diagnostic passes the native internal timing inventory and
the common-device selected-word reference. **Physical board timing is still
BLOCKED.** No production build, seal, export, kit claim or HIL was performed.

This continues [#835](https://github.com/DeanoC/fes/issues/835), based on
`cf89a5756` from [#1970](https://github.com/DeanoC/fes/pull/1970), whose base is
`5e592f00389fd2bb61194788c917c36f5c880dec`. It is the second level of that stack.
Misteross owns the FPGA change; FES owns integration evidence. No shared ABI,
runtime, image, compiler lock or package selection changes. The registered
`fes.pcxt-dos` producer still leaves `SDRAM_CAPTURE_PIPELINE` disabled.

## Correct the word, not just the CAS bits

The inherited #1954 reference assumed the first word launched at READ+CAS
clocks. That is one clock late. [ISSI's manufacturer document, printed page
26](https://www.mouser.com/datasheet/2/198/42-45R-S_86400F-16320F-706495.pdf)
explicitly distinguishes data availability at n+CAS from output launch at
n+CAS-1. Etron revision 2.4 figure 20 shows the same distinction: its CL2 READ
at T5 launches the first word from T6. The primary Etron PDF is retained with
the hash recorded in the [common envelope](../2026-11-18-pcxt-sdram-envelope/devices.json).

The old tagged-input fixture supplied data at an assumed FPGA sample. It did
not prove that a real chip still held that word. The old +0.340/+0.912 ns
selected-word margins are therefore withdrawn for that selected physical word,
with the correction recorded in #1970. Raw reports and failed controls remain
unchanged. No previous hardware acceptance is transferred to this path.

CAS-3 first-word launch is READ+20 ns. The implementation reuses the unchanged
shared controller's existing CAS-3 profile (rate selector 1) with a 100 MHz PLL.
Its nominal 130 MHz refresh count is explicitly replaced with 699, giving a
700-clock / 7 us scheduling interval. Other clients and the default PC/XT path
keep their existing configuration. The shared controller RTL is not edited.

Controller and capture clocks have phase 0, forwarding +1.250 ns, and handoff
+9.375 ns. DDR inversion makes the ideal memory-pin rising phase +6.250 ns.
Quartus merges the identical controller/capture output counters. The fit report,
logical PLL connections and actual timed launch clock bind that alias explicitly;
a missing unrelated clock is not accepted.

| Event, relative to nominal READ observation | Time |
| --- | ---: |
| First chip word launch, CAS-1 | +20 ns |
| Selected input capture | +33.750 ns |
| One enabled retained-word latch | +43.125 ns |
| Controller-domain landing | +48.750 ns |
| Unused legacy controller rdata transition | +53.750 ns |
| Actual frontend response | +83.750 ns |

The response is one controller cycle later than the earlier experimental path.
Guest READY still waits for actual completion; the required clock stays 100 MHz.
The declared frontend deadline is checked on every read. A later response does
not extend the physical memory's word-valid interval.

## Proof before fitting

[verify_pipeline.py](verify_pipeline.py) authenticates the pinned Yosys and
Verilator wrapper/engine using the retained reference harness. Temporal
induction proves the existing three-cycle write stability and CAS-3 returned
input relationship with symbolic requests, data and reset. The reset-free
history covers the full additional return cycle; the initial insufficient
six-observation fence failed induction and is retained, not reported passing.

The separate active-pipeline trace runs real initialization, checks CAS-3/BL1
programming on both ranks, performs 512 reads covering all byte values on both
lanes, and asserts one retained-word latch, one landing and the +83.750 ns
frontend response for every request. It observes runtime refresh during requests
and idle, bounded to at most 740 clocks (7.4 us) in that trace. No universal
refresh scheduling or physical power-up proof is inferred from this bound.

Capture bypass, wrong handoff and missing retention mutations fail. Missing
retention is checked at the actual frontend response. The component regression
also rejects the wrong READ-history cycle. These are logical selected-input
tests, not a zero-delay chip or electrical board model. Actual tAC/tOH validity
is the separate routed timing obligation below.

## Source-frozen fit and common-device reference

The accepted diagnostic is fixed seed 1, Quartus Lite 17.0.2 Build 602, with
experimental ID `192041326f5f13129ed52b77e5937dc0`. Its raw RBF SHA256 is
`a0769ea829272762cb6408283e178229859ba3ae9f36cf03abdeb37d654190fb`.
It completed elaboration, compilation and native/conditional collection in
326.537 seconds. This is not a registered design-v1 identity or sealed package.

All 178 captured code/tool/constraint/fixture inputs match the final source
except the subsequently updated core README. That difference is explicit in
[fit-receipt.json](fit-receipt.json). The frozen source, experiment ID and RBF
are preserved; no source or identity is rewritten in the retained project.
The normal producer owes a fresh identity and complete qualification on adoption.

The collector uses the common CAS-3 envelope: tAC maximum 6 ns, tOH minimum
2.5 ns, receiver setup/hold 1.5/1.0 ns. The periodic nearest-edge relationship is
3.750 ns. The proved first-word launch-to-capture relationship is 13.750 ns.
[assess_pipeline.py](assess_pipeline.py) records algebraic +10 ns setup and
-10 ns hold for that word, preserving the raw periodic failures. No production
multicycle, false path, frequency change or seal exception is introduced.

| Minimum across four FPGA corners | Setup | Hold |
| --- | ---: | ---: |
| Consumed first-word reference | +0.452 ns | +0.488 ns |
| Capture/data-enable clock pair to held word | +0.794 ns | +0.736 ns |
| Held word to landing | +1.763 ns | +4.484 ns |
| Direct command/address | +3.615 ns | +0.666 ns |
| Conditional second-CS inverter | +0.135 ns | +1.673 ns |
| Raw periodic read, retained as reported | -9.548 ns | +10.488 ns |

All native reported setup, hold, recovery, removal and pulse checks pass.
Audio payload/interstage minima are +0.533/+0.226 ns; mailbox interstage is
+0.215 ns. All 16 DQ input registers are packed. The common CAS-3 device cycle
minimum is 7.5 ns, giving 2.5 ns nominal headroom before physical clock bounds.

Capture data and READ enable now share one physical launch clock. The native
auditor's 128 worst endpoint/clock-pair rows therefore select either data (63)
or enable (65). Their combined worst bound covers both; they are not claimed
as 128 independently reported rows for each source. All 128 landing rows and
568 conditional channel rows are present. This reporting difference initially
tripped the old separate-clock assertion; no timing row or failure was removed.

The first CAS-3 candidate failed setup by 1.481 ns from READ history to the
held-word enable: its +0.625 ns handoff phase gave that control too little time.
The final phases give it 9.375 ns. Its collector also rejected an absent counter
2, merged into identical counter 1. The final candidate retains forwarding
counter 2 and explicitly verifies the controller/capture alias instead. Both
are fixed-seed design candidates, not seed rerolls. [prior-attempts.json](prior-attempts.json)
records the rejected fit, failed harness, source snapshots and full DB inventory.

## Remaining gates and evidence retention

This is host proof and conditional diagnostic post-fit timing. The +0.452 ns
read setup margin is before missing board clock-out plus returning-data flight
and electrical changes. The common chip envelope is not a board-flight model.
Actual motherboard/connector/module flight and skew, loads/slew and receiver
thresholds, voltage/temperature, physical CLK period/pulses/jitter, initialization
and electrical DQ/rank turnaround remain unqualified. The inverter row is still
conditional on its part and load, not a universal module assumption.

Consequently the user's condition for a production build, seal and exact-artifact
HIL has not been met. Those stages are NOT STARTED or waived. Adoption needs
board bounds, production admission of the consumed-word contract, normal identity,
all declared seeds, independent repeat, timing/seal gates and exact package and
payload hashes. HIL then needs an identified conforming module, kit claim and
verified restoration of the named baseline. No diagnostic RBF is HIL evidence.
Other #835 interfaces and physical ISA containment remain open.

Raw sources, fit databases, complete database inventories, reports, logs,
proofs and failed attempts are protected at
`/home/deano/fes-hil/k835-cas3-capture/`. The complete inventories include all
regular files under db/incremental_db without cache exclusions. A manifest
binds retained inputs/results. Tracked compact receipts bind the full protected
receipt and DB inventory. No shared harness or another owner's process was used.

Validation includes the pinned-tool proof and negative traces, component capture
regression, Quartus/package tests, explicit locked diagnostic/DOS elaboration,
default memory at all three mask delays (10,489 reads, 8,316 writes, 497 refreshes,
80 warm/40 cold-reset phases each), and CGA Direct CPU/DMA cold/warm system boots.
Parent consistency/affected checks and exact-head CI are recorded on the PR.

## Reproduce

Use fresh work directories. Each proof child has a 600-second cap and heartbeat;
the formal solver has a 120-second cap. Fit children use shared host/Quartus
locks, a 1,200-second cap and heartbeat. All jobs refuse under 30 GiB free.

```sh
python3 docs/validation/2026-11-18-pcxt-cas3-capture/verify_pipeline.py \
  --module-root sources/misteross \
  --reference-harness docs/validation/2026-11-17-pcxt-sdram-budget/windows \
  --yosys /absolute/locked/yosys --work /absolute/new-proof
python3 docs/validation/2026-11-18-pcxt-cas3-capture/fit_pipeline.py \
  --module-root sources/misteross --yosys /absolute/locked/yosys \
  --quartus-root /absolute/quartus --work /absolute/new-fit \
  --collector docs/validation/2026-11-17-pcxt-sdram-budget/collect.tcl \
  --proof /absolute/new-proof/proof.json
python3 docs/validation/2026-11-18-pcxt-cas3-capture/assess_pipeline.py \
  --fit /absolute/new-fit/seed-1 --proof /absolute/new-proof/proof.json \
  --output /absolute/new-fit/result.json
```
