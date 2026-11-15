# Descriptor-driven native gamepad model

Implementation `9dc71b7a1` adds a HID gamepad parser, menu adapter, typed DWC2
transport and firmware-owned source. This is host/model and cross-build evidence;
no new physical USB, input or core-load acceptance is claimed.

The preceding physical image enumerated `081f:e401`, then rejected its absent
boot-keyboard interface. This implementation retrieves the selected non-boot
HID interface's exact report descriptor and validates one Joystick/Game Pad
application before firmware confirmation. It uses no vendor-specific layout
guess. Ambiguous interfaces/devices fail before selecting an input source.
Gamepad reports never implement the keyboard-report interface.

The parser bounds descriptors, report IDs, nesting and report sizes. It handles
signed/unaligned axes, buttons, hat/null values and independent report-ID state;
invalid reports commit no partial state. The menu adapter has explicit button
mapping, an axis deadzone and press edges; a value snapshot supports a future
core-specific input route. All test descriptors are synthetic.

Keyboard and gamepad sources share one private lease state machine. Actual C
firmware tests cover prepared/confirmed ownership, copied-source closure,
replacement generations, startup/poll/stop failures and retained reservations.
DWC2 model tests cover descriptor retrieval, absence of boot-only requests,
report delivery to the menu, retained completion metadata and closure without
subsequent controller access. Existing keyboard tests still pass.

Verification under `out/nano-gamepad-parser-20261104/`:

- `gamepad-ownership-r2`: focused race tests including mapping/ownership.
- `all-host-go`: complete TamaGo host package suite, race enabled, PASS.
- `sdk-typecheck`: Nano `usbcheck` cross-build, PASS; selected runtime remains
  the existing keyboard diagnostic. This type-checks the new board API but does
  not claim a gamepad runtime image or hardware execution.
- `consistency`: `make check`, PASS; `git diff --check`, PASS.

The initial `gamepad-ownership` run failed because its stop-failure test injected
the confirmation-failure mode. The test now injects the modeled stop fault;
production behavior was not changed to satisfy that failure. Both logs remain.

Both Powerboat DE25 programming/serial adapters were visible after the reported
motherboard USB recovery. This turn opened neither kit, performed no JTAG scan,
programming, reset or SD write, and retained the existing TRWU8CBL claim.

Next: explicitly select the gamepad diagnostic in the matched build and trace
contracts, capture this controller's descriptor/reports in one bounded test,
then route physical input through Tenfoot/core ownership. Combined device use,
ordinary package/ROM/media admission, native video/network and normal DE10-like
core-load acceptance remain incomplete.
