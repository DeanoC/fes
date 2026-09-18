# Package-only developer preparation — 2026-09-18

Host-only validation on Powerboat, FES branch `feat/core-developer-workflow`
based on `c98cb86`. No component pins, factory package set or kit state changed.

## Real cached package preparation

From `/home/deano/fes/out/dev/library-client/fes`, ran the new command against
each existing selected HIP package:

```sh
make core-dev CORE_DEV_ARGS='prepare --core fes.pong --output out/core-dev/pong-workflow-20260918-01'
make core-dev CORE_DEV_ARGS='prepare --core fes.zx81 --output out/core-dev/zx81-workflow-20260918-01'
make core-dev CORE_DEV_ARGS='prepare --core fes.coleco --output out/core-dev/coleco-workflow-20260918-01'
```

All returned exit 0. Observed wall times were 2.908, 2.865 and 2.756 seconds,
respectively. These are warm package/cache checks and snapshots, not synthesis
benchmarks or cold-build performance claims. No image builder was invoked.

The package IDs remained:

- Pong: `9be4b59993cfd0ac6b347da42decb340235c570d310901468778d6dad15d0120`
- ZX81: `af84d2c7fd0ec920beb3214594688d2b17eb5724aba5ac917cfa37f359cc3a1e`
- Coleco: `61714b569c705d2785e6820caac028ec96f6b5ff25395e39d27a6fdb13c3fc7d`

Each output retains its exact archive, selection and `prepared.json`, including
component revisions and file digests. The selected misteross parser compared
each archive with the resolver's sealed package directory. The acceptance
adapter also read and rehashed the Pong candidate successfully, without executing
acceptance.

## Scope

`make test` passed, including 372 parent tests (36 delegated outer skips),
delegated media checks, platform Go tests and image-script tests. After the
final adapter provenance/test additions, the 70 focused preparation, adapter,
registry and receipt tests passed. `make check` passed with 18 generated
consumers, 15 fixture copies and four copied source pins matching.
Logs are in `out/core-dev/workflow-tests-20260918.log` and
`out/core-dev/workflow-focused-final.log` in the integration checkout.

Focused tests cover registry schema rejection, a fifth data-only recipe,
single-core dispatch, unchanged environment cleanup, build-lock exclusion,
media bounds/digest checks, file snapshots, archive mismatch, failure receipts,
candidate tampering, forbidden identity overrides and explicit execution opt-in.
Media and SMS preparation are fixture-tested in this slice; the real cached
runs above cover Pong, ZX81 and Coleco without media.

No isolated Docker host, network target API, FPGA launch, input injection,
display capture or physical acceptance was performed for this new command.
Existing hardware evidence is not relabelled as command acceptance. Core-specific
input diagnostics and independent onboarding remain follow-up work.
