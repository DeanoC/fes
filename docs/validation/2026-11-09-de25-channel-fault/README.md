# First fatal USB channel observation

The stepped-tape image reached one menu frame on TRWU8CBL, then reported USB
transaction error `0x80` and failed its existing 500 ms channel halt. There was
no tape or core-launch request. The complete corrected trace and consumed image
are retained in the preceding stepped-tape trial record.

The previous trace keeps command history and the original channel interrupt
bits, but not the live queue/transfer/controller state immediately before its
containment attempt. A fatal-channel-only diagnostic now retains 15 sequential
non-destructive register reads: RX peek, interrupt/PIO ownership, channel,
transfer, split route, link/reset, both transmit queues, frame and interval.
It also records the admitted root/device speeds and hub route. The original
interrupt sample remains separate from the later interrupt-register read.
These observations are not atomic and cannot prove containment or input.

The successful transfer path is unchanged. The capture issues no command,
acknowledges no status, consumes no RX entry or payload, samples no clock and
adds no retry. Existing permanent retirement and halt bounds remain. Host tests
verify read order, unchanged packet/register/data state, first-error preservation
when the halt also fails, distinct transaction/overrun status, and no hardware
access through a retired host even after registers change.

The full DWC2 suite passes with `-race -count=1`, as do all six planned affected
checks, including parent Python regressions and generated consistency. An initial
new-test assertion incorrectly assumed the command history was empty after
keyboard enumeration; the test now checks the one-command delta and retained
history. That failure and final passing logs remain in the isolated output.

This is diagnostic software evidence, not a USB repair or hardware pass. A new
matched SDK/entry/BL31/SPL/FIT/SOF chain and fresh single-use trial are required.
The two timing fields in the preceding trace describe software durations, not
remaining USB frame time; no scheduling change is justified by comparing them.
Old-file cleanup remains paused. Full ordinary package/appliance acceptance is
still open.
