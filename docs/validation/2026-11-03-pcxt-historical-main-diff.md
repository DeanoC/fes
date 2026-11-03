# PC/XT historical-main diff audit

This records a source comparison for a proposed physical diagnostic. It is
not hardware evidence. The producer commit is
`6974817dfecc1e86b89e04429738790af73e0285`; the authenticated main tip at
review was `c25e8de270d0f6ab337df4813f10c39dceb6d7ea`. The producer is on
the tip's first-parent chain. A diagnostic of the producer tests that commit
and its identified artifacts only. The later main tip and its changes are not
hardware-tested by that diagnostic.

The canonical raw diff identity is
`5f6dbe211758571a71d9751baf572cb17519e1029184768708039fb9aa249396`:

```sh
GIT_NO_REPLACE_OBJECTS=1 git diff --raw -z --no-abbrev --no-renames --no-ext-diff \
  6974817dfecc1e86b89e04429738790af73e0285 \
  c25e8de270d0f6ab337df4813f10c39dceb6d7ea -- | sha256sum
```

The range contains #1458, a PC386 speaker BIOS and simulation change. The
21 changed paths are:

```text
.github/workflows/check.yml
docs/validation/2026-11-03-pc386-speaker-timer/compare_8254.py
docs/validation/2026-11-03-pc386-speaker-timer/kf8253_routine_bench.sv
docs/validation/2026-11-03-pc386-speaker-timer/speaker-before-report.json
docs/validation/2026-11-03-pc386-speaker-timer/speaker-report.json
docs/validation/2026-11-03-pc386-speaker-timer/speaker-rtl-report.json
docs/validation/2026-11-03-pc386-speaker-timer/summary.md
scripts/ci_simulations.py
sources/misteross/Makefile
sources/misteross/cores/fes-pc386/README.md
sources/misteross/cores/fes-pc386/firmware/README.md
sources/misteross/cores/fes-pc386/firmware/firmware.lock
sources/misteross/cores/fes-pc386/firmware/rombios/rombios.c
sources/misteross/cores/fes-pc386/firmware/rombios/rombios.h
sources/misteross/cores/fes-pc386/firmware/rombios/source.json
sources/misteross/cores/fes-pc386/sim/speaker_test.asm
sources/misteross/docs/pc386-machine-design.md
sources/misteross/scripts/sim_fes_pc386.py
sources/misteross/tests/test_pc386_machine.py
tests/test_ci_gate.py
tests/test_ci_workflow.py
```

No changed path intersects the PC/XT producer's recorded `source_roots`.
The changed misteross Makefile hunks add the PC386 speaker simulation target.
The Git trees for `sources/FogCast`, `sources/libmister-runtime`,
`sources/mister-packages`, `image`, and `platform` are unchanged, as are the
parent build, software bundle and environment recipes and `core-recipes.toml`.
This is a bounded diff review, not proof that a later-tip build would produce
identical binaries, manifests, receipts, or physical behavior.

The historical diagnostic requires this exact live tip and raw diff identity;
a main advance, force-push, missing local history, or first-parent ancestry
failure rejects admission. A different tip needs a new full-range review and
an explicitly supplied new diff identity. It does not change the existing
exact-tip main mode or waive artifact, target ownership or restoration checks.
