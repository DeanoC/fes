# DE25 synchronous-reset option comparison

This investigation rejects `SYNC_RESET=1` as a repair for the current MENU
RDC-50003 findings. The supported option compiles successfully but leaves
all twelve reported reset-chain heads and both failing clock-domain groups.
The product retains `SYNC_RESET=0`; no RTL, producer policy, constraints,
kernel or runtime change is selected.

Published source before this record is
`147995e3f6aa2ddb7db909db48cbf26b2bf8e78d`. The protected baseline's actual FPGA
producer is `72716153eb7be7b62917ed685d5d7380347a2d7d`, with Linux RBF SHA-256
`e18324235e32c86b643b24dba18deab344628cd9fe0a41bf9d6a3ea76836fde4`.
This experiment changes a private preparation only and produces no new
canonical candidate, Linux RBF or package.

## Actual comparison

The installed Pro 26.3 FIFO implementation retains its internal read/write
reset synchronizers with either setting. To check the actual fitted result,
the existing recipe prepares a fresh canonical-profile project from the
locked Terasic ZIP. One integration-script value selects `SYNC_RESET=1`
for both control and memory bridges before Platform Designer creates them.
The normal integration and IP-generation tools then generate a consistent
configuration. Both bridge wrappers and all four specialized FIFO wrappers
are checked for the selected mode; no generated parameter check is bypassed.

All six fresh vendor reset SDCs receive the existing canonical v3 strengthening.
Their resulting bytes exactly match the baseline's strengthened reset SDCs.
No new exception, clock adjustment, board delay or waived finding is added.

| Actual operation | Result | Seconds |
| --- | --- | ---: |
| Fresh integration | pass | 22.702 |
| Clear generated IP | pass | 1.316 |
| IP generation | pass | 73.630 |
| Synthesis, fit, signoff and SOF assembly | pass | 340.435 |
| Complete job | pass | 439.2 |

| Signoff observation | Baseline | Synchronous-reset experiment |
| --- | --- | --- |
| System-clock reset-chain heads | 6 | same 6 heads |
| Pixel-clock reset-chain heads | 6 | same 6 heads |
| RDC-50003 failing groups | 2 | 2 |
| Raw failed rules | TMC-20012:30; TMC-20011:2; RDC-50003:2 | same counts |
| Rule inventory | 85 | 85 |
| Reported timing checks | 69 pass | 69 pass |
| Waived findings | 0 | 0 |

The exact head names, generated-mode inputs and SDC hashes are in
[the comparison](comparison.json). This checks equality of the reported heads,
not functional equivalence of the two reset implementations. Some upstream
reset-source names move through different generated controllers. No fresh
asynchronous-input review is run for this rejected diagnostic, and no review
or physical evidence is transferred from the baseline to its new fit.

The final signoff-report SHA-256 is
`59c1700a7fe7bf28e3a693f9ac4b5645e3d8410dc481a613da35417af2d8f4bb`.
The public canonical timing parser independently rechecks the restored raw
report and summary. Compilation success does not grant package admission.

## Retained controls and limits

Three earlier operator attempts remain failed and are retained:

- Changing generated wrapper defaults alone fails elaboration in 26.2 s:
  all four specialized FIFO parameter checks reject the mismatch. The final
  job record is authoritative; its incomplete intermediate receipt is retained.
- A saved-project API attempt fails in 5.5 s because the restricted Tcl
  interpreter does not implement `file normalize`.
- Correcting the paths reaches the saved component, then fails in 6.5 s:
  that fixed component exposes no parent `SYNC_RESET` parameter. This leads
  to the fresh-preparation method above.

These failures are not counted as passing tests. The original fitted
source/RBF/database bindings remain unchanged after all attempts. Independent
local restoration verifies every archived file, the exact single integration
parameter change, six generated mode inputs, six identical strengthened SDCs,
the public timing-parser result and all 374 unchanged product MENU inputs.
[Retention](retention.json) binds the full fresh project, reports, scripts and
original failures.

No product implementation or test runner changes, so the prior exact-input
software evidence is reused; no new full software-suite pass is claimed.
Reset-domain coordination, physical reset/boot/allocation epochs and native
recovery remain unqualified. HDMI output timing, an eligible independent RBF
rebuild, admitted factory/card and full native MENU/catalog/ZX81 acceptance
also remain required. No kit or installed state is accessed, and both
consolidated PRs remain owned by `lane:deano`.
