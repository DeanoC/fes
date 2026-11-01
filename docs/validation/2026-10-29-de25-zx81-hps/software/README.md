# DE25 ZX81 HPS software checks

This record covers implementation commit `786e6383b32617af8baeb32e9de6cf99231a19f9`.
[CI run 40812839901](https://github.com/DeanoC/fes/actions/runs/40812839901)
completed successfully. All 10 executed jobs passed, including the final integration
gate. The planned `simulation-tools` and `fpga-simulation` jobs were skipped.

CI checked out PR merge `66d0cff98c323dd176850d48c853ac3272ef5532`.
Both that checkout and the implementation commit have the complete Git tree
`1dd77c0a82501cc70bd9a59544af2267933e7371`. The checkout log and independently
read tree IDs are retained in [ci-final.json](ci-final.json); the producer source
and CI checkout therefore contain identical repository bytes.

## CI coverage

| Job | Result retained from its log |
| --- | --- |
| plan | 102 planner/metadata cases and 74 documentation-reading cases; documentation links pass |
| parent-tests | 1,090 cases, including 2 skips |
| fpga-producer | 1,448 cases, including 58 skips; all 10 new build guards and the client protocol/file-backend wrapper pass |
| consistency | 31 generated consumers, 37 fixture copies and 0 copied source pins match; job also passes its unused-code check |
| contracts | Python groups of 4, 10 and 2 cases; package Go checks and generated fixture checks pass |
| host (1) | Host binary build, artifact functional checks, first race shard and expansion functional checks return 0 |
| host (2) | Second race shard plus host/expansion concurrency checks return 0 |
| host (3) | Third race shard, appliance race checks and UI tests return 0 |
| runtime | Native runtime suite returns 0, including protocol, MMIO, firmware-manager and hardware-access guard tests |
| integration | All planned software checks pass |

Each executed job has a `ci-job-<id>-summary.json` containing its URL, source-log
line numbers and selected conclusions. The complete job/step receipt is in
[ci-final.json](ci-final.json); [ci-initial.json](ci-initial.json) retains the earlier
in-progress observation. Full CI logs were fetched through the GitHub connector
to extract these summaries and are not copied here.

Unittest counts include their reported skips. Nested fixture runs are retained
as separate excerpts and must not be added to their enclosing suite. Go package
pass lines count package results rather than individual test functions.

## Finished local checks

| Log | Result |
| --- | --- |
| [focused-28.log](focused-28.log) | 28 client/build cases pass |
| [planner-87.log](planner-87.log) | 87 planner/policy cases pass |
| [consistency.log](consistency.log) | Generated-consumer and fixture consistency passes |
| [doclinks-tests.log](doclinks-tests.log) | 6 documentation-link cases pass |
| [doclinks.log](doclinks.log) | Repository documentation links pass |

[local-log-manifest.json](local-log-manifest.json) records the original log paths,
byte lengths and SHA-256 values. After the matching-tree CI jobs passed, the
root agent explicitly stopped the duplicate local affected runner. Its stable
[partial log](local-affected-partial.log) and [stop record](local-tail-stop.json)
are retained. The make wrapper exited 2 after termination; this is not a local
aggregate pass.

[plan-coverage.json](plan-coverage.json) maps all 10 commands in the retained
local plan to their CI jobs or local evidence. The two planner suites, committed
whitespace check and all selected software lanes have passing CI coverage.
Working-copy whitespace is separately evidenced by the sequential local runner
progressing beyond that step. The local and CI changed-path sets match.

These receipts remain frozen to the implementation commit and full tree named
above. A later tape RAM adaptation or other source change needs its own relevant
checks and fresh CI; it does not inherit this result.

These are host-only software checks. Tests with hardware-related names exercise
software guards and simulated interfaces; they do not establish physical DE25
acceptance. This record contains no new RTL simulation or Quartus execution.
The standalone machine/endpoint and compiled artifact evidence are recorded
separately. Production device access, firmware pairing, physical clocks, HDMI,
reset lifetime and first-board Linux boot remain outside this software result.
