# First master clock scope hardware attempt

Compiled native/firmware source `b7b2c86e74af1d099b6d6e6938d7d52eeacddf9c`; operator publication `25dd9a6dad7cdafbfa3a3bb0532439c34e48b71f`. One bounded attempt on TRWU8CBL, no retry.

Qualified recovery, seven Ethernet uploads with fresh SD readbacks, matched native startup with 14 CPU checkpoints, the expected gamepad descriptor and seven idle reports before owned USB generation-2 Pause passed. The first ZX81 FPGA transfer completed all 1,650,688 bytes with data held. ARM-to-COMPLETE measured 13.27658938 seconds, within the existing configuration guard.

Computer identity still exceeded its unchanged one-second limit: elapsed 1.002826647 s, final checked read 18.240627 ms, identity word index 2, 52 reads and three command writes. The previous image measured 30.145792 ms per final checked read. Clock-only master preparation improves this path but does not establish a successful launch. Full identity, Run, USB resume, return and Quit were not accepted; zero core cycles completed. Native entered reboot_required and retained its resources. Master-stop reported complete/data-held; CPU halt was not independently proved. The full panic tail was captured without a secondary error.

Independent trace decoding and cleanup passed: 15M transport unchanged, exact USB permissions restored, private JTAG process gone, UART free. Claim and spare card retained; factory card and other kit untouched. No further board command or same-image retry.

The candidate and all ten software checks are retained at `/home/deano/fes-retained/de25-master-clock-scope-20261105-r1`. Exact first-failure artifacts and frozen procedure are at `/home/deano/fes-retained/de25-master-clock-scope-stop-20261105-r1`. Checksums and source/publication restoration are verified separately. This is a failed hardware diagnostic, not normal FES core-load acceptance.

Next investigate the repeated fresh bridge/ownership observations and secure-call overhead on computer CSR access. Preserve the one-second/64-poll bounds and explicit device-readiness requirements; do not replace live ownership proof with cached permission.
