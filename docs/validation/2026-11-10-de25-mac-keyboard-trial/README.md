# Offline preparation for a fresh keyboard return trial

The next bounded trial selects native boot ID `bec0efa961847c4d` from the
[matched boot chain](../2026-11-10-de25-matched-boot-chain/README.md).
This step prepares files and tests host logic only. The user is away from the
kit and has deferred physical testing. No kit, card, JTAG, UART, network recovery
session or remote builder is used. Date rollover alone does not resume testing.

Implementation source remains `fed4e4e35b372321970efab495161c3cec804d4d`.
The preparation starts at `b017ff12605a8ad88b90ef52b0f8860b91aceaae`; no runtime,
firmware, shared definitions or FPGA source changes are made here.
The private bundle is `out/nano-mac-keyboard-trial-20261110/`.

Only helper source, the checked scan-probe bytes and the 12 verified package,
selector, ROM and tape files are copied from the previous trial. The upload
plan selects the new FIT and current SDK/composition receipts: 13 fixed purposes
including boot. The ZX81 package retains the 917-byte tape; ZX80 has no initial
tape. These immutable package admission identities are file provenance, not
live programming permission. No previous preflight, programming grant, consumed
reservation, upload receipt, private client configuration or console is copied.
The previous failed trial and all its evidence remain intact.

The current helpers pass 45 Python model/boundary tests and one Go race-enabled
real-package/tape preflight. The checks cover the serial launch/return path,
startup input exclusion, boot/MMU receipt matching, step/pending receipts,
firmware failure decoding, one-use programming and clock restoration. Trial
specific negatives reject the previous boot path, a wrong boot hash and missing
preflight before any claim or command. No live entrypoint is executed by these
tests. Historical traces used by decoders remain historical evidence.

`prepare_offline.py` validates the entire selected native chain, retained
recovery inputs, source maps, sealed project trees, assets and helper files.
It writes a fresh file-only admission record, reloads it against current hashes
and rejects a wrong admission digest or missing ownership predicate. The
successful replay uses an explicit file-only predicate; it does not inspect or
claim the board. The record declares `live_authority: false`.
Preliminary and final receipts remain separate. The final preparation refreshes
the checkout identity after the documentation commit and freezes an inventory
in `offline-preparation.json`. It creates neither the tracked operator source
record nor `trial/preflight.json`; those require a fresh physical-test setup.

When the user returns, verify the continuing top-only lease, current unique
TRWU8CBL USB identity, at least 60 seconds of connection stability, free UART,
available keyboard/monitor, recovery and current source/file hashes. Then freeze
one new bounded attempt. No kit state is inferred from this offline preparation.
TRWU8J8G is excluded. Factory restoration remains waived by the user; preserve
the factory card and leave old-output cleanup paused.

The physical sequence remains menu → ZX81 with tape → keyboard Stop → menu →
ZX80 → owned Quit → menu and final Park. Signal the user only after the first
core is running, then request one Ctrl+Alt+Escape press and release. The first
return needs positive fresh USB report progress and an empty UART input payload.
Visible launcher/core transitions require the user's separate HDMI observation.
Expected logical generation is 5, three menu sessions and five configurations;
firmware bridge closure is generation 29/reset mask 15.

Bounds stay unchanged: human-action wait 60 seconds, core close 5, menu restore
100, initial Ready 100, native 450, outer trial 800 and host 890. First failure
is terminal with a two-second passive tail; no retry or consumed-grant replay.
Cleanup restores the original 15 MHz/automatic JTAG setting, exact USB permissions
and releases only the owned UART/server. A disconnected host does not establish
that FPGA traffic stopped.

This new image has no physical boot, keyboard return or display acceptance yet.
The firmware MAC provider remains unbound and native Ethernet disabled. SD is
read-only, preferences volatile, and broader package/appliance acceptance is
still open. The Linux-free DE10-equivalent core-loading goal remains incomplete.
