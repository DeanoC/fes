# Native computer access timing diagnostic

Source `dd4c6aebebe039478852d846f93b74f4a89c9c2b`, base `3f295b9cd`.

The previous bounded hardware attempt completed FPGA configuration but exceeded the unchanged one-second computer identity limit. Its last checked read took 18.240627 ms. That value combines two secure ownership snapshots and the register access, whose CPU transport separately verifies live address mappings. It does not identify the dominant phase.

The native computer IO now retains durations for ownership-before, register-I/O and ownership-after. Computer asks for these saved values only when formatting a limit failure. It adds the access offset, read/write direction and completion flag, so a prior access cannot be confused with an unissued operation. Successful operations print nothing. The optional diagnostic interface supplies no address, mapping, ownership or retry permission. Timing is never used to accept a command. All existing per-CSR checks, mutex boundaries, one-second/64-poll bounds, device qualification, retirement and stop behavior remain.

Tests cover successful reads/writes, failures before IO, register errors and ownership loss after IO; exact callback counts and retirement are checked. Diagnostic retrieval performs no firmware or register IO. Limit tests preserve errors.Is identity, unchanged acceptance, one issue, and terminal retirement. All eight selected software checks passed, including parent/generated checks, computer transport regressions and isolated native firmware/FPGA launch/failure integration.

The application, entry loader, secure firmware and U-Boot were freshly rebuilt. The two actual sealed diagnostic packages and matched native catalog passed checks. Compiled source/call-path checks include the linked LastAccessTiming method, and the bounded operator fixtures passed. Composition preserves the sealed FPGA payload. Firmware supplier-count evidence is reused from source b7b2c86e7 only after checking every measured firmware source remains byte-identical; no fresh physical timing is claimed.

The next one-attempt procedure remains recovery, seven Ethernet uploads/fresh SD readbacks, then ZX81/ZX80/ZX81 launch/return/Quit with USB ownership checks. It stops at the first failure without retry. TRWU8CBL's original factory card remains safe outside the board, and restoration remains waived by the user. The new image has not run on hardware.

The sealed FPGA dependency remains `/home/deano/fes-retained/de25-tenfoot-native-boot-20261104-r3/matched-candidate.tar.gz`, SHA256 `1479d5fa439c77457a3c19959a97b810b8ce5a6aa0672bb4567519f2d7229d17`. No new synthesis or electrical/timing qualification is claimed. Normal FES package/ROM/media admission, physical controls/video, native Ethernet and DE10-normal-kit readiness remain open.
