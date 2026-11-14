# Held and running fetch activity verified on TRWU8CBL

Compiled native/firmware source `22f909e9c0de932e83ccdc5aa346c50d7de1ed18`; operator publication `daa153275`. One bounded attempt, no retry.

Serial Tenfoot completed ZX81 launch/return, ZX80 launch/return and ZX81 launch/Quit, with native generations 1/2, 3/4 and 5/6. Every launch required two identical fetch-count/address samples under Hold and positive counter progress after one Run. The package/build identity, owner and generation were independently decoded from each raw observation after FPGA completion and before published Running.

| Launch generation | Held samples (counts) | Forward fetch delta | Whole Hold/Run check (ms) |
| --- | ---: | ---: | ---: |
| 1 | 0 / 0 | 38429 | 245.487 |
| 3 | 0 / 0 | 38413 | 245.521 |
| 5 | 0 / 0 | 38430 | 245.436 |

Each check used one progress poll and passed the unchanged one-second bound. All three full configuration transfers passed the existing 20-second guard. The same physical gamepad owner remained confirmed at USB generation 2, with pause/resume boundaries and 43 idle reports before final owned Stop at generation 3. Exit reported Ready at native generation 6. Three complete/data-held master-stop receipts were present.

Recovery, seven fixed Ethernet uploads/fresh SD readbacks and all 14 native startup checkpoints passed. Independent raw command/firmware/fetch replay and host cleanup passed: unchanged 15M, exact USB permissions restored, private JTAG process gone and UART free. Claim/test card retained; factory card and other kit untouched. No factory restoration or further board command.

This verifies instruction-fetch bus activity under the selected Hold/Run control on the matched real FPGA image. It does not prove retired instructions, correct emulation/video or CPU halt after return. Physical button/movement mapping was not exercised. Normal package/ROM/media admission and native Ethernet remain open, so complete DE10-equivalent readiness is not achieved.

All seven selected software checks passed before the attempt. Candidate/source retention is `/home/deano/fes-retained/de25-fetch-launch-20261105-r1`; exact successful traces/procedure are `/home/deano/fes-retained/de25-fetch-launch-hardware-20261105-r1`. Archive checksums and source/publication restore are verified. No working artifacts were deleted.

Next qualify actual USB reports through the running core's input path, then implement the qualified ROM delivery/profile contract needed for normal package admission. The existing linked-ROM rejection stays until that path exists.
