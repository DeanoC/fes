# Startup input release fence and incomplete boot-chain build

Source7b30e31ccc229c5c8fb20ad252b3c0e5b634dab7 requires a valid neutral typed
input report before accepting a fresh menu press at startup, matching the
existing return fence. Held controls, NAKs, rollover and held modifiers cannot
arm it. The Nano boot keyboard requests actual100ms idle snapshots through the
existing SET_IDLE request, so an idle keyboard need not lose its first press.
This uses [HID1.11 section7.2.4](https://www.usb.org/sites/default/files/hid1_11.pdf),
with25 four-ms units and report ID0. No synthetic release or extra control
transfer is introduced. Device polling, transfer and startup bounds remain.

The change follows the stopped4489 trial's USB keyboard Select and the user's
report of possible unintended pressure on the keyboard. That does not prove a
driver fault or a physical key press. Its failure remains consumed1/max1 and
[fully retained](../2026-11-08-de25-tape-cost-board-trial/README.md). No App command,
replay or further board operation was used to bypass that failed startup gate.

Focused race tests pass for native input/full launch-return/failures and the
DWC2/input/USB-control packages. The real controller model checks periodic
neutral/held snapshots and fresh press/release without fabricated host reports.
The native input tests check startup suppression, buffered edges, genuine
subsequent presses and existing return/ownership failures. All selected parent,
generated-consumer and actual native firmware/RTL integration checks pass.
Earlier fixture/compilation failures remain in the evidence; final tests use
frozen sources. These are injected/software checks, not physical qualification.

The rebuilt SDK and entry pass. BL31 linking and binary output completed, but
the360s build bound stopped post-build validation before a producer receipt was
published. An independent partial ELF/chain-memory check passes; that cannot
replace the missing producer result or admit the firmware for hardware. No
U-Boot/composition result or complete new boot chain is claimed.

A bounded profile of the actual cold-profile derivation passes in201.882s:
102 HPS context workers consume201.196s. The builder repeats that derivation
before and after compilation. This identifies a concrete off-board build cost;
new work should remove redundant derivations while preserving fresh complete
source checks and the original bounds. Live device/owner proofs must remain
fresh. The partial build and source are retained for that repair; they are not
promoted to a bootable image.

Top TRWU8CBL remains claimed with its spare card installed; factory restore is
waived unless needed and the bottom kit is untouched. Last observed board
state was Loading after uncommanded keyboard Select, with final fabric state
unproven. No physical startup-release, tape performance, normal core return,
HDMI or ordinary DE10-equivalent core-load acceptance is established here.
