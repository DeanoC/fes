# RAM tester: physical HPS-ready pin in placement prediction

For [FES #264](https://github.com/DeanoC/fes/issues/264), test whether a
narrow physical-pin correction in nextpnr placement prediction helps the
unchanged RAM-test design. The retained routed result is **116.918037 MHz**
memory; the separately measured enable-reuse tradeoff is 117.868927 MHz.
This pin-aware placement trial does not produce a routed result and is rejected.
FES base: `2b11bf5b`.

## Why this test

The HPS FPGA-to-SDRAM atom occupies BEL (52,53), but `cmd_ready_1` actually
enters the fabric at `GIN.51.64.18`. `Arch::predictDelay` formerly used the
atom's BEL coordinates for placement cost. At the retained slot-free sink
(30,26), the old estimate is 4,370 ps; using the physical output-pin wire
gives 5,435 ps. These are **placement estimates**, not a correction to final
timing or proof that the old model was pessimistic. Earlier HeAP pin-offset
and fixed-anchor experiments changed different placement stages and regressed
routed memory timing; this test isolates the predictor's ready-1 source.

Private nextpnr commit
[`d165f9c2`](https://github.com/DeanoC/nextpnr/commit/d165f9c291576c48183be00f187b5f93e1dd55d4)
adds opt-in `NEXTPNR_MISTRAL_HPS_READY_PIN_PREDICT=1`. Only HPS
`cmd_ready_1` uses the GIN wire location in `Arch::predictDelay`; other pins,
the default path, HeAP pin offsets, routing and final delay computation remain
unchanged. A real-device focused test verifies the GIN coordinate, the two
estimates and unchanged `cmd_ready_2` prediction. The complete backend CTest
suite passes.

## Matched placement comparison

Both runs use one frozen compiler binary, synthesis JSON and BUILD_ID, device,
QSF/SDC, seed 2, HeAP timing weight 10, critical exponent 2, GPU selection,
enable-replication budget four and the same timeout/retained-enable hooks.
The only intended difference is the opt-in predictor flag. The disabled run
matches **12 saved baseline artifacts byte-for-byte**, including placed JSON,
timing JSON, timeout/retained snapshots, pin states, and the retained audit.

| Placement-only observation | Disabled control | Pin-aware trial |
| --- | ---: | ---: |
| Memory prediction | 77.04 MHz | 74.18 MHz |
| Pixel prediction | 84.99 MHz | 85.21 MHz |
| Capture prediction | 397.57 MHz | 327.47 MHz |
| Automatic enable replicas | 1 | 0 |
| Slot-free ALUT2 BEL | (30,26,48) | (30,20,19) |
| Ready-pin geometric distance, `abs(dx)+2 abs(dy)` | 97 | 109 |
| Port-1 skid FF BEL | (30,26,28) | (45,20,16) |

The trial moves 14,840 common cells to different BELs, so it is a whole-design
placement response to the changed estimate. The key ready sink moves **away**
from the actual output pin under the stated geometric metric. The predictor
itself differs between runs, so the placement MHz values are not comparable
as if they were a common calibrated timing model.

After six timeout roots are replaced by 41 LUTs, the trial stops at the
conservative retained-enable hook. Its expected ALUT4 driver is still the
same mask and type, but has moved from ordinary LAB (24,16) to LAB (36,15),
which contains arithmetic carry logic. That fails the hook's ordinary-LAB
precondition. This is **not** evidence of an illegal BEL or a failed physical
route. No full route, RBF or final analogue Fmax was produced for the trial.

The pin-aware predictor is not stacked into the accepted result. The prior
116.918037 MHz route remains the retained baseline; 130 MHz is unresolved.
The next useful test is bounded around the accepted placement and Quartus's
nearby feedback/state copies, with both the state-input and timeout/control
boundaries guarded. Another unconstrained global placement response is not
justified by this trial.

## Evidence and scope

The adjacent [JSON record](2026-09-28-ramtest-hps-ready-pin-predict.json)
contains the binary/source identities, exact-control artifact list, placement
measurements, BEL changes and 27 raw artifact hashes. Raw scripts and outputs
are in `out/ramtest-hps-pin-predict/`; the analysis regenerates byte-identically.
The focused predictor test and backend CTest pass. This is a host-only compiler
probe. It changes no production RTL, recipe, producer lock, ABI or shared
contract, and performs no hardware operation.
