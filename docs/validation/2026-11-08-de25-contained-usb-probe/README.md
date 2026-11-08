# Contained USB inspection on the current Nano shell

Source `4f6694732defd1fee6df699a5a0878a40011135e`. The explicit `usbcheck` program now builds against the current
held-ROM FPGA shell while selecting no FPGA control/configuration, ROM, computer,
SD, menu or reader resources. Entry link/program checks, strict capability types,
source provenance and pin validation remain required. Normal core-loading
profiles keep their complete ROM/computer requirements.

A source-bound `usb_probe` cold field is derived from the checked native entry.
The header and final composer require the same contained selection. Mixed modes,
resource/digest injection, missing flags and entry/cold disagreements reject.
The original USB limits remain: 500 ms per register wait, four seconds for
enumeration and five seconds for caller startup.

The first seven-command affected plan passes on `2cf822864`; the composer-only
follow-up passes its six-command plan on the recorded source. The final parent
suite passes 1,713 tests with three existing skips. Focused selection/composition
checks pass 43 tests, native firmware/FPGA integration passes, and committed-source
`make check` passes 52 generated consumers and 37 fixture copies.

A matched USB-only SDK, entry, BL31, U-Boot/FIT/SPL and HPS-first SOF is built.
Composition held the shared Quartus lock. Post-build checks verify 2,248
source/artifact digests, the embedded SPL and unchanged FPGA core payload.
This standalone probe uses the existing DDR firmware layout. It selects no
OCRAM exclusion profile and supplies no combined shared-USB/ROM/core-load proof.
SDK source bytes match the recorded commit, while embedded Go VCS metadata is
retained separately; this is not described as a clean-HEAD build.

No board test was performed for this image. The preceding secure-tape trial
stopped at USB root connection before launcher or tape execution, and its image
attempt remains consumed. The user later reported possible power flicker; a
quiet passive UART capture did not establish the board's current state.
Next is USB-only file admission and a separately frozen bounded enumeration,
Ready/Stop trial. Current cached native admission still requires paired fpgaload
and was not broadened here. Full core-load appliance acceptance remains open.
See [result.json](result.json) for exact artifacts and evidence boundaries.
