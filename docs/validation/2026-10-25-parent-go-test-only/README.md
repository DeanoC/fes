# Parent selection for verified Go tests

Both arms ran the same parent command for the file changed by #1117:

```sh
python3 scripts/parent_tests.py --video false --media false \
  --paths-json '["sources/FogCast/host/target_bridge_lease_test.go"]'
```

| | Baseline | Candidate |
| --- | ---: | ---: |
| FES commit | `1bfefd6c` | `7a95fb42` |
| Elapsed seconds | 202.585 | 49.081 |
| Selected tests | 853 | 389 |
| Groups | policy, build, packages, acceptance | policy, build |
| Result | passed | passed |

The candidate includes eight additional policy regressions. Both commands ran
on Powerboat in `/home/deano/fes/out/dev/ci-parent-go-test-only`, with
`GOMAXPROCS=4 GOFLAGS=-p=4`, system Python, and the same changed-path argument.
Elapsed time includes discovering tests and validating the real native/race Go
package metadata. This one pair saved 153.504 seconds (76%) on a shared host;
it is not a guaranteed GitHub CI time. The smaller selected count is reported
separately from that observed duration.

The fast path requires current `TestGoFiles`/`XTestGoFiles` ownership for existing
non-symlink Go tests in reviewed FogCast areas, and retains embedded input and
guide consumers. Missing metadata, unlisted/removed files, production changes,
shared inputs, media consumers, extended modes and full runs retain their old
selection. Documentation readers and unassigned tests remain selected.
Host Go tests/reverse consumers, UI selection and generated consistency are
unchanged. Changes to the parent policy itself still select its full suite.

Focused coverage includes real Go metadata for normal and race-only tests,
unselected build tags and explicitly embedded Go test source, as well as
negative fixtures for missing files, symlinks, metadata errors, mixed inputs,
guides and full CLI plans. The recorded candidate successfully exercised the
actual #1117 package graph, rather than mocked ownership.

`runs.json` contains exact arguments, commits, results, counts and hashes of the
complete reports/logs. Those files remain at `/tmp/fes-parent-go-{base,candidate}.*`
on Powerboat and in the originating checkout's ignored
`out/validation/2026-10-25-parent-go-test-only/` directory.
Use `benchmark.py /absolute/fes base` (then `candidate`) with private checkouts
at the recorded commits to reproduce. It writes separate report, log and result
files under `/tmp`. Evidence is host-only; no kits, images or cores were built
or deployed.
