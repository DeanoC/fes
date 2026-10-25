# Runtime header-rebuild checks

The header regression changed only header timestamps, then called `run-tests`:
it rebuilt all 23 binaries and executed the entire functional suite a second
time. Its Make prerequisite had already executed that suite. The candidate
builds those same 23 targets and retains the new-timestamp assertion for each,
without repeating the functional executions. No test case or dependency
assertion is removed. Routine-mode selection, version checks, production
header checks, archive determinism and active-tree fixtures are unchanged.

A regression runs a real Make fixture through an initial functional pass and
then the header check. It verifies one execution, an actual rebuild, restored
header timestamps, and failure on missing dependencies. On main, it fails with
two executions; on the candidate, both the valid and broken-edge cases pass
their expected assertions.

## Matched stage profile

Powerboat, Linux 6.12.101-p2pdma-p2pdma x86_64, Debian C++ 14.2.0-19,
GNU Make 4.4.1. Baseline `c9888f00`; candidate `b04fa8c2`.
Each arm started without a build directory, in the same private worktree at
`/home/deano/fes/out/dev/perf-runtime-ci-rebuilds`. Prior build outputs were
preserved under that worktree's ignored `out/runtime-profile/` directory.
The `run-tests` and `active-tree-test` stages use `-j2`; the recorded
hardware-support truth-test command has no explicit parallelism flag. The
existing header/version scripts retain `-j16`. The stage sequence is the same
sequence used by `make -j2 test`.

| Stage | Baseline seconds | Candidate seconds |
| --- | ---: | ---: |
| Initial compile and functional tests | 123.527 | 130.154 |
| Header rebuild regression | 97.715 | 44.617 |
| Version rebuild regression | 5.711 | 8.660 |
| Active-tree and archive guards | 49.026 | 59.574 |
| Hardware-support truth guard | 0.043 | 0.023 |
| Total | 276.022 | 243.028 |

All stages passed. The changed stage took 53.098 seconds less (54%); the full
sequence took 32.994 seconds less (12%). This is one pair on a shared host,
without exclusive CPU scheduling. Variation in unchanged stages is visible;
these results do not establish a guaranteed GitHub CI wall-time reduction.
The concrete reduction is from two functional executions to one, while every
binary still undergoes the real header-triggered rebuild.

The header-rebuild logs contain only the success marker because the script
redirects Make output. Matching baseline and candidate `log_sha256` values for
that stage are expected and do not show how many times the functional suite
ran; the timings and regression test provide that evidence.

`runs.jsonl` records exact arguments, results, commits, timings and log hashes.
The original logs are retained on Powerboat at the recorded `/tmp/` paths.
When run locally, the script writes logs under `/tmp` and does not copy them
into the checkout. To reproduce, use separate private checkouts at the two
commits, each without existing build outputs:

```sh
python3 docs/validation/2026-10-25-runtime-header-checks/profile.py /absolute/fes baseline
```

Run the same script with label `header-candidate` in the candidate checkout.
The script appends records to `/tmp/fes-runtime-profile.jsonl`. These are
host-only checks; no image, core build, kit, deployment or hardware acceptance
was involved.
