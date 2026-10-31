# DE25 consolidated ZX81 HPS-first rebuild

Base: `8e432b82f799c5314f6580b5127b6f9e634a8f0e` (`origin/main`).
Quartus source producer: `42cb6e7ec2f7f28852db9c22cc6e43a6c2113f88`.
PR #1299 was folded with merge commit `3d8ce4449`; PR #1300 was folded with
merge commit `61353263b`. The producer commit also contains focused fixes for
the integration and simulation failures and evidence wording.

## Quartus 26.3 build and STA

The locked Terasic DE25-Nano rev B archive hash is
`f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`.
Quartus Prime Pro 26.3.0 Build 114 completed full compilation and assembly:
0 errors, 333 warnings; fitter status successful. The original 100 MHz system
and 74.25 MHz pixel clocks were retained. The builder produced a raw SOF,
complete SOF, HPS-first JIC and Linux core RBF; none was programmed.
Their hashes are bound in `build.json` and `linux-programming.json`.

STA is **not qualified**. The 74.25 MHz video clock setup row is −6.215 ns
with −18.467 ns TNS. The reported path launches from
`soc_inst|subsys_periph|rst_controller|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out`
on the system clock and ends at `fes_video_inst|visible_line` on the pixel
clock, with no SDC exception. The 100 MHz system setup row is +1.483 ns; the
minimum listed hold slack is 0.000 ns. The complete summary and compressed
full STA report retain the rows and path details. No timing constraint or check
was relaxed. The earlier 30 October timing numbers do not carry over to this
producer revision. A passing timing seal is not issued; this path needs focused
clock-domain and reset-path review.

This remains a diagnostic-only build: `build.json` says timing acceptance is
`not-qualified`, hardware acceptance is `not-run`, and production admission is
not established. HIL was waived and not run for this task. No kit or capture
path was used. `/data` was not writable in this job, so the large Quartus build
outputs remain under the ignored task-worktree `out/`; only compressed reports,
logs and hashed receipts are committed here.

## Host validation

- `python3 -m unittest -v test_de25nano_combined test_de25nano_display test_de25nano_zx81_hps` from `sources/misteross/tests`: 46 tests passed.
- `PATH=/home/deano/tmp/fixjob-fes-1297-20261031-054639/out/de25-1297-validation/venv/bin:$PATH /home/deano/tmp/fixjob-fes-1297-20261031-054639/out/de25-1297-validation/venv/bin/python scripts/test_changed.py --base origin/main`: passed. Parent suite: 1,244 tests, 42 skipped; expanded suite: 1,669 tests, 75 skipped; every lane passed.
- `python3 scripts/check_doc_links.py`: passed.
- `git diff --check`: passed before the evidence files were added; it is rerun before commit.

The final integration-fix commit adds no shared-definition or module-contract
changes. This receipt records host tests and a successful Quartus compile with
an explicit STA failure; it is not a hardware acceptance record.
