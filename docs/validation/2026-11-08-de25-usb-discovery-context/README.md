# USB discovery context and rejected capture

The consumed native USB boot `3c23bd7f67779909` completed hub/HID discovery,
configuration and a full 53-byte report descriptor read. The existing gamepad
policy correctly rejected the descriptor's System/Consumer Control applications.
No Ready, Stop, input report or ordinary core-load acceptance was established.
That source and raw trace are retained in
`/home/deano/fes-retained/de25-usb-control-nak-board-result-20261108-r1`.

The exact descriptor is now a negative fixture with source/hash provenance.
Discovery errors retain already captured device address/VID/PID/class/speed and
the candidate interface/endpoint/report length. Formatting reads no registers,
issues no transfers, selects no replacement device and wraps the original error.
No-candidate errors keep their errors.Is identity and report candidate=none.
Descriptor, confirmation, channel ownership, failure containment and all timing
bounds are unchanged. The physical device model is still unproven; model IDs
in tests are synthetic and do not identify the device currently on the kit.

Three focused tests cover the captured rejection, secure-source abort before
confirmation, retained copied descriptor, no subsequent host/source polling,
and preserved no-candidate error identity. The driver/parser/ownership race
suites pass. Six affected checks pass, including 1,719 parent tests with three
existing skips and generated consumer consistency.

A fresh SDK/entry/BL31/U-Boot/FIT/SPL/SOF chain is built for the same explicit
gamepad diagnostic. Source/artifact joins and embedded SPL are verified with
unchanged core bytes; exact boot identity and digests are in result.json.
Composition uses the shared Quartus lock. These changed bytes have no hardware
acceptance. The next USB test needs to identify the actual device/interface and
qualify typed input readiness, then resume combined media/core launch/return.
The user's ordinary core-load/appliance goal remains open.
