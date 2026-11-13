# Bounded secure-tape trial stopped during USB startup

Compiled source `211ab49464df5ed93c31f33312a6567f2c4b32e3`, native boot
identity `e93ab2e295f96494`, operator `d9e558209de4e7820ce0a4f83071085408df369b`.
The changed native image consumed its one allowed attempt. It must not replay.
The preceding ba2f image consumed one attempt; the earlier f865 image consumed
four. The initial host-only USB admission abort consumed no native attempt.

Qualified recovery, thirteen fixed uploads/write-readbacks and thirteen fresh
reads passed. Both image programming operations used separate consumed live JTAG
admissions. Matched secure native boot passed. Firmware reported USB setup
status zero and the pin/ownership checks passed with data held.

The native DWC2 driver then stopped at its original 500 ms root-connect wait:
`address=0x440 value=0x1000 mask=0x1 want=0x1`. The connection status bit was
clear. The port-power request bit was set; this is not an electrical VBUS
measurement. Shared USB Ready and Tenfoot Ready were never observed. No launcher
command, tape transfer or verified computer launch/return occurred. This run
provides no hardware timing result for secure bulk tape loading.

Deano confirmed the hub/controller were connected and powered, then reported
replugging the controller on the kit and seeing power flicker, possibly resetting
the board. Its timing relative to the saved failure is unproven. The trace does
not establish a USB driver regression or a definite electrical cause. Current
board state is unverified after that possible physical reset; final Park was
not proven. No further native command or reprogramming followed the failure.

Independent cleanup passes exact USB permissions, recorded 15M clock readback,
owned private JTAG-server termination and free UART. The continuing top-board
claim stays held, the spare card stays in place, factory restoration remains
waived unless needed, and bottom TRWU8J8G was untouched.

Next is stable board/peripheral-state verification and a separately frozen
bounded procedure with explicit attempt accounting. Preserve this failure and
the preceding tape deadline failure. Ordinary package/core-loading, physical
tape consumption, display/input and native networking acceptance remain open.
See [hardware-result.json](hardware-result.json) for the evidence join.
