# Qualified recovery MMC read summary

Base `4e9c602b3`. The preceding native write completed every required stage,
but its independent recovery verifier stopped at the first raw prefix read.
The [exact returned command](captured-result.json) shows a successful read and
fresh zero result nonce. The host expected `blocks read: OK` at the beginning
of a line; qualified U-Boot prints it after its MMC summary on the same line.
That original whole attempt remains failed, with its raw trace and passive
tail preserved at `/home/deano/fes-retained/de25-sd-fixed-writer-native-20261110-r1`.

The verifier now requires one complete captured vendor summary, binding device
0, requested LBA, requested sector count and completed count. Unknown device,
wrong extent, short completion, duplicate summary and bare completion messages
are rejected. The fixture is the literal MMC status line extracted from that
retained command. The exclusive console still requires its own fresh result
nonce and prompt; this parser supplies neither.

All eight focused verifier tests pass. The successful native image and all its
production source/receipt checks remain unchanged by this host-only fix.
No writer is rebuilt or rerun, and the old original-tail plan is consumed.
The next physical operation must be a separate recovery-only boot/readback
with fresh connection, source/file/tool/claim/UART admission and a new one-use
programming grant. It must clear the old volatile marker through that fresh
boot and check the pattern and protected media independently. No independent
readback success, FAT persistence or ordinary core installation is claimed
by this software correction.
