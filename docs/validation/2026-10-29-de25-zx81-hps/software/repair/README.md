# DE25 ZX81 tape RAM repair checks

This record covers producer commit `5a7382c4b5c17173f48bd074847acc0acc22de93`.
The earlier `786e6383b3` receipts remain in the parent directory and are unchanged.
[Fresh CI run 40814412866](https://github.com/DeanoC/fes/actions/runs/40814412866)
completed successfully: all 10 executed jobs passed, with no gate failures.
The planned `simulation-tools` and `fpga-simulation` jobs were skipped.

CI checked out merge `28054f4d7dd76469d92199c5f4c2e03ad7664912`.
It and the producer commit have the identical complete Git tree
`52d53dfda7919fede37563bfbddade4ada318f7e`.
[Git verification](git-tree-verification.json) records both revisions and tree
IDs; [the command log](git-tree-verification.log) retains the fetch and tree
queries. The CI checkout and final integration log independently name that merge.

## Fresh CI coverage

| Job | Retained result |
| --- | --- |
| plan | 102 planner/metadata cases and 74 documentation-reading cases pass; documentation links pass |
| parent-tests | 1,090 cases, including 2 skips |
| fpga-producer | 1,448 cases, including 58 skips; current build guards and client wrapper pass |
| consistency | 31 generated consumers, 37 fixture copies and 0 copied source pins match; unused-code check passes |
| contracts | Python groups of 4, 10 and 2 cases, generated fixtures and package Go checks pass |
| host (1) | Binary build, artifact functional checks, first race shard and expansion functional checks return 0 |
| host (2) | Second race shard and host/expansion concurrency checks return 0 |
| host (3) | Third race shard, appliance race checks and UI tests return 0 |
| runtime | Native software suite returns 0, including runtime, GP, MMIO, FPGA-manager, input, video and daemon checks |
| integration | All planned software checks pass |

[ci-final.json](ci-final.json) retains the completed workflow, all job/step
receipts and the empty gate-failure list. [ci-initial.json](ci-initial.json)
retains the initial in-progress observation. Each executed job has a
`ci-job-<id>-summary.json` with its URL, source-log line numbers and selected
conclusions. Full CI logs were fetched through the GitHub connector to extract
these summaries and are not copied here.

Reported unittest counts include skips; nested fixture runs must not be added
to their enclosing suite. Go package pass lines are not individual test counts.

## Repair-local evidence

| File | Retained result |
| --- | --- |
| [focused-28.log](focused-28.log) | 28 current build/client cases pass |
| [consistency.log](consistency.log) | Generated consumers and fixture consistency pass |
| [endpoint-simulation.log](endpoint-simulation.log) | Four focused endpoint groups pass: identity/HOLD/RUN/HID and CRC/tape; coherent responses/BUSY; malformed accesses; stopped-clock reset and late-write containment |
| [delta-affected-plan.log](delta-affected-plan.log) | Delta from `786e6383b3` selects parent and FPGA software lanes, with no core consumers |
| [hil-plan.json](hil-plan.json) | Strict deployment-impact decision is `NO_DEPLOY_CHANGE` |

[local-file-manifest.json](local-file-manifest.json) binds the five copied inputs
to their original paths, byte lengths and SHA-256 values.
[local-checks-summary.json](local-checks-summary.json) retains the focused
conclusions, delta command labels and HIL classification. The delta snapshot
is planning evidence, not an aggregate local test result. The [whole-PR CI
plan](ci-affected-plan.json) selects parent, host, runtime, contracts and FPGA
software lanes, with no core consumers; fresh CI passes each selected lane.

The endpoint run uses behavioral RAM and direct digital clocks. It does not
prove physical M20K inference, PLL/HDMI behavior or board acceptance. Quartus
build and timing evidence are recorded separately. `NO_DEPLOY_CHANGE` is a
deployment-impact classification; it does not admit DE25 hardware access.
No new tests or hardware operations were run while collecting this record.
