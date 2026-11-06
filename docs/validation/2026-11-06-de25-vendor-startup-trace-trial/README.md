# One bounded vendor-startup trace trial on TRWU8CBL

This freezes a fresh procedure for compiled firmware `7e90d783e` and the
byte-exact native application/entry from `31d716957`. The matched file-chain
validation is recorded in the sibling vendor-startup-trace directory. This
record authorizes no inference of board boot or core-load acceptance.

The procedure requires ordered early RAM entry/pass and all 15 vendor markers
before accepting U-Boot or sending native input. Its complete startup/result
model rejects missing or invalid guard/vendor traces. The same physical
recovery, eight fixed uploads, eight fresh SD readbacks, native checkpoints,
shared USB pause/resume and three ROM launch/return/Quit cycles remain gated.
An unsuccessful boot records partial progress and sends no fesboot command.

Bounds remain prompt50, startup100, launch100, configuration20, control5,
native450, child480, work740 and host850 seconds. One attempt is permitted;
the first failed gate stops the run. Prior stops are retained separately:
one programmer failure, one host setup stop, four ROM deadlines and three
pre-native startup stops. The consumed f40 image is not replayed.

The normal-user shared Quartus lock encloses the test. The claimed top kit is
revalidated by serial, exact USB path, private JTAG/chip qualification and UART
ownership. The temporary 6M programming link restores the original 15M with
readback; CPU/FPGA operating clocks are unchanged. Cleanup independently checks
USB permissions, private JTAG termination and UART release. It does not prove
CPU/FPGA halt. The claim and spare card remain retained; the factory card and
other kit are untouched, with factory restoration waived by the user.

All helpers are frozen in operator-source.json. Five fixture groups plus source
provenance rejection tests pass before live admission. A preparation fixture
initially lacked its source ledger; its failed log and helper snapshot remain
retained, and the corrected transport fixture passes. No device was opened by
those preparation tests. This publication precedes live preflight and hardware.
