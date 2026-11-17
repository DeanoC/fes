# Downloaded package catalog installation

Against base `883213b60`, an HTTP export downloaded into an admitted RAM plan can
now be attached to the normal Tenfoot core catalog with `AttachProgramInstall`.
The existing SD-staged adapter delegates to the same publication path. The user
still chooses Install; publication never writes storage or programs firmware.
Both adapters share the existing single retained-source slot.

The integration test closes the export server before publication, then checks
Available → Install → Setup → installed status → launch → return through the
normal host client. Eight refusal cases cover missing backend/context/plan,
foreign loader, cancellation, active session, repeated attachment and conflicting
installed identity. Rejected publication changes neither catalog nor devices.
Existing HTTP integrity failures, real FAT installation/reboot discovery and
installer/launch failure suites also pass in the same integration run.

All seven selected software gates pass. The parent run executes 1,935 tests
with three skips. Native lifecycle verification uses the actual patched TF-A C
and Verilated FPGA endpoint, simulated bridges/memory/SDM and race-enabled Go.
The AArch64 TamaGo SDK cross-build passes; all 2,131 selected SDK source hashes
and 469 integration source hashes match. See `result.json` for artifact identity.

This is software integration and cross-build evidence. No board operations,
matched boot-chain assembly or physical acceptance took place. The shipped
profile keeps native Ethernet disabled. There is no automatic fetch, network
listener or multi-package source catalog introduced by this change. Broader
core/device acceptance, native Ethernet qualification and full appliance parity
remain open. Source-bound artifacts are retained outside the worktree at the
path recorded in `result.json`.
