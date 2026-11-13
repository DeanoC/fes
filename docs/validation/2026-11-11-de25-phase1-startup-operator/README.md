# Phase1-only volatile startup operator

One bounded TRWU8CBL diagnostic selects native boot `548caa50395f3d94` with
its exact 831,488-byte HPS phase1 RBF, SHA256
`c01c97dde2fe75ce7e6145ccce2a52b6fa8449f43731ba0bd2ce94cbfc3d91d4`.
The matched full-SOF autoboot and two installed-core cycles have already passed;
this procedure changes only the volatile initial image. It is not a hardware
result, flash authority or standalone power-on qualification.

The installed Quartus Pro 26.3 programmer documents `p;file.rbf` as a JTAG
configuration operation with optional device index. The new exact-image gate
admits only `p;<selected-phase1>@1` for native startup and the unchanged matched
recovery SOF for recovery. It rejects the full native SOF, phase2 RBF, other paths,
changed bytes, JICs, erase/flash/verify variants, extra arguments and other labels.
The full SOF digest remains a matched-chain reference in native results and is
explicitly marked as not programmed. The programmed image is recorded separately.

A fresh recovery epoch reads fourteen staged files and all three installed
records without network uploads or SD writes. A separate private native epoch
programs phase1 alone, observes automatic startup without `fesboot`, then requires
initial menu configuration from SD, discovery of the existing library without
Install, both selected core launch/return cycles and final Park. All existing
source, claim, cable, clock, vendor-first list, fresh forced scan, one-use fuse,
USB ownership, package/ROM/tape identity and saved-wire gates remain required.

Operator models pass for exact-image admission and its failures, read-only source
checks, receive-only automatic startup, App exchanges, saved-wire replay and
separate private-service lifetimes. Prior passing software checks are reused
only after confirming unchanged deployed code. The current 5,665-file candidate
closure is freshly rehashed. No test input is modified after its freeze.

Bounds remain native 450 seconds, outer 800, host 890, program 90, loader/handoff
50, initial menu Ready 100, launch 100, close 5, menu restore 100 and Park 5.
Current sixty-second parent/top USB stability and a new source/claim/UART/node
preflight precede live admission. First failure ends the attempt with a two-second
receive-only tail; no retry or fallback is permitted. Independent host cleanup
must verify private services absent, restored 15 MHz/auto-adjust, USB permissions
and free UART even on failure.

The spare card remains in the top kit. Factory flash/card and the bottom kit are
preserved; this procedure includes no flash command, SD write, card swap or power
cycle. Actual flash identification/backup/rollback and explicit approval remain
separate requirements before any persistent flash change. Native Ethernet,
current physical HDMI/key actions, broader packages/devices and full appliance
acceptance remain open. `operator-source.json` binds helpers, tools and inputs.
