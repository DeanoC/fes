# DE25 fixed tape publication and secure service

Source `2c5daf36a14005a989f1e2f6d9ce1e4ff2668934`, based on `252c0ff5eff747d38daace8b18571c51d2a9bd91`. This is software integration and ARM64
object compilation. TRWU8CBL was untouched; no new firmware channel or physical
fast path is enabled. The overall native core-loading goal remains incomplete.

The native arena publisher now reuses a completed configuration lease for a
fixed 128-byte tape descriptor and private payload, under the existing pinned
8 MiB mapping. It checks mapping and copied bytes around publication/completion,
validates the entire secure receipt, retains uncertain memory and rejects stale
callbacks, replay and a new tape without a newly completed program lease.

The actual C service samples the descriptor twice under fresh ownership,
checks its private accepted build and fixed-source digest, and runs the actual
held-transfer engine. Only a complete final ownership proof publishes success
fields. Header capture, hashing, transfer and final proof share the original
one-second budget. The engine now receives the last prologue timer sample as
well as its start time, closing a backwards-clock gap at that boundary.

All 14 affected checks passed, including 1,700 parent regressions, native
launch/return/failure integration, runtime software, generated consumers,
contract fixtures, producer checks, firmware/endpoint RTL and 1,595 FPGA software
tests. Parent reports three existing skips; FPGA software reports 79.
`make generate` and committed-source `make check` passed with 52 generated
consumers and 37 fixture copies.

The actual Go publication/receipt codec joins the actual C service/engine at
1, 512, 917 and 16,375 bytes. Portable tests additionally cover all 128 header
byte mutations, 13 service failures, 24 engine failures and eight boundary
sizes. Ownership, source-digest supplier and endpoint are injected in that
join. Separate captured-computer RTL checks pass 273 top-level cases or skips,
including existing actual-engine/native launch, return and failure tests.
Neither test supplies a strong Nano service or physical timing evidence.

ARM64 objects contain 4,720 engine and 1,564 service text bytes. Maximum
individual reported stack frames are 144 and 272 bytes respectively; this is
not a complete BL31 link, secure RAM or cumulative stack qualification.

Initial failed logs are retained. The consistency coverage list was updated
for the new definition, and the exact private contract now uses routine
firmware test modes; unknown neighboring contracts retain broad coverage.
The first FPGA suite observed held GPU status before owner metadata in one
unchanged lease test. Its isolated 17-test control and the complete 1,595-test
confirmation run passed; GPU lease source and tests were unchanged.

The next work is the matched strong supplier, SiP dispatch and eligible Nano
CPU hook, followed by joined physical-provider simulations and a newly matched
boot chain. The earlier tape trial remains consumed and must not be replayed.
The [machine-readable result](result.json) binds the checked source and retained
logs in `out/nano-tape-publication-20261108`. The original failure checkpoint remains
`out/nano-game-tape-trial-20261108/checkpoint.json`.
