# HPS-first ZX81 build for Linux FPGA-manager testing

Base: `c6e978159dcf5277c0c86dbd0f368ff036b314b5`.
Artifact producer: `79d616e85e19d65f0d645b67a85ffe7e142dcdfc`.
Scope: explicit HPS-first configuration profile and supported split-artifact
packaging for the existing ZX81-HPS diagnostic. No shared RTL, clocks, memory,
register schema, Linux image or runtime admission change.

The existing FPGA-first image is rejected by the documented PFG HPS split;
[the earlier record](../2026-10-30-de25-linux-fpga/README.md) retains that rejection
and the two failed Linux transfers. The new profile sets `HPS_INITIALIZATION
"HPS FIRST"`, includes the mode in its computer build identity and checks the
captured assignment before packaging. The default maintenance profile explicitly
selects `AFTER INIT_DONE`.

The real locked Terasic ZIP prepares both modes with distinct identities and
identical captured source bytes, HPS/EMIF parameters and top-level RTL. Sixteen
focused build/packaging tests pass. Full Quartus Pro 26.3 compilation succeeds.
PFG enriches the new SOF with the preserved SPL and now accepts `hps=on`, creating
both phase artifacts. No programmer is invoked and no device is changed.

The receipt binds:

| Artifact | SHA256 |
| --- | --- |
| Raw compiled SOF | `633201e014bb4e82c18d7cc49dfca25928568da8e796769d142c399abdf5da3e` |
| Complete SOF | `0a9475308c4411664083377e7865ad7dedac287bc3b8c4b6fcb520386c7fcc5f` |
| HPS-first JIC | `fefe5246045354d665fbc195dad5350a0e23d1e3bf33516b93b0411832bfde64` |
| Linux core RBF | `7130375986bd59633d856beb6cf69db238432a06b84fa0ba1900ffd556ca180a` |

Computer build ID is `06ee047f143317df0fd1d98717b2b991`.
The original 100 MHz system and 74.25 MHz pixel clocks remain selected. Reported
system setup slack is 1.718 ns and hold is 0.000 ns; pixel setup is 3.333 ns and
hold is 0.041 ns. All retained summary rows are nonnegative with zero TNS.
This is reported diagnostic timing, without external IO, MTBF, boot-firmware,
physical reset or board acceptance. No clock/constraint is relaxed and no seed
reroll is used.

Outputs remain under `out/hps-first-build/hps/` in this task worktree. The JIC is
an unprogrammed candidate. Before normal runtime admission, verify the new
complete SOF boots the approved FES card and the separate core loads through
Linux while preserving the boot UUID/SSH, then identify and exercise the actual
computer endpoint. Failed-load recovery and hardware buffer/reset fencing remain
separate gates in [#1298](https://github.com/DeanoC/fes/issues/1298). Factory QSPI
and the approved SD baseline must remain available for recovery; a persistent
flash change needs explicit authorization after its rollback procedure is ready.

`make test-changed` passes against parent `c6e978159`, including the selected
parent/media checks, generated consistency and FPGA source/simulation suite.
The actual ZX81 machine keyboard/tape/transport regression passes. Complete
logs are retained; documentation links and whitespace checks also pass.
