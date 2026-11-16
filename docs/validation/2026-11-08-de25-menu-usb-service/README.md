# USB servicing during native menu presentation

The preceding paired board trial stopped after a real menu frame because its
first pending USB packet was not polled within the original 500 ms deadline.
The source repair services the existing synchronous input owner during bounded
frame copies, ownership checks and every 32 KiB of full uncached arena comparison.
It preserves both complete slot images and padding checks. No packet deadline,
memory exclusion, firmware permission or scanout bound changes.

Decoded menu events wait in a bounded 64-event queue until the next App update.
Press/release/press produces two commands, not a coalesced held state. Backend
boundaries discard queued commands and retain the fresh-neutral fence, including
rejected content. Configuration and return still pause/drain the original source;
no background poller or additional pause/resume observations are introduced.
Device error, overflow, cancellation and changed ownership retain the existing
terminal containment and no-replay behavior. The scoped callback is removed
before independent menu closure.

## Software evidence

[software-result.json](software-result.json) records the exact checks and hashes,
against base `b6554238af42d0d4ca344d9149c34f6f5726eef2`. Full logs and source-bound
results remain in `out/nano-menu-usb-service-20261108`.

- A paired fixture presents more than a second of modeled secure/MMIO work.
  The unserviced control expires pending input; the repaired platform preserves
  both press edges, presents the expected pixels and never replays the commands.
- Real DWC2 tests retain the captured first-poll timeout after a 600 ms service
  gap, and deliver press/release/press over 1.5 seconds of regularly serviced
  modeled wire time. These are driver-model tests, not physical USB evidence.
- Keyboard/gamepad queue, boundary, overflow, cancellation, device and owner
  failures pass. The actual App/backend/menu/core/menu path now includes typed
  USB and checks the original two pause/resume pairs and terminal input failure.
- Focused race tests pass. The complete native integration passes 379 top-level
  tests across five existing phases, using patched TF-A functions and Verilated
  FPGA endpoint responses with injected bridges, memory and SDM.
- All 1,668 selected parent cases are accounted for: 1,665 pass and three retain
  their existing platform/tool prerequisites as skips. The initial parent job
  reached its 300-second host cap without a test failure. Remaining cases ran
  separately; a continuation helper could not import one dynamically loaded
  alias by name. Its successful original execution and the remaining successes
  were joined against the actual discovered suite, with unchanged native source
  hashes. Neither interrupted/erroring wrapper is recorded as a passing run.
- Affected planner, whitespace and generated-consumer consistency checks pass.

## Next hardware gate

The last physical candidate remains the failed paired trial. This repair has no
hardware acceptance and does not establish visible output, physical input or
ordinary core-load readiness. The source-matched SDK, entry, secure firmware,
SPL/FIT and composed programming image have now been rebuilt and independently
checked; [boot-result.json](boot-result.json) records their identities and bounds.
The [fresh bounded procedure](../2026-11-08-de25-menu-usb-board-trial/README.md)
still needs read-only device admission before its one attempt. The previous
physical attempt is consumed and must not be replayed.

These hooks cover bounded menu publication; arbitrary blocking application work
still has no general USB latency guarantee. No board command, card write or
factory restoration was performed for this software milestone.
