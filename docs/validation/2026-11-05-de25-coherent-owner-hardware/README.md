# Shared USB and selected core launch cycles pass

Compiled native/firmware source `7f3cfa92c87963238162e208b5aac909a64c8e13`; operator publication `1ab54ed17`. One bounded attempt on TRWU8CBL, no retry.

The serial Tenfoot App completed ZX81 launch/owned return, ZX80 launch/owned return, then ZX81 launch/owned Quit. Native generations were 1/2, 3/4 and 5/6. Both selected sealed identities were independently decoded from the saved raw console. All three full FPGA transfers completed, with ARM-to-COMPLETE times 12.670803 s, 12.620425 s, 12.676894 s. Launch commands took about 73 seconds each, including SD retention and firmware configuration; return/exit took about 2.3–2.4 seconds. The unchanged one-second/64-poll computer and 20-second configuration limits passed. No timeout was widened and no ownership result was cached to grant access.

The same physical gamepad owner remained confirmed at USB generation 2. Polling paused during mutations and resumed between sessions, with 44 idle reports by final Stop at generation 3. This verifies shared polling/ownership composition; physical button/movement mapping was not exercised. The final App exit reported Ready at native generation 6. Three independent complete/data-held master-stop receipts were present.

Qualified recovery, seven Ethernet uploads/fresh SD readbacks, matched boot and all 14 CPU checkpoints passed. Independent raw command/firmware replay and host cleanup passed: unchanged 15M, exact USB permissions restored, private JTAG process gone and UART free. Claim/test card retained; factory card and other kit untouched. No factory restoration or further board command.

The new complete cold/CPU/producer observation and single-observation dispatcher remove the previously observed identity timeout for this exact image. All eleven final software checks passed. Failed compiler drafts remain retained and were never programmed. This is physical acceptance of the bounded diagnostic procedure, not ordinary FES package/ROM/media admission or complete DE10-equivalent readiness. CPU execution/fetch activity and halt were not independently measured, display is serial state only, and native Ethernet remains unqualified.

Candidate/source retention: `/home/deano/fes-retained/de25-coherent-owner-20261105-r1`. Exact successful hardware traces/procedure: `/home/deano/fes-retained/de25-coherent-owner-hardware-20261105-r1`. Both archives are checksum-verified; source/publication restore is separately verified. No working artifacts were deleted.

Next qualify actual execution and physical input in the running core, then connect normal package/ROM/media admission to a qualified Nano profile. Retain the distinction between controlled diagnostic packages and normal kit core loading.
