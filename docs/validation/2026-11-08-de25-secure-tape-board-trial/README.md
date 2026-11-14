# Prepared secure initial-tape board trial

This is a fresh one-attempt procedure for TRWU8CBL only, using compiled source
`211ab49464df5ed93c31f33312a6567f2c4b32e3` and matched native boot identity
`e93ab2e295f96494`. No hardware result is claimed here. The preceding ba2f
image stopped at the original one-second tape deadline and consumed its single
attempt; the earlier f865 image consumed four attempts. Those procedures remain
closed. This image changes the native CPU hook, matched selection and secure
bulk tape path rather than relaxing the operation bound.

The complete off-board candidate validation passes, with 5,520 files in eight
source/project trees. Fresh-hash worker admission passes in under one second
without repeating HPS derivation inside the native test window. Injected loader,
transport, paired-receipt, upload/readback, lifecycle, tape and rejection checks
pass. The actual SDK and retained ZX81/ZX80 packages, ROMs and 917-byte tape pass
native package preflight with no firmware calls. Recovery boot files and the
protected SD prefix are byte-identical to the qualified recovery package.

The bounded test admits one recovery and thirteen fixed uploads/readbacks plus
thirteen fresh reads before native boot. It must observe ZX81 with tape, menu,
ZX80 without tape, menu, ZX81 with tape, menu and final Parked: three verified
computer launches, three returns, four menu sessions and seven configurations.
The tape is the independently ROM/RTL SAVE/LOAD-tested BASIC program `10 REM A`.
Its physical consumption by the ROM is still unproven.

Bounds remain: tape one second; each launch 100 seconds, configuration 20 seconds,
core close five seconds, menu restore 100 seconds, final park five seconds;
native collection 450 seconds, native worker 480 seconds, procedure 840 seconds,
outer worker 890 seconds. First failure is terminal. No replay or fallback is
allowed. Each image program requires its own one-use live JTAG scan. The top-only
private server, temporary 6M/restored 15M clock, exact USB permissions and free
UART require cleanup evidence. Bottom TRWU8J8G remains untouched.

The continuing top claim is retained. Deano waived factory restoration unless
needed; the spare card stays in the board and the factory card remains safe.
Visible display, physical input and ordinary appliance/core-load acceptance are
outside this diagnostic result and remain open. Frozen source/input hashes,
tools, attempt counts and unchanged bounds are in [operator-source.json](operator-source.json).

The first host invocation stopped at the exact USB-node check before any device
access or attempt fuse. The shared hub had re-enumerated both adapters. No JTAG
programming, SD write or native attempt occurred. The abort remains preserved in
[preflight-abort.json](preflight-abort.json); fresh device admission is required
after the user-authorized stable-connection check. The native attempt budget
remains zero used, one allowed.

The physical attempt subsequently stopped during USB initialization, before
Tenfoot Ready or any tape request. See [hardware.md](hardware.md) and the exact
[hardware result](hardware-result.json). The native attempt is consumed.
