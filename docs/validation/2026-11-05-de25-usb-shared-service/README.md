# Owned shared USB PIO service and native client

Source `d69230e02` adds an explicit shared PIO capability through the canonical
USB contract, generated firmware/Go consumers, secure service and native client.
The existing exclusive constructor still requires closed-only service. The new
`NewSharedPIO` constructor requires the new capability, held FPGA data, the exact
boot owner and an unused contained USB lease. Neither constructor adopts an
already active device or enables a secure initializer from non-secure code.

The trusted shared initializer verifies the actual accepted boot/master binding,
cold composition and exact platform callbacks. USB has its own reservation and
generation; it cannot steal or clear SD's device lease. The dispatcher allows
other owned operations only after live observation confirms USB ownership.
Unconfirmed USB blocks them. Mutation while bridges are open remains busy;
observing confirmed USB remains possible during configuration. Producer stop
and bridge/SD ownership checks remain independently required.

The tests connect the real Go USB and SD clients to the actual C dispatcher,
boot/master engine and platform services with injected register hardware. A
typed gamepad report source remains valid through three SD/core control cycles,
including two control-only returns and a final DDR close. USB generation and
activity remain unchanged, and USB stop does not clear an active SD lease.
The final DDR return does not authorize SD resumption without its separate
reader/window policy. These cycles do not configure an actual FPGA package.

Failures cover incompatible service scopes, substituted callbacks, absent boot
binding, foreign mutation, unconfirmed ownership, DMA, PHY/master/reset failure,
producer stop failure and late loss of stop proof. Failed authority retains its
reservation, retires subsequent use and performs no retry. Native constructor
checks reject unknown/mixed flags and scope changes. The report source uses
injected reports; this result contains no physical HID control evidence.

The committed source passes the selected software plan, race tests, secure
firmware/rearm checks, contract generation checks and default ARM64/BL31 builds.
`result.json` records exact commands, log hashes and source identities. The first
failure-matrix test draft failed to compile due to a test helper name; the
corrected draft passes. The combined parent run reached its 900-second cap
without an assertion failure during the video fixture tests. Its partial log
is retained; every class without complete terminal evidence is rerun in a
separately bounded module group. The completion receipt verifies coverage of
all 1,533 originally selected tests, preserving video/media modes and original
skips. The pre-commit consistency check correctly rejected the
dirty package module; the same check passes after committing the complete
contract and consumers. Both early failures are retained separately.

The built image remains USB-only. It does not select the shared composition
initializer or qualify simultaneous physical USB, SD and FPGA operation. No
hardware was programmed or card written for this milestone. The next integration
must handle input polling across long core transitions, select the composed
trusted startup and wire the explicit native shared client. Normal package,
ROM/media, physical input/video and repeated play/return acceptance remain
required before claiming DE10-style core loading readiness.

Source, logs and rebuilt firmware are retained at
`/home/deano/fes-retained/de25-usb-shared-service-20261105-r1/`.
`retention.json` records archive readback and independent source restoration with
prerequisite bundles and strict Git integrity verification. External toolchains
are not exported; no working artifacts are removed.
