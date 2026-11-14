# First full-array attempt stopped at its fixed bound

The frozen ffd41ffdd operator reached its600-second console limit on pass1.
It saved4,239,360 bytes in1035 complete reads; those bytes independently replay
against exact packet replies and POST ownership grants. This is a partial prefix,
not a complete backup. The reservation is retired, pass2 never started, and no
resume or retry is authorized by this record. The model had completed16MiB in
about102seconds; actual hardware transport was much slower.

The hard timeout killed the console before an acknowledged QSPI CLOSE. The
flash-session state is uncertain and needs one separately admitted owned cleanup
CLOSE before another read. Host cleanup independently passed: no task namespace
members/private server survive,15MHz/auto1/exact USB permissions/private client
bytes+mode600 are restored and UART is free. These host checks do not prove
QSPI ownership was released or CPU Park; neither is claimed.

No image programming, reset/reboot, flash or SD writes occurred. Factory media and
the other kit are preserved. The next candidate retains the same600-second hard
read bound, adds an earlier owned soft stop allowing a unique CLOSE, and tests
selective syscall tracing. A device-free restricted-UID benchmark retained the
selected trace events while running100000 syscalls about44times faster with
seccomp filtering. This is an overhead hypothesis, not hardware throughput proof.
The stopped attempt and raw partial evidence remain retained.
