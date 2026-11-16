# Trusted USB PIO composition: cold ownership foundation

Source `87fd08789` adds a boot-local device composition contract to the existing
Nano cold ownership callbacks. A trusted initializer binds the exact contained
USB and SD platform objects from one boot, once. The cold handoff record and
non-secure wire interfaces are unchanged. No shipped boot selector enables the
composition initializer yet.

The policy takes expected reset releases from the retained platform owners,
then checks independent register samples. It never infers permission from a
released reset bit. Only the paired USB0 module/RAM bits and the SD controller
bit can appear in that expected mask. Every unrelated HPS master stays reset.
An active SD controller still forbids bridge ownership. SD platform operations
and control/DDR target observations require the USB owner to be confirmed and
its real platform Observe callback to prove PIO/IRQ/PHY readiness. Default
unbound ownership and the USB-only profile retain their original strict masks.

USB Acquire reserves before publishing its generation. USB Stop independently
checks the inactive hardware target before clearing its reservation. The new
stop-target readback grants no reusable inactive lease: cold ownership still
rejects a reset controller whose cached owner remains active. Two samples also
catch mask changes during the USB health callback, and the composition checks
that USB's generation did not change during that callback.

The injected-register test runs actual USB and SD platform code through three
SD resume/prepare/stop cycles while USB remains confirmed. It then exercises
the existing exact control/DDR target transitions and a checked final USB Stop.
Those target transitions are simulated register changes, not FPGA configuration
or bridge-closure receipts. The fixture supplies the USB preparation/PHY callback;
the existing USB board tests separately exercise the actual clock/pin/PHY code.

Failures cover DMA, global interrupt enable/mask policy, PHY mode/readiness, paired
reset mismatch, an unrelated master release, an unarmed bridge, stale cached SD
state, register access failure, generation changes during observation, unconfirmed
USB, failed release/stop, attempted SD use with control open, foreign owners and
epochs, duplicate binding and platforms from different boots. They check retained
active reservations, no unauthorized writes, no USB register access when its
reset scope is wrong, and no hardware retry after the failed authority retires.

The old all-producer stop receipt refuses an active USB controller and leaves
its output untouched; it retains its original meaning.
The existing `nano_boot` master engine still independently requires its strict
reset/producer proof. The SiP dispatcher, exclusive `device_lease` gate and USB
`CLOSED_ONLY`/`DATA_HELD` capability flags also remain unchanged. This milestone
cannot yet run a physical USB/controller and core together.

`result.json` records exact-source software, firmware-build and integration
evidence. No hardware programming, card write, FPGA rebuild or physical core
acceptance occurred. The native/secure build remains the selected USB-only
diagnostic and does not advertise composition capability.

The cumulative 900-second orchestration budget expired during the last command,
the broad FPGA Python suite, without a reported assertion failure. The earlier
parent, consistency, native lifecycle, producer and firmware/RTL commands had
completed. The final suite was restarted alone with its own 900-second budget;
the original timeout and separate phase receipt are retained explicitly.

The retained source update, logs, native/secure build and injected integration
fixtures are under `/home/deano/fes-retained/de25-usb-composition-20261104-r1/`.
`retention.json` records archive readback, source restoration with the prior
bundles, and strict Git fsck. Toolchains remain external pinned dependencies;
this is not a standalone offline SDK. No working artifacts were removed.

## Next integration

Add an explicit composed master containment/stop policy that distinguishes USB's
checked, DMA-disabled bus master from reset-held masters, without fabricating a
fabric drain. Then extend the owned services/dispatcher and generated capability
contract together, preserve SD's exclusive configuration boundary, and connect
the native Tenfoot input owner. Test the full service lifecycle and failures
before selecting a matched composed image for one bounded TRWU8CBL trial.
Ordinary packages, ROM/media admission, physical core execution/video and normal
DE10-style operation remain acceptance requirements.
