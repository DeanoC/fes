# Strong shared USB/menu composition and native menu evidence

Base: `978d063a0919d3d6de2180a0591bae3363a23c23`.
Strong-provider test implementation: `092434323`.
Native observation/parser implementation: `b3c1cc9e1`.
Stage: `out/nano-mixed-usb-20261107`.

## Strong firmware composition

The actual strong cold provider now runs with controlled/shared USB enabled,
not substitute producer callbacks. The existing private boot-binding engine,
owned SiP dispatcher, USB board/PHY policy, cold-device composition, retained
USB master, SD platform, mixed-role configuration, ROM and independent FPGA
master-stop code execute together with pinned vendor SHA. Physical registers,
configuration transport and raw bridge toggles are injected. The compiled pin
policy comes from the existing captured handoff fixture. Register vectors give
both real clock policies a common 200 MHz bus; no production checks changed.

Both profiles pass: thirteen ordinary scenarios and nineteen shared-USB
scenarios, each also rejecting an undersized union reservation at compile time.
Four five-load sequences cover changing roles in both orders and repeated
menu/core loads. Confirmed USB queries cross active SD, menu DDR and core control,
then return to closed bridges and a final owned USB stop. Six additional shared
faults inject foreign DMA/IRQ ownership, lost AHB idle, changed clocks, partial
USB reset and lost PHY readiness during an active menu. They retain the USB
reservation and reject further FPGA/SD I/O or hardware replay.

This is host execution of the strong composition. It does not enumerate a USB
device, send a HID report or establish electrical, board or HDMI acceptance.

## Native menu observations

Trusted ProgramMenuOptions.Observe receives copied evidence after a successful
frame or normal independent menu return. The native paired command emits
FES_NATIVE_MENU_STATE records, including the initial frame before the App begins.
Identity, boot digest, menu instance, source/open/return generations, frame
sequence/slot and all three source map hashes come from the actual sessions.
Return is observed before any SD grant. Trace failure retires the owner; it does
not replay a frame/closure or resume SD. The Go race suite exercises the actual
App/service cycle and observer failures alongside existing menu paths.

The strict menu-only parser consumes actual Go service records exported by its
lifecycle test. It rejects changed sources/identities, wrong ownership or
capabilities, incomplete frames/returns, stale or skipped sequences, an invalid
storage-generation gap, duplicate keys, nonfinite numbers, overlong/deep JSON
and truncated captures. A bad observation retires the parser. Returned objects
cannot mutate its retained comparisons. It explicitly reports boot/core-cycle
and physical-display verification as false: two menus alone do not prove a
computer ran. An independently admitted source contract is still required.

## Evidence boundary and next integration

All ten selected checks pass, including 1653 parent tests
(three skips) and 1595 overlapping misteross tests (79 skips). The
complete corrected native integration, producer and firmware/RTL suites pass.
All thirteen model-backend top-level tests run exactly once across the two
original 90-second phases. The actual new SDK and entry image build; their
recorded production/source bytes match and the entry contains the exact SDK
payload and linked native menu emitter. See [result.json](result.json) for
receipts and source hashes. No FPGA
conversion, board programming, SD writes, USB enumeration or physical test was
performed. The previous matched chain remains an earlier source artifact; the
new observation executable needs a newly matched entry/BL31/SPL/FIT before use.

The next board-controller change must integrate these menu records with the
existing App/core/fetch/USB and firmware evidence, support the paired display
marker and final Parked state, and derive CPU union memory only from the checked
companion. Existing serial-only controller assumptions (including generation
formulas and a five-second core-only return budget) must continue to reject
this new mode until explicitly extended. A returned menu can take a fresh
bounded configuration/presentation cycle. Do not globally widen normal ABI41
memory or accept a menu-only trace as a complete launch proof.

Then rebuild/recheck the matched chain, use fixed recovery menu upload targets
with actual readbacks, compose HPS under the shared Quartus lock, freshly claim
and reidentify TRWU8CBL, and run one bounded menu/core/menu/controller trial.
Broader core/media/audio/network/appliance and physical acceptance remain open.

## Verification budget repair

The first full integration reached its existing model-backend phase and failed:
a single 15-second context covered the initial ROM link plus all three bad-part
requests, and the combined model/endpoint package subsequently reached 90 seconds.
All six model-backend source files and all 138 package-fixture bytes matched the
previous successful run. An overlay of the previous native Go source reproduced
the same request timeout; isolated old/current runs sat around the deadline.
Those failures and the exact overlay inputs are retained.

`126a308bd` gives each independently checked model request the same 15-second
bound and separates software-model/control tests from injected endpoint tests,
with the original 90-second package limit for each. Their filters partition all
top-level model-backend tests exactly once. Runtime and hardware timeouts did not
change. The repaired focused rejection test passes. The complete corrected
integration and parent checks are recorded separately from the failed first run.
The changed test and verification tool are outside the SDK's selected production
source set; all recorded SDK input bytes remain exact, so it is reused.

The source delta and evidence are retained with archive readback and Git pack
import verification under `/home/deano/fes-retained/de25-shared-usb-menu-evidence-20261107-r1`.
