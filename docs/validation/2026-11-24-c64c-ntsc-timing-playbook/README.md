# C64C NTSC timing campaign

The 2fb097638 production search used the pinned source snapshot and failed to produce a passing route. The pixel path crosses the frame-store bank mux. The existing timing constraints, router and 600-second per-candidate limit remain in force.

Run logs are retained locally at the paths in `runs.jsonl` and identified there by SHA-256; those paths are provenance on this host, not portable links. The interrupted exploratory run has unknown argv, environment and GPU identity because its partial log was overwritten; its journal timestamps and 69-second conservative charge are retained rather than inferred.

The rows below distinguish a timeout from a completed route that missed timing. Route-log birth-to-last-write is used where no measured replay duration is available, with lease waits included. GPU lease waits and device identities are in the corresponding JSONL entries. Host load was not measured.

| Run | Seed | Weight | Critexp | Result | System MHz | Pixel MHz | Audio MHz | Elapsed s |
| --- | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| original production search | 8 | 2000 | 5 | timing miss | 55.90 | 66.34 | 182.78 | 578 |
| original production search | 1 | 2000 | 5 | timeout | — | — | — | 600 |
| original production search | 2 | 2000 | 5 | timeout | — | — | — | 601 |
| original production search | 3 | 2000 | 5 | timeout | — | — | — | 600 |
| original production search | 4 | 2000 | 5 | timeout | — | — | — | 600 |
| original production search | 5 | 2000 | 5 | timeout | — | — | — | 600 |
| original production search | 6 | 2000 | 5 | timeout | — | — | — | 601 |
| original production search | 7 | 2000 | 5 | timeout | — | — | — | 600 |
| original production search | 9 | 2000 | 5 | timeout | — | — | — | 600 |
| original production search | 10 | 2000 | 5 | timeout | — | — | — | 601 |
| interrupted exploratory run | 11 | 2000 | 5 | interrupted | — | — | — | 68 |
| exploratory seed search | 11 | 2000 | 5 | PASS | 55.94 | 83.08 | 158.60 | 412 |
| production recipe | 8 | 2000 | 5 | timeout | — | — | — | 600.729 |
| production recipe | 5 | 2000 | 5 | timeout | — | — | — | 600.471 |
| seed diagnostic | 11 | 2000 | 5 | PASS | 55.94 | 83.08 | 158.60 | 498.082 |
| seed diagnostic | 12 | 2000 | 5 | timeout | — | — | — | 600.709 |
| weight 2000 critexp 2 diagnostic | 5 | 2000 | 2 | timeout | — | — | — | 601.291 |
| weight 2000 critexp 2 diagnostic | 11 | 2000 | 2 | timeout | — | — | — | 600.315 |
| weight 2000 critexp 2 diagnostic | 12 | 2000 | 2 | timeout | — | — | — | 600.382 |
| weight 300 critexp 5 diagnostic | 5 | 300 | 5 | timeout | — | — | — | 601.261 |
| weight 300 critexp 5 diagnostic | 11 | 300 | 5 | timeout | — | — | — | 600.422 |
| weight 300 critexp 5 diagnostic | 12 | 300 | 5 | PASS | 56.84 | 79.19 | 204.75 | 415.344 |
| weight 300 critexp 2 diagnostic | 5 | 300 | 2 | timeout | — | — | — | 601.394 |
| weight 300 critexp 2 diagnostic | 11 | 300 | 2 | timeout | — | — | — | 600.433 |
| weight 300 critexp 2 diagnostic | 12 | 300 | 2 | timeout | — | — | — | 601.468 |
| weight 1000 critexp 5 diagnostic | 5 | 1000 | 5 | timeout | — | — | — | 602.282 |
| weight 1000 critexp 5 diagnostic | 11 | 1000 | 5 | timeout | — | — | — | 600.682 |
| weight 1000 critexp 5 diagnostic | 12 | 1000 | 5 | timeout | — | — | — | 600.342 |
| weight 1000 critexp 2 diagnostic | 5 | 1000 | 2 | timeout | — | — | — | 601.575 |
| weight 1000 critexp 2 diagnostic | 11 | 1000 | 2 | timeout | — | — | — | 600.427 |
| weight 1000 critexp 2 diagnostic | 12 | 1000 | 2 | timeout | — | — | — | 600.421 |
| weight 2000 critexp 5 arrival-pin-assignment diagnostic | 5 | 2000 | 5 | timeout | — | — | — | 600.777 |
| weight 2000 critexp 5 arrival-pin-assignment diagnostic | 11 | 2000 | 5 | timeout | — | — | — | 600.532 |
| weight 2000 critexp 5 arrival-pin-assignment diagnostic | 12 | 2000 | 5 | timeout | — | — | — | 600.466 |
| weight 300 critexp 5 arrival-pin-assignment diagnostic | 5 | 300 | 5 | PASS | 54.97 | 81.82 | 176.30 | 487.669 |
| weight 300 critexp 5 arrival-pin-assignment diagnostic | 11 | 300 | 5 | timeout | — | — | — | 600.507 |
| weight 300 critexp 5 arrival-pin-assignment diagnostic | 12 | 300 | 5 | PASS | 58.03 | 76.70 | 199.72 | 376.92 |
| fresh 055bfc production seal | 11 | 2000 | 5 | PASS | 56.09 | 76.42 | 187.76 | 304 |

The seed-11 exploratory run uses the retained 2fb synthesis and passes the producer validation when assessed independently. The fresh 055bfc76 production seal used a different synthesis and embedded build ID. The 300/5 arrival-pin assignment option passed seeds 5 and 12, with seed 11 timing out; both passing routes passed the complete producer evidence validator on private copies. The fresh production seal is recorded below. These routes do not assert hardware acceptance.

The pre-production campaign recorded 21159.9 seconds of conservative total route elapsed. This counts CPU routing and lease wait as if they were GPU time. The GPU-time upper bound charges the full 600-second route cap for historical routes without measured completion duration, then subtracts only logged pre-grant HeAP intervals with one-second margins, and is 19020.6 seconds. Other CPU and unmeasured waits remain charged.

Fresh production seal: FES cbf5a96774fb5748569f44f1266c14aded1ee138, BUILD_ID `e251fee20bce7c13e05806f79c90548b`, RBF SHA-256 `13df5c3ccfb8d6b0b376cf38ea9ebfba324e1ad2cdf6ea4eaee61d677af271dc`. Producer summary status: pass. The run records include every production candidate and retain the prior 055 build separately.

| Production seed | Weight | Critexp | Result | System MHz | Pixel MHz | Audio MHz |
| ---: | ---: | ---: | --- | ---: | ---: | ---: |
| 5 | 300 | 5 | PASS | 56.74 | 80.60 | 221.68 |

The pre-production GPU upper bound is 19020.611 seconds. For the production route, the JSONL records a conservative upper bound of 536.86 seconds: the full 600-second route cap plus a one-second rounding margin, less 64.14 seconds of logged pre-grant HeAP placement. The resulting incident upper bound is 19557.471 seconds, below the 21600-second cap. Independently, the production watchdog journal recorded 414.151 seconds of wall time from first observed route log to process exit, including CPU and lease waits; that measurement is not added to the JSONL bound.

The cbf5a967 exact-recipe production seal completed as recorded above; physical board validation remains separate.
