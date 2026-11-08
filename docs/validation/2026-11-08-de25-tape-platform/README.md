# DE25 strong initial-tape firmware connection

Source `f4969d2881bd5f827700707b6c24c1173d0174cf`, based on `f73669a3d0a0a729285beafd0819aabe29199654`. This is secure-provider/software
integration with modeled hardware and an ARM64 firmware link. TRWU8CBL was
untouched. No current SDK/boot selection or native CPU transport enables the
new service, and the overall native core-loading goal remains incomplete.

The owned exporter includes the tape service. Its default channel is unready;
`FES_NANO_INITIAL_TAPE` connects the strong supplier only with the complete
shared-USB/SD/held-ROM computer composition. Actual phase1 dispatch holds the
existing secure lock, checks current owner/generation, and retires/contains a
selected uncertain transfer. Menu, closed, foreign and replay requests cannot
issue tape endpoint I/O. A healthy unused service permits no-tape return/reload.

The supplier binds the private accepted build, completed configuration and ROM,
runtime READ phase, inactive SD, confirmed shared USB and live CPU/reset/master
scope. Whole observations frame preparation, each 512-byte chunk and completion;
every CSR independently checks live access before/after. A second fixed-source
SHA-384 rejects mutation after the final data chunk was copied. All work keeps
the original one-second deadline; no reset, Run or retry is added.

Thirty-eight combined scenarios execute the actual dispatcher, strong callbacks,
pinned Terasic SHA, C tape engine, ROM, SD return and master-stop path with
injected MMIO/mailbox/endpoint/CPU/timer behavior. Five incomplete compile
selections reject. The lock model asserts every tape CSR occurs within dispatch.
Transfers cover 1, 512, 513, 917 and 16,375 bytes, repeated roles/no-tape sessions,
identity/source/ACK errors, late CPU/reset/USB changes, expiry and replay.

All 12 affected checks pass, including 1,701 parent regressions (three existing
skips), 1,595 FPGA software tests (79 skips), native firmware/FPGA integration,
SD lifecycle, producer/Avalon checks and captured-computer simulation (273
top-level passes/skips). Committed-source `make check` passes with 52 generated
consumers and 37 fixture copies. The parent suite caught a real source-inventory
omission: all eight newly installed firmware inputs now enter the shared cold
chain identity. The focused 29-test boot regression and full parent suite passed
after that repair; original failed logs remain retained.

A fresh pinned Terasic export links complete ARM64 BL31 with the tape flag and
passes the original secure RAM ELF checks. The private segment ends at
`0x5e200`, leaving 7,680 bytes before the unchanged `0x60000` limit. This uses a
retained board-resource profile as a build fixture; it is not a fresh matched
boot chain, cumulative secure-stack proof, timing or hardware acceptance.

Next is the eligible native CPU hook and matched SDK/entry/cold selection,
followed by joined native/App checks, a new matched boot chain and a fresh
bounded board procedure. The old tape trial remains consumed and cannot replay.
See the [machine-readable result](result.json) for exact hashes and boundaries;
closed logs and inputs are in `out/nano-tape-platform-20261108`.
