# Nano recovery uploads and cold serial boot

`jtag_probe.checked` validates the selected scan executable's bytes and frozen
Unix mode, checks execution access, and rejects file changes during hashing.
The experimental operator applies it before reserving an operation and again
at each fresh transport observation. Copies of the qualified tool must preserve
its executable mode. This is a file check; scan/programming authority still
requires the complete owned live transport and one-use policy below.

`jtag_scan.cpp` is the source-controlled fresh programming-scan producer.
Build it against the pinned Intel AJI headers and selected vendor libraries.
It accepts one explicit private top-kit cable, requires a locked 3 MHz clock
with automatic clock adjustment disabled, performs one forced scan and only
then one non-scanning identity read. It rechecks both clock properties while
still locked. Its exact count1/4362C0DD/mask0/IR10 result is consumed by
`jtag_programming.ProgramAttempt`, whose complete transport identity now also
requires automatic adjustment0. Earlier6MHz transcripts do not satisfy it.
A failed API call, clock property or identity stops
before further reads, unlocks an acquired chain and cannot authorize programming.
The caller must still hash-bind the probe/client bytes, recheck the live
claim/server/config/clock and durably reserve each operation before scanning.
Adapter discovery uses `--listhardware`; it performs no preliminary `--enum`.
Raw pre-scan chain type is not a device identity check; the locked scan API and
fresh exact device result provide that proof. This producer performs no FPGA
programming or CPU reset itself. Scan diagnostics remain a separate format.

`jtag_clock.FixedClock` owns one host-side clock lifetime through injected
claim/read/write callbacks. It snapshots both original properties, disables
automatic adjustment with readback, selects3MHz and verifies both properties.
Live verification failure closes the active lifetime. Cleanup attempts both
original-property restorations independently, checks their readbacks and never
retries an uncertain restoration. Every operation still checks ownership.
This conservative transport candidate follows a recovery-programming failure
despite a passing6MHz scan; a separate observation found automatic adjustment
still enabled. Neither observation establishes its cause. These host controls
change no CPU/FPGA operating clock, cache policy, SD file or native admission.
Image programming and boot still need a separately bounded physical qualification.

`jtag_diagnostic.cpp` is a separate scan-only host diagnostic, built against
the pinned Intel AJI headers and selected vendor client. It accepts only an
explicit private top-kit cable/configuration, checks 6 MHz and issues one
forced scan. A successful scan can have one non-scanning identity read; a
failed scan never uses a cached identity. Bounded, hex-encoded vendor messages,
four named captured-data queries and clock/auto-adjust context help investigate
failures. Its output cannot satisfy the programming-scan grant decoder, and it
has no FPGA programming or HPS CPU-reset call. An owned operator must restore
clock, exact USB permissions and its private service independently.
The raw pre-scan chain type is observational: the installed vendor client has
reported a value absent from its public header. Like Intel's `jtagconfig`, this
separate diagnostic uses the locked scan API to establish scan availability,
then checks the fresh device identity. The programming-grant gate is unchanged.

Scan-only discovery uses `jtagconfig --listhardware`. Intel's `--enum` path can
request automatic chain scans before the selected clock is established; it is
unsuitable for a diagnostic promising just one controlled scan. Removing that
implicit request does not by itself establish the cause of a previous chain
failure. Programming still needs its own fresh verified scan and one-use grant.

The experimental `--menu-project` option on native `tamago/run.py fpgaload`
adds a separately sealed menu companion to the normal selected-computer boot.
It requires `--selected-programs` and its computer/rearm/SD-return selections;
the initial `--hps-first-project` remains the computer. Both roles must agree
on all six shared board/HPS/DDR fingerprints and keep their own complete master
inventories and resource maps. The SDK links the full companion and constructs
`NewProgramLoaderWithMenu`; the entry and cold builders recheck its sealed
source. The computer's reader descriptor stays unchanged, while CPU mapping,
entry staging, secure firmware and SPL reserve the full 8 MiB menu area. Only
this explicit pairing enables the role-aware secure firmware. Existing menu-only
and computer-only selections keep their original checks.

With `--tenfoot-serial`, the native frontend now selects the service's menu
lifecycle and presents the actual Tenfoot software-rendered frames. The SDK
exports `MENU.FCP`, `MENU.JSN` and `MENUSEL.JSN` for the card's `FES/` directory,
with their byte hashes and sizes in its receipt. Startup reads the single menu
selection alongside `CORESEL.JSN` and `UI.JSON` before the service owns SD.
Every return restores a freshly loaded menu and cached frame before Ready;
application shutdown parks it independently with SD contained.

The actual App/backend/service cycle passes host tests with injected hardware,
including pixel comparison and terminal failure paths. A new matched boot chain,
active-menu/shared-USB qualification and bounded hardware acceptance remain
required. The actual strong cold provider now passes shared confirmed USB/menu
composition tests with injected registers, including USB-specific failures;
electrical behavior, enumeration and controller input still need hardware.
Ethernet recovery remains a separate loader profile.

The paired frontend emits `FES_NATIVE_MENU_STATE` after successful frames and
independent menu returns, before resuming SD. `menu_service_trace.MenuTrace`
validates their identities, hashes, ownership, generations and frame/closure
sequence against independently admitted source fields. It is a menu-only
decoder: the existing serial-only board operator still needs explicit paired
CPU mapping and App/core/USB/boot integration before it can run this new mode.

The recovery uploader accepts `--purpose menu-package`, `menu-metadata` and
`menu-selection` for those three fixed files. It keeps the existing 8 MiB,
16 KiB and 8 KiB bounds respectively, with RAM hash, SD write/readback and
unchanged boot-file checks. Upload the archive and metadata before the selection;
uploading files does not activate the native image or grant runtime admission.

`--purpose install-manifest` supplies only `FES/INSTALL.JSN`, bounded to 8 KiB,
through the same owned upload and fresh readback checks. Upload its referenced
package, metadata and ROM before the manifest. It does not install a core or
enable writable native storage; the explicitly selected native installer
independently validates the source and waits for the normal Tenfoot Install
action. Arbitrary paths and protected boot files remain outside this purpose.

Explicit writable Tenfoot images also emit `FES_NATIVE_INSTALLER_VIEW` when
their Settings/Systems controls change. This bounded read-only record contains
row kinds, focus, busy/status and cached native ownership, separately from App
and firmware receipts. `installer_trace.py` checks its complete schema and
closed-panel state. Its App decoder takes the caller's explicit install phase
and completed-install generation origin; it preserves every native counter.
Ordinary launch/return framing keeps its previous defaults. These observations
do not establish an installation or replace fresh media, menu/USB drain,
configuration or reboot/discovery proofs.

The source-bound two-slot menu has an explicit native selection:
`tamago/run.py fpgaload --board de25-nano --menu-present`, together with the
sealed menu `--hps-first-project`, source-controlled SD profile and selected
TamaGo compiler. The SDK, entry and cold-profile builders require its complete
menu access map, 8 MiB reservation and menu ABI; they do not treat a 4 MiB
checksum window as a menu. The command presents three changing Tenfoot frames
through `MenuSession` and performs independent secure master-stop/bridge close.
The HPS-first composer checks the same access digest. It does not write a card
or program a board. This cold test does not resume SD or supply the full native
menu/core/menu lifecycle, and has no physical display acceptance yet.
An OCRAM-selected menu uses the same final SPL allocation snapshot and exclusion
guard as the shared-ROM chain, after the final trace and before entering BL31.
Selecting the reserved layout never omits that one-shot guard.

`native_trace.decode_native_configuration(..., menu_read=...)` observes that
specific cold menu using the checked identity, configuration, CPU mapping,
two-slot memory and menu access descriptors. It requires the complete boot and
configuration prefix, zeroed 8 MiB publication, both secure use-point gates,
three ordered frame acknowledgements in slots 0/1/0, and the independent
master-stop and owned closure. Partial, replayed, failed or foreign observations
cannot finish it. The decoder performs no device operations. Its result remains
serial evidence, with physical HDMI, memory release, runtime admission and the
complete menu/core/menu lifecycle unqualified.

Native selected-program platform version two compares the complete boot
configuration through byte `0x657`, using `hps_configuration_sha256`. The pinned
TF-A handoff struct occupies `0x648` bytes; the pinned SPL additionally reads
five words from SDRAM data offset `0x644`, ending at `0x658`. The digest covers
their complete union, with checked section headers and unchanged HPS/EMIF
parameter and master inventories. Quartus's following text bytes can differ
between core builds. Full raw-file SHA-256 checks in the compiler receipt and
HPS I/O map remain mandatory. Version-one platform records reject, so this
change requires a freshly built native SDK and matching selected metadata.

The return-trace UART is UART1 (`0x10c02100`, status `0x10c02114`). The build and
trace package checker independently resolve `/chosen/stdout-path` through its
alias in both compiled U-Boot and FIT device trees, requiring that enabled port,
four-byte access and register shift two. `serial0` is an alias to UART1 on this
board. The initial v1 diagnostic incorrectly targeted UART0; its physical trace
is invalid and required an explicitly bound repair. The UART1 repair passed;
the later complete-launch test then stopped between receive-drain observations.
The corrected 100-ms profile below still needs a new bound repair and hardware
test against the currently installed recovery image.
The UART update/readbacks passed, but no network/core-load result is admitted
from that diagnostic. The old attempt must not be replayed.

The explicit `build_terasic_uboot.py --network-recovery --network-return-trace`
selection corrects `XGMAC_TIMEOUT_100MS` from 100000 to 100 for its four
millisecond-based API callers and adds 34 ordered UART observations around
successful TFTP return. This functional correction is explicit in the v2
profile identity; source inversion restores both the original constant and
observation-free inputs. Register operations and the 90-second host transfer
deadline remain unchanged. Earlier trace packages are historical bootstrap/
repair inputs and fail current profile admission. Hardware qualification of
this correction remains open.

The observations cover
the transfer-count print, environment updates, Ethernet/XGMAC/PHY stop, net-loop
return and command completion. The source transform reconstructs the pinned
network inputs and existing PHY preparation exactly. It retains the vendor
operations and intended 100-ms drain budget; a returned drain loop does not
prove idle DMA.
The UART helper uses at most 100,000 polls per wait and 96 bytes per string,
then parks on uncertainty. Host execution compares actual vendor return/stop
blocks before and after instrumentation and checks stalled UART/partial traces.
The flag is off by default and requires the separate recovery profile. Its
artifact has a distinct receipt/FIT identity and remains ineligible for the
default recovery package validator. `recovery_return_package.py` separately
checks the traced build, delegates base checks on reconstructed private sources,
and prepares regular-file card/manifest images using only the exact qualified
SOF/SPL/BL31. Its distinct `prepared-return-trace` status requires its explicit
loader; the ordinary uploader rejects it. Packaging invokes no FPGA or device
operation and verifies that only the fixed FIT mirror changes in the prefix.

`recovery_trace_upgrade.py` supplies a one-shot UART controller for the explicit
qualified PHY-prepare-to-trace transition. It verifies the known spare, existing
package/experimental FIT and memory ownership, saves fresh rollback files, then
writes/readbacks the FAT FIT, raw mirror and manifest. It never activates its
result. Claim, journal, UART or write failures retire every wire entry point;
caller-owned inputs are snapshotted. `recovery_trace_console.TraceConsole`
additionally requires the full ordered TFTP trace after the base console's fresh
nonce/prompt checks, before a download can succeed. Trace loss prevents the
upload session's SD write, and a retired console sends no cleanup commands.
A complete frozen physical operator and new bounded hardware test remain
necessary. No marker grants SD/native programming permission or widens deadlines.

The separate `recovery_trace_upgrade.Repair` transition admits only the exact
consumed UART0 diagnostic FIT, manifest and prefix. It requires the original
R1107A rollback pair and reads/hashes it before and after its five fixed writes.
Fresh R1107B files preserve the consumed image. The ordinary `Upgrade` keeps its
original baseline restriction; neither transition activates its result. The
repair shares the existing ownership, memory, UART, write/readback and permanent
retirement checks. A new frozen operator and fresh bounded boot/card proofs are
required after the reported power loss. Model success grants no physical write
or core-load acceptance.

`recovery_trace_upgrade.TimerRepair` separately admits the exact installed
UART1 FIT, manifest and prefix for the 100-ms correction. It authenticates
both the original R1107A pair and consumed UART0 R1107B pair, reads/hashes both
before and after the five fixed writes, and preserves the current UART1 pair
under fresh R1107C names. Existing R1107C destinations reject before any write.
It shares the one-shot controller and never activates its result. Neither
earlier transition accepts this installed UART1 source. A fresh frozen
operator and live card/boot checks are still required after the power report;
the later serial version/prompt observation does not prove card integrity.

The current guarded-RAM contract keeps the per-CPU array in sealed initialized
BL31 DDR instead of OCRAM BSS. Its original cache flush remains in place. The
matched device tree reserves the complete two-MiB BL31 DDR interval, and the
loader rejects missing, changed, overlapping or duplicate firmware reservations
before CPU queries. Both the initialized-array ELF checks and the live LMB
reservation check are required; a reservation is not a cache-operation proof.

Current guarded-RAM builds also emit `FES_NANO_CALL` receipts at the selected
SMC instruction, in the PSCI affinity handler and around its existing per-CPU
cache flush. The decoder pairs six receipts for each of four initial and four
late queries; partial or misplaced calls cannot complete native handoff.
The U-Boot wrapper traces only the selected 64-bit affinity call with level
zero and one of the four compiled affinities. Its temporary callee register
is declared to the compiler, and result/argument registers remain unchanged.
The PSCI function keeps its original validation and cache operation. Diagnostic
leaves use bounded direct UART writes, preserve callee registers, allocate no
private state and call no scheduler or firmware service. A before receipt
marks uncertainty; a handler receipt does not establish an exact fault PC.

The initial `fesboot` path emits ordered `FES_NANO_LOADER` receipts around
memory validation, the CPU snapshot and each PSCI affinity call, register-read
groups, CPU/watchdog checks and the secure ownership observation. Markers use
direct UART writes with 100,000 polls per wait, a 128-byte string limit and a
final transmit drain. They call no scheduler, timer, watchdog or firmware
service; a failed wait parks. The late snapshot retains its original untraced
behavior. Calls, arguments and rejection policies remain unchanged.
`loader_progress.py` retains bounded partial progress and permanently rejects
damaged order or scope. Current guarded-RAM decoding requires this progression
before late handoff; a before marker records uncertainty rather than proving
a firmware call executed. Progress alone grants no boot or package admission.

`hps_context.py SEALED_PROJECT rom` derives the held-ROM resource contract for
the explicit ROM producer variant. It verifies the source seal, generated
conduit/clock/reset, complete master inventory and nonoverlapping lightweight
CPU address region, and binds the result to the same package, build, control
and computer maps. Older producer profiles return no ROM resource. Native entry
and cold firmware reject a ROM profile with a missing or altered resource;
the cold header can select the secure ROM provider with the derived aperture.
The native selected-program builder connects the retained ROM receipt and
compiled resource to its explicit ROM verifier. It requires a separate firmware
image and host linker compiler. A fresh FPGA producer and matched boot-chain
test remain necessary for physical ROM acceptance.
The HPS-first file composer independently compares that ROM resource and its
digest with the sealed project before conversion and records `held_rom` in its
output. Missing, changed or out-of-profile ROM resources cannot publish a
composition receipt.

`upload.py --purpose firmware` writes only `FES/FIRMWARE.BIN`; `firmware-alt`
writes only `FES/FIRMALT.BIN`. Both accept exactly 4 or 8 KiB. They use the
existing exclusive recovery upload, protected boot-file
checks and fresh SD readback. Other paths and sizes reject before device I/O.
The upload supplies bytes only; the native store and loader separately check
their source digest, retained activation receipt and compiled ROM resources.

`usb_trace.py` observes the separately selected `usbcheck` image. It requires
the matched SPL/BL31 handoff, a successful secure USB setup, the complete HPS
context/pin observations, entry/BSP CPU startup through `go_vectors`, and
ordered USB Ready/report/Stop/Pass records. Stop must carry the same owner and
generation 3. Missing, duplicate, malformed, failed or fragmented observations
cannot complete the diagnostic. The caller supplies checked build identities.
The terminal observation is the program's USB Pass after its checked Stop.
The pinned BSP installs TamaGo's silent ARM64 exit hook; the runtime's generic
halt message is not an observable checkpoint on this board. CPU halt is not
claimed, and foreign runtime output cannot replace the USB receipt.
The decoder performs no device operations or admission. An idle success proves
reported startup and stop only. Valid key presses are separate input evidence,
and neither result qualifies combined USB/storage/core operation.

`native_admission.py` keeps its default paired `fpgaload` file-reuse scope.
The explicit `USB_SCOPE` selects a separate USB-only record, its checked
`probe-profile.json` and one complete sealed project. Entry and cold firmware
must agree on contained USB with the same context/pins and no ROM, computer,
program, menu or SD selection. Both scopes run full validation before recording
and fresh source/artifact/tree-membership and claim checks when loading. Their
tags cannot be exchanged. A cached record grants no live board authority.

The configuration code uses an owned per-load descriptor in the last page of
the existing DMA reservation. Firmware captures and checks that binding instead
of using a compiled payload digest table. Its matched
[two-load hardware diagnostic](../../../docs/validation/2026-11-03-de25-program-binding/README.md)
passes on the fixed sealed package. The opt-in `--selected-programs` native
frontend reads package choices and retained metadata from SD against a
compiled board contract. It now serves persistent serial launch/return requests
through `ProgramService`, preserving the same contained loader and control
operations. Its
[matched service hardware test](../../../docs/validation/2026-11-03-de25-service-hardware/README.md)
passes three command-driven sessions and final Park on the same package. The
[distinct-package board test](../../../docs/validation/2026-11-03-de25-distinct-hardware/README.md)
now passes ZX81 → ZX80 → ZX81 after one owned six-file Ethernet upload and six
fresh readbacks, including native SD retention between sessions. Its
[matched SD-selected board test](../../../docs/validation/2026-11-03-de25-selected-upload/README.md)
passes on its recorded two-cycle frontend: one owned Ethernet session writes all four files and freshly reads
them back before two native same-package load/Run/return cycles with fresh SD
retention. Ordinary FES admission remains open. Earlier physical results below apply to
their named source and artifacts.

The [serial drain/recovery/warm-SD hardware sequence](../../../docs/validation/2026-11-03-de25-uart-drain-warm-sd/README.md)
passes on TRWU8CBL. The spare now contains the matched `phy-prepare` recovery
FIT; its serial upgrade, protected-file readbacks and repeated Ethernet uploads
pass. Native code loads/runs/returns the fixed sealed core twice, reopening SD
and freshly retaining its package between the loads. Normal Nano runtime
admission, arbitrary package selection and physical Tenfoot remain open. The
updated spare requires its matched `phy-prepare` recovery SOF; retain the old
FIT/manifest rollback copies and the safe factory card.

The [native preflight/warm-return hardware sequence](../../../docs/validation/2026-11-03-de25-stored-preflight/README.md)
passes on TRWU8CBL with the new matched native image. The loader preflights each retained selection before stopping SD.
Wrong package/map/digest choices leave storage usable. A single-use load plan
freezes the checked inputs and requires the exact contained selection plus fresh
secure ownership before a DMA reservation or START. Copies and concurrent uses
cannot replay the plan. Both native load/Run/return cycles and fresh SD retention
between them pass. It does not enable an
ordinary Nano programming profile or arbitrary package admission.

`uart_transfer.send` supplies a bounded Kermit RAM transfer to an already owned
exclusive recovery console. Its caller must first establish the complete live
LMB ledger and reserve the window with a verified FAT load. The sender may exit
successfully while its stdout pipe or UART transmit queue still holds bytes;
the relay drains both within the original deadline before accepting the loader
size, return-rate and current prompt acknowledgement. The caller still verifies
the RAM hash and current ownership before using the result. A sender failure,
claim loss or exceeded bound stops the attempt and retains status/packet logs;
the caller handles target recovery. The fixed-pattern hardware capture under
`tests/fixtures/nano-kermit-exit/` reproduces the successful-exit/queued-CRLF race.
This transport issues no SD write or activation operation. Physical transfer
qualification remains separate from SD update, native boot and core admission.

The recovery version check requires the complete pinned profile banner,
including the build date and Terasic's `socfpga_agilex5` board suffix. The
selected configuration must contain that exact `CONFIG_IDENT_STRING`.
The captured old-loader response tests the vendor format while remaining
ineligible for the new PHY-preparation upload path. Missing, duplicate,
extended or foreign banners reject before any card command.

The current recovery profile provides `fesphy`. After selecting the stable MAC
and probing Ethernet, the uploader invokes this command before checking PHY
identity and two consecutive link/negotiation-ready samples. It prepares the
pinned KSZ9031 once; subsequent calls only check that preparation. DHCP's first
XGMAC start requires the prepared object and performs no PHY reset/configuration.
Unknown preparation retires that object. The link gate remains 20 seconds and
the vendor's internal negotiation timeout remains 4,000 ms. Earlier recovery
images lack this command and cannot qualify this path. The source transformation,
paired vendor/changed C test and recovery cross-build pass; its new board test
is pending.

`upload.UploadSession` owns one live recovery console and supports repeated
fixed boot/package/metadata/selection uploads before native boot. Keep the same
session and exclusive console for all files, then use `session.verify` to
freshly reread every file after the final write. Each upload still checks the card, prefix, protected recovery
FIT/manifest, Ethernet identity/link, complete download, SD readback and byte
comparison. The command-line uploader uses a single session for its one file.

The session starts only with an empty scratch window and no existing volatile
`fes_upload_session` marker. It sets and reads back its own marker without
saving the environment. Subsequent operations require that marker and the exact
live DRAM, relocation and complete LMB inventory. Only successful bounded
FAT/TFTP loads extend its allocation ledger; observing a `none` reservation
cannot confer ownership. The complete map is checked again before an SD write
and before reporting success. Smaller later loads preserve the prior maximum;
adjacent owned ranges coalesce as in the pinned loader. Read-only verification
is limited to the current identity of a file already verified by this session
and updates the same ledger.

Any command/proof failure or explicit close retires the session. A lost marker,
target reset, changed reservation or new session cannot reuse the old scope.
Concurrent operations and reused receipts reject before I/O. The
[stopped hardware sequences](../../../docs/validation/2026-11-02-de25-native-fetch-trial/README.md)
provide the real before/after LMB fixture: the earlier stateless uploader
rejected the second transfer after a successful first download. The
[owned upload and native fetch trial](../../../docs/validation/2026-11-02-de25-native-fetch-hardware/README.md)
now passes both uploads, the final reread and real native Z80 fetch activity
across Run/Hold/relaunch, followed by complete master stop and owned reclosure.
This remains a bounded native diagnostic; ordinary runtime core admission,
Tenfoot launch/return, USB input and video acceptance stay open. The earlier
separate PHY startup timeout's cause remains unexplained.

Current matched cold builds also emit `FES_NANO_SPL ATF` immediately before
branching into BL31. SPL checks the fixed loaded BL31 bytes against its compiled
SHA-256, the target/argument addresses, completed cold containment, EL3, MMU/data
cache state, masked exceptions and unchanged primary CPU. The hash uses the raw
vendor SHA API without watchdog or device callbacks. It changes no cache or
reset controls. Invalid arguments, state or bytes reject before the branch.

The current `loaded-bl31-handoff-v2` descriptor also requires paired
`FES_NANO_SPL BARRIER` observations around the final `DSB SY`, followed by
`FES_NANO_SPL ATF_STEP` at guard entry and before/after raw hashing. These
checkpoints narrow partial progress. Hash completion does not mean the digest
matched; the final ATF observation and subsequent secure/loader ownership checks
are still required. The barrier remains in place. The
[checkpoint trial](../../../docs/validation/2026-11-02-de25-spl-barrier/README.md)
verifies both SD files but stops after handoff without even the pre-barrier
marker. The actual vendor UART calls the general scheduler after transmitting
a newline; a bounded post-containment console path is the next candidate.
The physical fault location and this recipe remain unqualified.

The contained console routes post-containment handoff, barrier and ATF
messages through `spl_console.inc`. It uses only the already configured UART1
TX/status registers, with bounded polling, bounded formatting and transmitter
drain. Timeout, invalid status or an oversized message parks without retry or
callback fallback. The vendor console remains in the earlier startup path.
The final barrier and all CPU/argument/digest checks remain in place. Its
[matched TRWU8CBL trial](../../../docs/validation/2026-11-02-de25-contained-console/README.md)
passes native startup, SD package retention, sealed FPGA configuration and the
fixed 32-byte DDR read/drain/reclose diagnostic. All 1,273 selected parent tests
and the actual linked callback-free sink checks pass. Runtime admission,
arbitrary DDR access and real emulation-core launch/return remain open.

The U-Boot receipt's checked `atf_handoff` descriptor supplies `atf=...` to the
serial/configuration decoder. Current file composition requires that descriptor
and `spl_atf_trace=true`; the decoder requires its ordered observation and matches
the later capture pointer/cookie to the final SPL arguments. Historical captures
keep their explicit base observation scope. A completed ATF observation proves
neither execution of BL31 nor runtime admission. Its
[bounded TRWU8CBL trial](../../../docs/validation/2026-11-02-de25-atf-handoff/README.md)
verifies the firmware/package uploads but stops after SPL handoff before the
final ATF observation. The unresolved interval includes the final barrier,
return and loaded-memory guard; BL31 entry is unobserved. No DDR read/drain
or runtime admission is qualified.

`smmu-profile.json` selects a separate serial-only `probe` image. Its source-bound
SPL/BL31 chain enables a read-only SMMUv3 observation after trusted cold ownership
is accepted and before BL33. It records two matching samples of translation,
bypass, abort and secure controls, checking ownership around every read. It
issues no SMMU, bridge, reset or translation-table write and enables no native
device. `native_trace.decode_native_boot(..., smmu_observe=True)` requires this
extra ordered record and checks its interpretation against the raw words.
A complete trace is a serial diagnostic, never a DDR access qualification.

Build with `tamago/run.py probe --board de25-nano --profile
platform/embedded/nano/smmu-profile.json` and the usual explicit compiler/output.
Other programs reject that profile. The ordinary serial and SD profiles keep
boot observation disabled. The cold DDR configuration profile observes the
same controls again after its SDM readiness query, before opening control.

The cold DDR reader candidate now binds its sealed package to the intended
`0x95000000..0x95002000` test window and a separate 2 MiB CPU reservation.
The native builder, entry image and source-bound SPL/BL31 chain retain the same
descriptor and digest. U-Boot rejects live allocations that overlap either
the reader arena or the configuration arena, then reserves both before entry.
Native Go maps the reader arena as normal uncached memory and pins its fixed
test pattern until reboot. This supplies no FPGA translation or access grant:
the MPFE TBU/firewall state and complete master-stop callbacks still need
qualification before releasing the DDR data bridge. The ordinary runtime
rejects this diagnostic profile, and its on-chip reader operation rejects DDR.

The [contained DDR prerequisite observation](../../../docs/validation/2026-11-02-de25-ddr-prerequisites/README.md)
passes on TRWU8CBL with the matched sealed DDR image and native boot chain.
It verifies the live identity, stable SMMU/firewall readings, coverage of the
reserved buffer and the idle reader/probe stop callbacks before reclosure.
All data bridges stay reset in that prerequisite trial. The subsequent
[fixed read/drain trial](../../../docs/validation/2026-11-02-de25-contained-console/README.md)
qualifies its guarded 32-byte operation; broader runtime access remains open.

The bounded DDR diagnostic is selected explicitly with `fpgaload --ddr-read`
in `platform/embedded/tamago/run.py`. It requires that same sealed DDR project
and the contained SD profile. The builder compares the complete generated
F2SDRAM adapter with its sealed vendor template, binds its read attributes and
fixed window into the native image and cold chain, and enables the secure
diagnostic only for that selection. The provider rechecks SMMU/firewall state
and all 2048 pattern words immediately before opening DDR alone. It requests
one read of 32 bytes, disables the reader with that request outstanding, checks
returned data and the matching acknowledgement, stops every master and closes
control plus DDR through the existing hardware drain. Owner/generation checks
cover each transition; errors retire the attempt and keep memory pinned until
reboot. The trace decoder checks the paired accesses, both fresh gates and the
actual closure receipt. It has software/RTL coverage, and its fixed active
DDR hardware test now
[passes on TRWU8CBL](../../../docs/validation/2026-11-02-de25-contained-console/README.md).
Normal runtime admission stays unavailable.

The separate `fpgaload --window-memory --window-read` path binds a complete
4 MiB reservation and one rendered tenfoot frame to the window personality.
Firmware reports its control/data scopes, both fresh full-window permission
checks, all-master stop and actual hardware closure receipt. The independent
`window_trace.py` audit requires these records plus the matching native frame
checksum, retained-memory identity and closed owner/generation. Partial,
replayed or contradictory traces remain incomplete. This is a bounded
read/return diagnostic; native HDMI, buffer reuse and ordinary core admission
still need their own qualification.

The first full-frame hardware candidate, `ec894b8fe`, verifies both uploads,
fresh SD readbacks and both full-window permission checks on TRWU8CBL, then
times out in the native reader and secure master stop. It has no successful
frame or closure receipt. Failed operations now report their cached native
stage, START attempt and latched progress, plus the firmware's stable reader
status/pending samples. Reporting rereads no reader registers and replays no
controls. All outcomes keep the reservation pinned until reboot.

The [full-width window trial](../../../docs/validation/2026-11-04-de25-fullwidth-window/README.md)
passes on TRWU8CBL with FPGA producer `df234b524` and native/firmware source
`74ef8736a`. The read boundary now uses aligned 256-bit transfers and splits
each actual response into the endpoint's two 128-bit beats, retaining both
halves through drain. The actual generated-interconnect model passes a complete
frame and four held/abort cases with 32-byte HPS transfers enforced.

The physical trial verifies both Ethernet uploads and fresh SD readbacks,
native package retention, both full-window permission gates and all 3,686,400
frame bytes. CRC `0xf7429b0e` matches the native CPU publication. Reader/master
stop completes with two polls, followed by generation-7 bridge closure at
reset `0xf` and hardware drain mask `0x8`. Independent replay of the complete
saved capture agrees and rejects six damaged captures. The prior narrow-width
failure remains retained. This qualifies one frame in this matched diagnostic;
memory remains pinned and HDMI, reuse and ordinary admission remain open.

Native entry now stores its complete ELF as one bounded gzip member, with
strict decoded-size/CRC/stream checks and an explicit temporary CPU workspace
inside owned Go RAM. Destination segments exclude that workspace before any
load copy. The full Tenfoot command handler is selected again and the frame
fixture uses its real search/text operations. This changes the native loader;
previous hardware passes do not qualify the new compressed image. The FIT
limit stays 8 MiB; ordinary UI/core admission and HDMI remain open.

`fpgaload --window-memory --window-read --window-present` selects twelve
changing real tenfoot frames through the owned `WindowSession` presenter.
Each publication is read completely and locally drained before that arena is
updated; reader generations advance from 2 through 13 and drain cookies
advance with them. The final secure master stop checks the already-drained
reader and reports its cached generation/cookie acknowledgement without an
extra device read. The trace audit requires every changing frame, its matching
publication/read CRC, local drain and final firmware closure. The selection is
bound into native ELF, entry, cold profile and composition receipts. This is
a separate bounded diagnostic. The [matched repeated-frame test](../../../docs/validation/2026-11-04-de25-buffered-window/README.md)
now passes all twelve frames, local drains and final firmware master stop and
bridge closure on TRWU8CBL. Full-arena hashing reads through private 32 KiB CPU
scratch storage with unchanged coverage and bounds. HDMI and ordinary core
loads remain open.

The [first repeated-frame trial](../../../docs/validation/2026-11-04-de25-repeated-window/README.md)
stops at the host's phase bound after one complete frame/local drain; final
bridge closure remains unknown. Current native builds add ordered, monotonic
`STEP` observations around rendering, presentation and return. The audit
retains partial frame/progress records, requires all fifty progress points for
the complete twelve-frame path, and still requires the independent frame,
master-stop and bridge-closure receipts. Historical captures use their
source-bound decoder; missing progress is never accepted as the new format.

The [bounded read candidate](../../../docs/validation/2026-11-02-de25-ddr-read-candidate/README.md)
passes the complete software/RTL checks and matched cross-build. Its first
TRWU8CBL attempt verifies both network uploads, then stops after the SPL handoff
before secure startup or native execution. No DDR read/drain was reached;
startup and subsequent JTAG discovery need investigation.

Current cold builds record `secure_capture_trace=true` and emit a paired
`FES_NANO_EL3_CAPTURE` observation around the secure record copy after console
registration. The decoder requires the pair and matching arguments before the
provider/loader sequence. It never infers success from these markers alone.
Archived traces made before this instrumentation use the explicit historical
decoder format; missing records cannot select that format automatically.

The [early-capture trace trial](../../../docs/validation/2026-11-02-de25-el3-capture/README.md)
passes its software checks, matched build and both network uploads, but still
stops after SPL handoff with neither new marker. The private top-only JTAG
service programs successfully and restores its original clock. The earlier
EL3 entry/console interval remains unresolved; no DDR read/drain or runtime
admission is qualified.

The [native Go hardware diagnostic](../../../docs/validation/2026-11-01-de25-native-go-boot/README.md)
now passes the actual secure provider, both loader ownership snapshots and
EL2/EL1 entry on TRWU8CBL, ending in the TamaGo Go probe. Timer, goroutine,
allocation, GC and volatile memory-filesystem checks passed. The
[cookie and cleanup repairs](../../../docs/validation/2026-11-01-de25-cold-cookie-cleanup/README.md)
preserve ownership through that transition. This serial diagnostic does not
establish native SD/FAT32, USB/network/display, FPGA programming or physical
core launch/return.

The separate [controlled native SD diagnostic](../../../docs/validation/2026-11-01-de25-sd-platform/README.md)
now passes on TRWU8CBL with candidate `db7c01de309270e7`: native FAT32 reads,
secure containment, resume, a matching reread and final containment. It uses
the real CPU/SMC and PIO paths, preserves the single-CPU cold scope and makes
no native SD writes. Recovery upload/readback remains a separate stage.
The original runner's CID-width rejection is retained; the corrected observer
validates the complete saved trace without another hardware execution.
This result does not qualify other peripherals or physical core loading.

The [KSZ9031 recovery and native package diagnostic](../../../docs/validation/2026-11-01-de25-ksz9031-packages/README.md)
now also passes on TRWU8CBL. Two fresh recovery boots uploaded and independently
read back the native FIT and synthetic package, preserving protected boot files.
The matched Linux-free probe retained the expected package, contained SD,
consumed retained RAM bytes, resumed/reread, and contained again with one owner
and generations 3/4/6/7. No native SD write or FPGA payload activation occurred.

The [closed-bridge SDM status diagnostic](../../../docs/validation/2026-11-01-de25-fpga-ready/README.md)
now passes on TRWU8CBL: one fixed read-only firmware query and unchanged
closed ownership at generation 1. The observed readiness is `0`.
No FPGA endpoint access or bridge opening was requested. This observation
is separate from live build identity and runtime configuration acceptance.

This prototype joins a repeatable recovery upload tool to a matched Linux-free
SPL, BL31 and native serial probe. The [current validation](../../../docs/validation/2026-10-31-de25-cold-upload/README.md)
records the first host tests, cross-builds and regular-file packaging. The
[subsequent TRWU8CBL trial](../../../docs/validation/2026-10-31-de25-upload-cold-trial/README.md)
passed the production upload core/transport with a 4.7 MB FIT and independent
SD readback. Native SPL stopped at its initial ownership check before FIT/DDR
loading or EL3/native entry. The POR mask is corrected from the official CS/R1
register map; the observed CPU reset value still rejects. The follow-up adds
MPIDR and fresh CPU/reset diagnostics and remains hardware unqualified. The earlier manual
[Ethernet and SD scratch trial](../../../docs/validation/2026-10-31-de25-ethernet-sd/README.md)
passed with the separate recovery loader; that result does not qualify these
new artifacts.

The subsequent [CPU-only TRWU8CBL observation](../../../docs/validation/2026-11-01-de25-cpu-state/README.md)
programmed successfully but captured no UART bytes within 40 seconds. It
provided no new CPU/reset evidence. Factory boot was restored by reference-SOF
JTAG and authenticated without a card swap or physical power cycle.
The [next trace candidate](../../../docs/validation/2026-11-01-de25-cpu-trace-preparation/README.md)
preserves the actual vendor startup and adds earlier checkpoints. Its
[bounded TRWU8CBL trial](../../../docs/validation/2026-11-01-de25-cpu-trace-trial/README.md)
programmed successfully but again captured no UART bytes. JTAG still responds;
the user permits retaining this exclusive kit's diagnostic without factory
restoration. A subsequent [debug readback and console control](../../../docs/validation/2026-11-01-de25-console-control/README.md)
found that the SOF disables HPS DAP access; no OCRAM dump was obtained. Removing
the pre-console cold sample also left UART silent. The [seed-configuration
control](../../../docs/validation/2026-11-01-de25-seed-prefix-control/README.md)
then reached console, six register observations and unconditional park, reporting
EL3 and `MPIDR=0x81000000`. It retains the seed drivers and defers tracing until
after console setup. Those later observations grant no initial ownership proof.
The continuing claim is held under the user's optional factory-restore policy.
The [subsequent full initial trace](../../../docs/validation/2026-11-01-de25-initial-trace/README.md)
also passes with the preserved seed configuration: all 18 first-C-to-park
checkpoints, EL3/MPIDR and first/fresh reset samples were captured. Initial
admission in that diagnostic rejected `CPUINRESET=0xf`; its gate was unchanged. The
[factory inventory comparison](../../../docs/validation/2026-11-01-de25-cpu-inventory-reference/README.md)
then showed that this register is not a complete live CPU-state check. CPU1
hotplug restoration failed and testing stopped; exact reference-SOF recovery
verified a fresh original factory boot with all four CPUs online. The native
version-2 ownership prototype below adds initial parking and release prevention;
physical qualification remains required.
The authoritative kit log records the current image and continuing claim.
The [EL3 power inventory](../../../docs/validation/2026-11-01-de25-el3-power-inventory/README.md)
now captures all 32 records: first/pre-park values agree, with spare CPU status
words zero and CPU-channel request/status masks selecting only core0. The
diagnostic is parked with the original card inserted. These observations do
not grant native ownership. The subsequent
[version-2 initial-parking trace](../../../docs/validation/2026-11-01-de25-initial-parking-v2/README.md)
passes its first check and all 32 records, then parks before DDR/storage/BL31.
The matched native handoff and firmware release restriction still need their
own physical test.

`native_trace.py` checks the serial observations for that next probe. Its
`decode_native_boot(capture, build_id, owner_seed)` takes the build ID and
cold-chain owner seed from the checked matched U-Boot receipt. `loader_ready`
requires the ordered SPL, sealed FIT, secure-provider, loader and RAM reports
through the manual prompt. `complete` additionally requires the entry command,
both ownership snapshots, live CPU state and ordered EL2/EL1/Go checkpoints.
The SPL owner must derive from the expected seed and reported epoch and must
match both later snapshots. The public snapshot tag, flags and generation are
exact. Raw bridge reset words use the same four-bit mask as the firmware:
`0x4f` and `0x0f` both retain the required bits. Mixed builds, reordered or
duplicate stages, failure markers and partial final lines cannot complete.
Partial UART reads retain progress. A completed decode records the serial
probe only; kit/card authorization, artifact verification and later device/core
qualification remain with the caller.

Passing `storage={'bytes': N, 'sha256': HASH}` selects the controlled SD
observer, using the independently checked recovery manifest. It additionally
requires secure SD setup, a 15-byte SDHCI R2 CID, same-owner generations 3/4/6/7,
both expected reads and final containment. The board-trial caller separately
checks the known spare-card capacity. Serial-only observations cannot grant SD.

Passing `package={'package_id': ID, 'payload_sha256': HASH, 'payload_bytes': N,
'build_id': BUILD}` selects the read-only package-retention observer instead.
Those identities must come from the independently checked archive, not its
serial output. It requires the same controlled SD setup and generations 3/4/6/7,
retention, containment, resume/reread and final containment. It never reports
core-load acceptance. The storage and package observer modes are exclusive.

The same spare card carries two paths:

| Volatile JTAG image | SPL boot file | Purpose |
| --- | --- | --- |
| `network-recovery.sof` | `/u-boot.itb` | Recovery loader, manual prompt, Ethernet download and experimental file writing |
| `native-probe.sof` | `FES/UPLOAD.BIN` | Exact sealed native FIT, cold serial ownership handoff, manual `fesboot` |

Recovery's raw prefix, root boot FIT and `FES/RECOVERY.JSON` stay unchanged.
Uploading a file does not activate it. A different native FIT needs its matched
SPL/SOF because SPL checks the whole file's compiled SHA-256 **before parsing**.
The parser then reads the verified RAM copy at byte granularity, without FAT
DMA rounding beyond the sealed file or rereading FAT. This is
source-bound diagnostic sequencing, not signed or measured secure boot.

## Repeatable network upload

`upload.py` defaults to a file-only plan. It supports one fixed diagnostic file
per invocation. The default `--purpose boot` writes `FES/UPLOAD.BIN`;
`--purpose package` writes `FES/PKGTEST.BIN`, each bounded to 8 MiB.
`--purpose metadata` writes `FES/PROGRAM.JSN` with a 16 KiB bound, and
`--purpose selection` writes `FES/CORESEL.JSN` with an 8 KiB bound.
The second fixed package slot uses `--purpose package-alt` for `FES/PKGALT.BIN`
(8 MiB) and `--purpose metadata-alt` for `FES/PROGALT.JSN` (16 KiB). A selection
catalogue can refer to both slots. These limits
match native retention and selection parsing. Arbitrary destination paths are
rejected. Use one `UploadSession` for a multi-file update and fresh readbacks;
separate CLI sessions cannot adopt the preceding live RAM reservations. For two
packages, write the FIT, both archives and their metadata, then the selection;
freshly verify all six files after the final write. The native service still
retains and checks the selected archive and metadata before every load.
For a native candidate, supply the
matched build's `build/u-boot.itb`:

```sh
python3 platform/embedded/nano/upload.py \
  --payload out/nano-uboot/build/u-boot.itb \
  --recovery-kit out/nano-recovery-kit \
  --host-ip 192.168.10.202 --output out/nano-upload-plan
```

Use the **current** local Ethernet address for `--host-ip`. For an authorized
live run, freshly boot the exact recovery loader with the spare card and leave it at
`SOCFPGA_AGILEX5 # `. Add `--execute` and choose a fresh output directory.
The operator needs access to the Nano UART and permission to bind UDP port 69
(normally run the same command using `sudo`). No system TFTP service is changed.
The tool claims `~/kit-status/de25-TRWU8CBL-STATUS.md` and selects only the stable
TRWU8CBL UART `if02` link. It does not shut Linux down, program JTAG or boot a
candidate. The default volatile MAC is `02:fe:25:08:cb:01`; DHCP supplies the
board address, so no board IP is assumed.

Use one upload attempt per fresh recovery boot. The pinned loader retains LMB
reservations from FAT reads, including reads made before a failed transfer.
A later invocation must not overwrite those reservations; restart recovery
rather than clearing or ignoring its live memory map.

The tool observes the current MAC, reuses a matching write-once value and sets
it only when empty. A different address rejects. It explicitly probes the
expected Ethernet device, then requires two consecutive PHY link/negotiation
observations within 20 seconds before DHCP. Unknown PHY status or a missed
deadline stops before download or SD writing; the PHY command is separately
bounded. This initialization runs in recovery and grants no native ownership.

Before the only FAT write, the tool checks the loader banner, live RAM and LMB
reservations, spare-card capacity/partition layout, protected first 16 MiB,
root recovery FIT and card manifest. It serves one randomized TFTP filename to
one observed client, requires the final acknowledgement and verifies RAM SHA-256.
It writes the selected fixed file, reads it into a separate buffer, checks SHA-256 and
compares bytes, then rechecks the protected prefix, recovery FIT and card manifest. The receipt
contains the commands, outputs and completion state.

Timeouts, wrong cards, hashes, ownership loss and uncertain writes stop the
attempt. There is no automatic replay or activation. If the serial prompt is
uncertain, the tool leaves its claim held for recovery. A completed write is
verified readback, not an atomic FAT transaction or a power-loss guarantee;
FAT metadata damage can still prevent recovery boot. Keep the factory card safe.

The [package-retention trial](../../../docs/validation/2026-11-01-de25-sd-packages/README.md)
stopped before download or SD writing when the previous generic-PHY recovery
timed out during its first Ethernet start. The board's readback is KSZ9031
`00221622`. Recovery now requires the matching Micrel driver and gigabit support,
checks the linked driver function/registration independently during build and
packaging, and retains the existing 4,000 ms negotiation limit. Its banner is
`-fes-nano-recovery-ksz9031`; upload plans require this profile in both recovery
metadata and the running loader, then check the live PHY ID before link/DHCP.
An earlier positive status sample cannot bypass a failed Ethernet start.
The matched corrected card and bounded test now pass as described above.
This qualifies recovery Ethernet and the diagnostic's read-only package path;
native Go Ethernet and physical FPGA admission remain separate work.

## Read-only native package probe

The [package store](../tamago/packagestore/README.md) joins the existing package
validator to actual owned SD/FAT32. Cross-build `run.py sdpackages --board
de25-nano --profile platform/embedded/nano/sd-profile.json --package-id
"$PACKAGE_ID"`, with the same compiler/output options as the storage probe.
The package ID is compiled into the diagnostic and recorded in its build
receipt. Other programs and QEMU reject this option. Prepare fresh entry,
BL31, U-Boot and matched native SOF/FIT artifacts from that exact build.

Stage the FIT with the default upload purpose and the independently checked
canonical format-2 Nano package with `--purpose package`. Use a **fresh recovery
boot for each upload**, then the matched native SOF for one bounded serial run.
The probe never programs the retained payload. A synthetic archive is appropriate
for this storage diagnostic but supplies no FPGA or playable-core evidence.
Physical activation, ROM linking on Agilex and storage return across actual FPGA
transitions remain separate work.

## Actual cold ownership handoff

For an observation that cannot proceed into the native chain, `cpu_diagnostic.py`
builds a separate SPL from a completed cold serial build and its exact pinned
tools/reference SOF. It keeps the first sample and fresh MPIDR/reset report,
then unconditionally prints `FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31`
and spins with `yield`, including when admission would pass. The private
`board_init_f` ends at that report; later DDR/device initialization is removed.
The seed configuration is retained except for the diagnostic identity; drivers
can stay linked. Both watchdog-start paths are removed, and the later SPL
initializer independently stops on entry. The linked park and excluded
watchdog-start/firmware-entry symbols are checked. The vendor's `SPL_ATF` and its required `SPL_LOAD_FIT`
selection stay enabled; the later ATF invocation is independently replaced by
a stop and no firmware payload is supplied. The build checks the actual Nano
startup sources and emitted startup text against the seed. No native CPU/reset
admission rule changes.

The trace records 18 lifecycle checkpoints from first C entry through the cold
sample, device setup, managers/pins, timer, mailbox, clocks, console, report and
park. Seven power/request/boot-configuration words are sampled before the cold
proof, and seven more immediately before park. The complete 32-event trace
fits its existing 304-byte record in loaded OCRAM data. The power helper uses
fixed 32-bit reads and never changes power requests or reset state. Before
clock setup returns, status reads store only OCRAM and do not access UART.
After clocks, guarded raw UART output can replay the
record before normal console setup if UART1 is already released and in 8N1.
Otherwise it tries after normal console initialization. It does not change
UART reset, clock, divisor, framing or interrupts, and caps TX polling per line.
Complete serial lines carry the build ID, stage, value, first-C MPIDR and EL.
Partial, reordered, mixed-identity or inconsistent-affinity traces never count
as a complete capture. None of these observations grants native admission.

`--console-control` prepares a separate contained comparison. It performs no cold proof
or tracing before normal console initialization. Both watchdog-start paths
are removed: the first board initializer ends in the unconditional diagnostic
park, and the later SPL initializer independently stops on entry. The linked
image is checked for absent watchdog-start, cold-proof and firmware-entry
symbols. DDR/storage drivers can remain linked with this configuration; the
two initializers cannot proceed to their later initialization/loading paths.
This control emits checkpoints `10`, `11` and `12` plus seven pre-park power
words, with no first-C power sample. Six bracketed register observations run
between the report checkpoint and power sample. Its MPIDR/EL scope is
after console initialization, not first C entry. Those observations never
supply the initial trusted ownership proof or grant native admission.

If execution stops before clock setup, a silent serial result can still be
inconclusive. Earlier checkpoints remain in OCRAM, but their retrieval over
JTAG has not been qualified. The retained reference SOF has HPS DAP disabled;
an HPS debug configuration would need a separately built and checked image.
A host timeout also cannot preempt a stalled target MMIO operation.

```sh
python3 platform/embedded/nano/cpu_diagnostic.py \
  --reference-repo out/vendor-uboot --seed-build out/nano-uboot \
  --entry-build out/nano-entry --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --vendor-sof /absolute/path/golden_top_hps.sof \
  --quartus-pfg /absolute/path/quartus_pfg --output out/nano-cpu-observation
```

This prepares files only. A separately authorized bounded volatile JTAG run
can keep the factory card inserted after a clean Linux shutdown. Capture the
exact SPL banner, `BEGIN`, `SCOPE` and unconditional park. Restore and authenticate
the factory system when recovery is needed or the baseline is required. For
TRWU8CBL the user explicitly permits leaving the diagnostic loaded; record the
actual state and hold the claim when execution/park are uncertain. A responding
JTAG chain does not prove CPU execution or a factory boot. This fixture neither
boots BL31 nor qualifies native execution, DDR, storage, ownership or drain.

The dedicated SPL samples SDM boot kind, reset cause, peripheral/bridge resets
and secondary CPU state before device initialization. The pinned
[vendor DDR code](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/drivers/ddr/altera/sdram_agilex5.c)
defines POR, cold reset, nCONFIG and JTAG configuration as kinds 0, 2, 3 and 4;
these are the reviewed entry paths. Reset-status bits 0 and 2 record SDM cold
reset and the sticky last-POR flag; the latter is not independent history proof.
Warm reset, RSU, unknown kinds, other reset
causes, open bridges or released unused masters reject. Reset bits alone do
not supply history or traffic-drain proof.

The private version-2 record adds the four CPU power-status words, MPU power
request/status and boot configuration. Trusted EL3 entry must execute on the
selected primary (core 0 or 2). Both CPU-channel masks must request/acknowledge
only that primary, every spare power-status word must retain its zero reset
default, spare reset-history bits must remain set, and no CPU release may be
pending. The executing primary's reset bit is not interpreted as live state:
the [physical comparison](../../../docs/validation/2026-11-01-de25-cpu-inventory-reference/README.md)
found it set even with all four Linux CPUs online. The
[boot guide](https://docs.altera.com/r/docs/813762/25.3/hard-processor-system-booting-user-guide-agilextm-3-and-agilextm-5-socs/boot-flow-overview-for-fpga-configuration-first-mode)
places non-boot CPUs in WFI. Zero spare status is used only as evidence of
untouched initial controllers; it is not a general CPU-off encoding or support
for hotplug. CPU-channel and boot-selector masks come from the pinned TF-A
power manager and its secondary-entry code. Other boot-configuration bits are
not decoded. The primary power word and non-CPU channel bits are observations.

The owned cold firmware denies PSCI CPU_ON in its public frontend, before
framework bookkeeping or any vector, power or reset write, without invalidating
an otherwise valid owner. Its lower platform callback also refuses release
using the framework's supported internal-failure result. The strong release-lock
proof lives beside the denied frontend and is required by the EL3 consumer and build
receipt. Other power/reset transitions still retire ownership. A version-1
record or the old six-register tuple cannot grant this version-2 contract.

Only SD is used, synchronously in PIO mode. SPL loads BL31, U-Boot and the native
entry from the sealed FIT. Before entering BL31, it verifies the entry hash,
disables SD interrupts, waits for independent SDHCI idle/buffer observations,
disables the card clock and checks readback. It then asserts functional resets
followed by OCP resets, with barriers/readback. Failed drain observations stop
before those resets. See the separate functional/OCP ordering in the
[HPS reset sequence](https://docs.altera.com/r/docs/814346/26.1/hard-processor-system-technical-reference-manual-agilextm-5-socs/reset-de-assertion-sequence?contentId=Z8zFZqWjknEOqRcvl2Xvbw).
The CS/R1 register map distinguishes the SD controller reset (PER0 bit7) from
its ECC OCP diagnostics reset (bit15). The PIO-only profile releases the
controller and keeps unused diagnostics held from the first sample. A held
diagnostics reset does not replace the independent SD drain/clock checks.

The record stays in SPL's loaded OCRAM data, not its DDR BSS. SPL passes its
pointer/cookie directly to early EL3 setup. BL31 bounds-checks and copies it
once, checks the exact source chain, owner, epoch, boot kind and live scope,
then installs the real platform ownership/master callbacks. Those callbacks
verify continued containment without touching SD while it is in reset. Any
restarted producer, open bridge, changed CPU/reset scope or replay rejects.
The additional `SCOPE` line reports MPIDR, primary index, fresh reset/CPU
registers and reset timeout status after clock/console setup. It cannot replace
a failed first sample. The initial and pre-park
[power diagnostic](../../../docs/validation/2026-11-01-de25-el3-power-inventory/README.md)
supplied the additional observations used by the version-2 prototype. That
diagnostic kept the old gate and rejected admission. The first matched v12
native attempt accepted the initial parking check, initialized 1 GiB DDR and
verified the sealed FIT, then stopped at the combined final handoff/drain gate.
It did not enter BL31 or run the native command. See the
[final handoff diagnostic](../../../docs/validation/2026-11-01-de25-final-handoff-diagnostic/README.md).

On a final handoff rejection, SPL now prints saved first/before/last samples,
SD observations, actual/expected entry hashes, individual EL/FIT/entry gates
and the first stopped `fes_nano_cold_finish_stage` from `nano_cold.h`.
`NANO_COLD_*` lines are failure context, not admission evidence. Stage 2 or 18
means the corresponding before/last sample may be incomplete; zero fields
after an unreached stage do not mean hardware was read as zero. Reporting
performs no new device reads. The stage and saved intermediate values stay in
the local context, outside the version-2 sealed record. A failed source remains
retired; the diagnostics do not change checks, ordering or timeouts.

The ordinary cold serial firmware permits read-only ownership Snapshot and
optional source-context reads. Generic bridge opening, reconfiguration and
release are denied, and configuration features remain zero. The dedicated
identity diagnostic below has one contained secure control transaction.
These profiles supply no active USB, Ethernet or FPGA traffic-drain policy.
The default controlled firmware still has an unready provider; recovery uses
the separate strict firmware. See the [firmware guide](../firmware/README.md).

Full U-Boot never probes/reopens SD and has no USB, network, DMA, flash-storage
commands or persistent environment. `fesboot` hashes the preloaded entry and
checks memory, CPU, watchdog and private ownership state. A late hook after
device removal rechecks the same owner/generation, timer, UART and resets.
The existing guarded EL2-to-EL1 entry then starts the serial Go probe. There
is one attempt per boot; uncertain failures park. Tenfoot and physical FPGA
launch/return remain later integration steps.

## Source-derived memory contract

| Resource | Enforced selection |
| --- | --- |
| Physical DDR | Require the live Nano bank `0x80000000..0xc0000000` |
| BL31 and initial U-Boot | Reserve `0x80000000..0x82000000`; initial U-Boot load is `0x80200000`, checked against BL31 ELF segments/BSS |
| SPL parser workspace | Conservatively reserve `0x82000000..0x84000000` |
| Go RAM | `0x84000000..0x94000000`, with early tables/stack checked by the entry validator |
| Verified FIT copy | `0x94000000..0x94800000`, at most 8 MiB |
| Preloaded entry | `0x96080000..0x98080000` maximum, separate from Go RAM |
| SPL/U-Boot upper DDR | Reserve `0xb0000000..0xc0000000`; SPL malloc/BSS are near its top |
| Live U-Boot | Reject Go/entry overlap with LMB, malloc, relocated code, FDT or the stack envelope |
| CPUs | Accept A55 Aff1=0 or A76 Aff1=2; require other CPUs held/OFF, retain that state through cleanup |
| Timer/console | Require 400 MHz architectural counter; preserve UART1 at `0x10c02100`, clocks/divisor and checked 8N1 state |

TF-A's larger MMU mapping and the U-Boot DTS CPU `reg` numbers do not replace
the live RAM and Aff1/PSCI checks. Unreturning MMIO/SMC operations cannot be
preempted by software poll limits or a host capture deadline.

## Build the matched candidate

Use the pinned clean Terasic references and compiler environment from the
[firmware guide](../firmware/README.md). First build the serial probe and
[native entry](../entry/README.md), then the cold firmware and native loader:

```sh
python3 platform/embedded/tamago/run.py probe --board de25-nano \
  --profile platform/embedded/nano/probe-profile.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-probe
python3 platform/embedded/entry/build.py --target de25-nano \
  --payload out/nano-probe/probe.elf --nano-build out/nano-probe \
  --profile platform/embedded/nano/probe-profile.json --image-address 0x96080000 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/nano-entry
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/vendor-tfa --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --nano-owned-bridges --nano-cold-serial-entry out/nano-entry \
  --output out/nano-bl31
python3 platform/embedded/firmware/build_terasic_uboot.py \
  --reference-repo out/vendor-uboot --bl31-build out/nano-bl31 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --native-entry-build out/nano-entry --output out/nano-uboot
python3 platform/embedded/nano/package.py \
  --uboot-build out/nano-uboot --entry-build out/nano-entry \
  --recovery-kit out/nano-recovery-kit \
  --vendor-sof /absolute/path/golden_top_hps.sof \
  --quartus-pfg /absolute/path/quartus_pfg --output out/nano-test-kit
```

The initial SPL compilation has a zero seal and cannot boot a FIT. After the
FIT is built, the builder compiles its exact size/hash into SPL and rebuilds
only SPL, requiring the FIT to remain unchanged. The FIT does not contain SPL,
so the seal has no hash cycle. PFG combines that SPL with the inventoried
vendor SOF (`e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09`)
and checks the Nano device and FPGA-first order. The packager additionally
uses `sof_hps.py` to unpack the bounded gzip SOF, decode its counted
literal HPS chunks and compare every SPL byte with the sealed Intel HEX.
It requires contiguous OCRAM coverage from zero, checks the section topology,
and preserves every reference section except the replaced HPS payload and
generated hash/checksum fields. Unsupported formats or changed bytes reject
before the SD image is assembled. The inspector's source hash is an input to
the package receipt. This checks payload identity, not SDM command semantics,
security hashes, configuration CRC, programming or boot acceptance.

Packaging takes a corrected FAT-boot recovery kit, copies its regular-file SD
image, and adds the native FIT at `FES/UPLOAD.BIN` plus `FES/BOOTCHAIN.JSON`.
It checks readback and preserved recovery files/prefix. It produces both
volatile SOFs and `nano-probe-sd.img`; it never writes a physical card.
The kit directory's `u-boot.itb` is the native upload payload. Do not copy it
over the card's root `u-boot.itb`, which belongs to recovery.

## Compose the HPS-first FPGA diagnostic with native SPL

`cold_profile.make` reuses identical isolated HPS worker results only within
one build-time derivation. Complete source-tree membership and file hashes,
sealed projects and declared source roots, entry files and native Go sources
are checked before and after that call. Results are copied for each caller;
failed workers are never cached. Separate calls before and after compilation
start empty, and callers outside this scope still run fresh workers. This
does not reuse live device, ownership, programming or boot observations.

`hps_boot.py` prepares regular files from the sealed
[HPS-first FPGA diagnostic producer](../../../sources/misteross/prototypes/de25_board_control/README.md)
and an existing matched native SPL/FIT/entry build:

```sh
python3 platform/embedded/nano/hps_boot.py \
  --project out/de25-hps-first \
  --uboot-build out/nano-uboot --entry-build out/nano-entry \
  --quartus-pfg /absolute/path/quartus_pfg --output out/nano-hps-first
```

The tool reuses the package producer's source/archive/pair verification and
the native packager's complete FIT/SPL/entry checks. The selected PFG wrapper
and binary must match the FPGA build. It combines the raw HPS-first SOF with
the native SPL, decodes every embedded SPL byte, preserves the other SOF
sections relative to the sealed vendor-SPL composition, and independently
splits the result again. The assembler's raw SOF omits the SPL section and is
not a complete reference for the strict SPL decoder. Both split files must
report the original HPS I/O hash, and the core payload must exactly match the
canonical package. Each conversion or inspection has a 120-second bound and
its own log. Failed checks leave diagnostics without a success receipt.

The complete native validator runs before conversion. Its checked source and
artifact digests are verified before and after conversion, including the native
ELF, profile, FIT/SPL/configuration and all native/entry/U-Boot producer sources.
Receipt bytes are captured before validation, so edits between validation and
the input snapshot also reject. FPGA provenance is fully checked again after
conversion. This avoids repeated context derivation while retaining input-change
rejection and the existing converter time bounds; it grants no hardware access.

The output contains `native-hps-first.sof`, `native-hps-first.hps.rbf`,
`native-hps-first.core.rbf`, the native FIT/SPL/entry copies and `result.json`.
The native phase-one identity is recorded separately from the original vendor
SPL pair. The original sealed package and pair remain immutable. This is
unqualified file composition: the receipt explicitly denies trusted live boot
handoff and physical programming admission. It cannot replace the model's
existing boot assertion or establish physical CPU mapping, DDR preservation,
DMA ownership or active bridge return. No board, card or flash is accessed.

The subsequent [bounded HPS-first loader observation](../../../docs/validation/2026-11-01-de25-native-hps-pair/README.md)
booted the composed SOF on TRWU8CBL through DDR, sealed FIT verification and
the matched manual loader prompt. It sent no native entry command or SD
write. Native FPGA admission and the HPS-first physical handoff remain
unqualified; the original vendor-SPL pair is unchanged.

## Source context readback

The separate `hpsio` native diagnostic selects `probe-profile.json` and
`--hps-first-project`. It derives the complete expected pinmux map from that
sealed build's compiled handoff, carries its digest through the SDK, entry,
cold SPL/BL31 and HPS-first composition, then requests two read-only secure
samples. The strict serial decoder requires all 328 ordered register records,
both secure response boundaries and the matching native result under the
same cold owner/generation. It observes 164 Agilex 5 registers and excludes
the generic handoff entries omitted by the pinned vendor code. It performs
no FPGA load or device opening and does not qualify general boot admission,
clocks or RAM. The [bounded TRWU8CBL test](../../../docs/validation/2026-11-02-de25-hps-io/README.md)
passes both live samples in 6.7 seconds, with all bridges held and no core
configuration. Its source and hardware evidence remain separate from general
boot qualification and normal runtime package admission.

`hps_context.py` derives a canonical descriptor from a verified sealed
HPS-first project. It names the package, build inputs, unchanged core, HPS
I/O hash, original SOF/pair and source/tool identities. It excludes the native
SPL and composed phase-one hashes because including them in SPL would create
a hash cycle. The existing cold-chain seal binds the descriptor digest and
native entry together.

```sh
python3 platform/embedded/tamago/run.py bootcontext --board de25-nano \
  --profile platform/embedded/nano/probe-profile.json \
  --hps-first-project out/de25-hps-first \
  --compiler /absolute/path/tamago-go/bin/go --output out/nano-context-sdk
```

Use that ELF/receipt in the normal Nano entry build, then select that entry
for both native TF-A and U-Boot builds. `hps_boot.py` requires the same sealed
FPGA source context when composing them. The original vendor-SOF packager
rejects context-bearing images. The source context is checked again across
these steps; no board is accessed during builds.

At EL1 the native reader takes an owned bridge Snapshot, reads both secure
digest halves and takes a second identical Snapshot. It compares the digest
with the compiled descriptor before printing `FES_NATIVE_HPS_CONTEXT PASS`.
`native_trace.py` accepts this diagnostic only with independently checked
`hps_context={sha256,package_id,build_id}` and the same SPL owner/generation.
Its completion still denies core-load acceptance. The native verifier does
not implement `BootHandoffVerifier`: a matching source digest cannot establish
the live CPU aperture, HPS configuration, active master drain or programming
authority. All bridges stay closed and secondary CPU release remains denied.

The [bounded source-context test](../../../docs/validation/2026-11-01-de25-hps-context/README.md)
passes on TRWU8CBL: verified Ethernet/SD FIT update, matched native EL1 entry
and the exact secure context digest under generation 1. The original cable
clock and USB permissions are restored; the spare and diagnostic remain in
place under the user's retained-diagnostic policy. This does not qualify CPU
FPGA MMIO or runtime core loading.

## CPU FPGA identity diagnostic

`run.py fpgacontext --board de25-nano` uses the same required profile,
`--hps-first-project`, compiler and matched entry/SPL/BL31/composition steps as
`bootcontext`. It additionally verifies the actual generated
`qsys_top.sopcinfo` against both compiler and generator hashes, follows the
lightweight bridge to the sealed endpoint, and rejects ambiguous or
overlapping routes. The canonical `hps-control.json` and its digest are
compiled into the native image and secure cold chain. The CPU aperture uses
the [Agilex 5 MPU lightweight base](https://altera-fpga.github.io/rel-25.1/embedded-designs/agilex-5/e-series/premium/gsrd/ug-gsrd-agx5e-premium/#lightweight-hps-to-fpga-address-map)
`0x20000000` plus the verified route offset; it does not use the JTAG master's
address space. The current sealed project's candidate is `0x20010400`.

After the source-context marker, native Go invokes one secure identity
operation with the current owner/generation and no physical address. Firmware
first requires a complete read-only SDM configuration-status response with
CONF_DONE and INIT_DONE. Unready fabric leaves the original bridges closed
at generation 1 and returns NOT_READY. Native Go rechecks the same source
ownership before reporting `FES_NATIVE_FPGA_UNREADY CLOSED`. Ordinary missing
providers also return NOT_READY; only the raw SDM response distinguishes them.
For a ready fabric, firmware requires EL3 translation and data cache disabled, opens only control, reads
two complete identity/idle snapshots, verifies magic/version/ABI/build and
zero generation/pending/acknowledgement state, then closes control. Data
bridges and masters stay contained and spare CPU release stays denied. A
failure asserts control reset independently, retires ownership and does not
retry. The raw trace preserves open/read/close/closed stages and saved
failure details without speculative extra reads.

`decode_native_boot(..., fpga_identity={sha256,package_id,build_id,control_abi,
cpu_base,map_sha256})` requires independently checked descriptor/map values,
the same SPL owner, bridge generations `1/2/2/3`, reset masks `f/d/d/f`, and
the final `FES_NATIVE_FPGA_IDENTITY PASS`. The native reader separately
checks a closed generation-3 Snapshot before reporting success. This source
path and its injected failure tests do not establish physical CPU access.
Even a successful physical identity read cannot grant runtime core admission,
measure the live HPS I/O configuration, authorize SDM programming or prove an
active fabric/master drain. `HPSContextVerifier` remains separate from
`BootHandoffVerifier`.

The [first CPU identity attempt](../../../docs/validation/2026-11-01-de25-cpu-fpga-identity/README.md)
passed upload and cold native handoff, then stopped after control opened; no
endpoint identity or reclose is accepted. This is why readiness precedes
aperture access. HPS-first boot has a separate fabric-configuration step; a
successful HPS boot is not fabric readiness. The new readiness path is a
separate diagnostic until physically observed. Select
`fpga_configuration=True` in the decoder to require the SDM query/reply and
all 30 per-register before/after pairs. Select `fpga_unready=True` as well
for the closed, not-ready branch; that completion proves no endpoint identity.
An abort or hung MMIO may prevent C cleanup from running. The host timeout
retains the first failure rather than establishing a safe reclose.

`run.py fpgastatus --board de25-nano` uses the same sealed project, source
profile and matched chain as `fpgacontext`. It selects the separate read-only
configuration operation, checks the unchanged closed Snapshot, and prints
`FES_NATIVE_FPGA_STATUS CLOSED ready=0|1`. It never requests endpoint identity
or control opening. Use `fpga_configuration=True, fpga_status_only=True` with
the independently checked `fpga_identity` descriptor/map to decode this path.
Both ready values can complete this status diagnostic; neither accepts an
endpoint identity or runtime core load. This path needs its own physical test.

## Contained phase-two configuration

`run.py fpgaload --board de25-nano` selects an explicit first-load prototype.
It requires the exact controlled SD profile (`sd-profile.json`) and a sealed
HPS-first project. Its producer emits that package's canonical `package.fcore`
as a separate upload artifact and derives a configuration descriptor with
SHA-256, SHA-384, per-chunk digests and the fixed transfer mapping. The native
image contains the expected identities and digests; the built-in RBF is removed.
At boot it retains `FES/PKGTEST.BIN` through the real read-only FAT/package
store, contains SD and consumes the retained payload from RAM. The SDK, entry,
SPL and BL31 validators rederive the descriptor; omitted or changed core bytes, maps and staging exclusions
reject. The FPGA core payload is not rebuilt or modified by this path.

The SDK also derives a `cpu_mapping` contract from those exact resource maps.
Entry and the matched chain require its complete digest and reject omitted
or substituted resources. Before the first transfer, the native image checks
live EL1 CPU/MMU state, two complete descriptor samples and hardware read/write
translations for Go RAM, the control window, configuration DMA and any reader
arena. It prints `FES_NATIVE_CPU_MAPPING` under the unchanged secure owner and
contained SD generation. The actual copy, configuration SMC and MMIO hooks
also enforce the [mapping contract](../tamago/board/arm64map/README.md).
These are EL1 mapping checks; physical RAM ownership, stage 2 disabled and
firmware/controller readiness remain separate matched-boot obligations.

Pass the independently checked `cpu_mapping` to the serial decoder for a new
configuration build. It requires exactly one ordered observation before the
first configuration, including in two-load mode. Its absence, changed CPU,
address, attribute, owner or generation rejects completion. The second cycle
retains the same reservation and must still provide fresh transfer and return
evidence. A successful mapping report grants no ordinary runtime admission.
The [bounded CPU mapping/two-load observation](../../../docs/validation/2026-11-03-de25-cpu-mapping/README.md)
passes these checks and both fixed-package Run/return cycles on TRWU8CBL for
its exact matched native artifact. Ordinary Nano package switching remains open.

Adding `--computer-control` selects the separate control-only ZX81 command
experiment. It is mutually exclusive with `--ddr-read`. The producer derives
the computer aperture, clocks/reset, internal-RAM parameter and capability
contract from the same sealed project. SDK and entry validation rederive it;
the cold chain enables its one-shot owned control scope only when all metadata
agrees. The native program checks full GP identity, exercises run, neutral
keyboard state, hold and relaunch, then verifies held control reclosure. The
current producer's version-1.1 transport adds an independent fetch count/address
from actual Z80 bus strobes. The native program observes three held intervals
and two running intervals; the count must remain stable while held, advance
on both runs and survive return. It reports `execution_observed=1` only after
those fetch checks and verified reclosure. This measures instruction-fetch bus
activity rather than retired instructions, correct emulation or HDMI output.
The earlier [physical command-only trial](../../../docs/validation/2026-11-02-de25-native-computer-control/README.md)
retains its truthful `execution_observed=0` result on its exact older artifact.
The new producer and native program require their own physical qualification.
No normal runtime admission is enabled. The original 20-second configuration
deadline and held data paths are preserved.

The host calls `decode_native_configuration(..., computer=checked_descriptor,
computer_activity=True)` for the current matched experiment. It requires all
eight ordered fetch observations, the exact contained SD transfer, pinned memory,
fresh SDM/translation/firewall observations, complete held-data master stop and
the final computer report with the same owner and reclosed generation. Missing,
replayed, malformed or mismatched required records reject; incomplete output
retains progress. This mode excludes an active DDR read and the separate EL3
identity-probe sequence. A complete report identifies the observation as
`z80-instruction-fetch-bus` while keeping runtime admission and core-load
acceptance false. The historical command-only decoder selection defaults to
`computer_activity=False`; it requires an explicit zero execution claim and
cannot infer the stronger selection from missing or additional serial lines.

Add `--computer-reload` to the native `fpgaload --computer-control` build for a
bounded two-load diagnostic. The SDK, entry and SPL/BL31 chain carry that exact
boolean. Each load uses the same retained sealed package and keeps the original
20-second transfer deadline; each Run/return probe keeps its five-second bound.
The data bridges remain held and, without storage return selected, the SD filesystem is not reopened for the
second load. This mode grants no arbitrary-package or ordinary runtime admission.

Select `decode_native_configuration(..., computer_activity=True,
computer_reload=True)` from that checked build flag. It requires two exact
BEGIN/END cycles, one complete cold boot, and the same returned owner with
configuration generations 4/6 and reclosed generations 6/8. Both transfers need
fresh `FES_NANO_FABRIC_ACTIVATION` records, readiness/translation/firewall checks,
complete independent reader/probe stops, fresh zero fetch counters followed by
Run progress and held stability, and continuous bounded CPU/hash timing.
The second phase cannot repeat the boot or reuse a preceding activation,
master-stop, counter or completion. Missing, overlapping or partial cycles
cannot complete. `loader_ready` comes only from the first verified boot;
`core_load_acceptance` stays false. The source and software checks do not qualify
a changed two-load physical boot.

Add `--computer-storage-return` with `--computer-reload` to select a fresh SD
read between the two core cycles. SDK, entry and secure cold-profile receipts
bind the selection. After the first successful Run/Close, native code checks
its private return, requests a new firmware SD lease, rediscovers the original
card and retains `FES/PKGTEST.BIN` again before containing SD. The second transfer
uses those newly retained bytes. Firmware requires its own producer-stop/held-data
closure and completed SD stop before the next activation. Active or uncertain
ownership prevents the operation; no native SD writes are added.

Pass `computer_storage_return=True` and the checked CPU mapping contract to the
serial decoder only for that selected build. It requires ordered
`FES_NATIVE_STORAGE_RETURN BEGIN`, `ACTIVE` and `CONTAINED` records strictly
between the first verified return and the next cycle. Generations are activation
4, return 6, prepared SD 8, contained SD/activation 9 and final return 11. The
20-second configuration bound, 50-second prompt and 45-second core observer
bounds remain; the additional SD read has its own 100-second phase. The current
warm-return implementation has software/PIO-model checks and a cross-build.
Physical warm-return qualification, arbitrary package selection and normal
runtime admission remain open.
The [first bounded warm-return sequence](../../../docs/validation/2026-11-03-de25-returned-sd/README.md)
stopped at recovery PHY autonegotiation before any SD write or native attempt.
Its failure, read-only PHY follow-up and verified cleanup are retained. The
next recovery repair must prepare the PHY before accepting link readiness.

The current block-size comparison selects `0xfffc0` (1,048,512) bytes: the
pinned vendor client's `0xffff0` limit rounded down to a 64-byte boundary.
Native construction, secure extent/digest checks and trace decoding use the
same rederived descriptor. The arena, source bytes, held paths and 20-second
transfer deadline are unchanged. The
[matched TRWU8CBL comparison](../../../docs/validation/2026-11-02-de25-large-blocks/README.md)
passes a complete 2,027,520-byte transfer, firmware readiness and live sealed
identity with control reclosed. That retained source used the preceding built-in
payload. The separate
[SD-package configuration trial](../../../docs/validation/2026-11-02-de25-stored-configuration/README.md)
also passes its exact retained source/artifacts, using identity-only probing.
The current reader-cycle caller is a new candidate and requires its own
matched boot and physical acceptance. This two-job cold diagnostic does not qualify
queue turnover, repeated runtime loading, master drain or active launch/return.

| Owner | Range |
| --- | --- |
| Native Go RAM | `0x84000000..0x94000000` |
| Sealed FIT staging | `0x94000000..0x94800000` |
| Boot-pinned configuration arena | `0x94800000..0x95000000` |
| Entry image | Begins at `0x96080000`; checked against every exclusion |

The board maps only the explicitly selected arena as normal uncached memory,
copies the verified core, publishes writes and retains the reservation until
reboot. A completed transfer cannot reassign this arena. Other native programs
have no configuration mapping. Secure code accepts no pointer or digest from
the preparation request: its fixed addresses and expected whole/chunk digests
are compiled into the matched cold profile.

Private operation 8 prepares one transfer under the existing owner lock and
the current never-enabled, contained generation. Initialization remains at
generation 1; one SD resume/prepare/stop cycle advances it to 4 before
configuration captures it. A stale receipt or active SD lease cannot prepare. The four existing contained configuration calls then validate
the same closed bridge/master scope, exact chunk extents and SDM-relative
completions. The native command uses the runtime's shared transfer engine;
accepted BUSY writes cannot replay, and an uncertain response or deadline
retains the issued memory. Context, SD and endpoint requests cannot interleave
with a prepared/running transfer. No configuration call opens a bridge.

After completion, native code verifies the same closed source ownership and
reports the exact programmed-byte digest and count. A separate fresh SDM
readiness query precedes the owned secure reader operation, which opens only
control, verifies the idle identity, enables the on-chip reader, holds an AXI
address across disable and completes a two-beat burst before accepting the
exact generation/cookie drain ACK. Two final identity/state snapshots precede
control reclose. `decode_native_reader_configuration(..., from_storage=True)`
requires the ordered transfer, each paired reader access/write, held/partial
observations, final ACK, reclosed identity and native reader receipt. Only
state-read sets may repeat, within the same firmware bound. The actual strong
provider/RTL fixture is decoded through this observer as well. Identity-only
success cannot substitute for the reader result. This on-chip responder is
not DDR, scanout or other-master containment; all data bridges remain reset.

The [first physical transfer](../../../docs/validation/2026-11-01-de25-contained-configuration/README.md)
stopped at its unchanged 20-second native deadline after 786,432 confirmed
bytes, before identity or bridge opening. Transfer markers now include the
physical counter and cumulative SHA-384 ticks to separate digest cost from
other elapsed work. These observations grant no
authority. Timed traces require monotonic counters and digest accounting
within elapsed time; the earlier untimed trace remains independently decodable.

The [bounded timing experiment](../../../docs/validation/2026-11-01-de25-configuration-timing/README.md)
measured at least 16.837575 seconds of secure hashing before a 20.121780-second
late-return stop. The cold configuration profile specializes the same pinned scalar digest
with three loop-unrolling hints and `-O3` on its object alone, preserving every
whole-image/chunk check, ownership rule, CPU/cache state and the 20-second
deadline. Independent digest vectors and injected secure-provider paths pass;
code inspection is not a physical speedup or configuration-acceptance result.

The [scalar-digest board trial](../../../docs/validation/2026-11-01-de25-scalar-digest-load/README.md)
measures the first whole-core digest at 1.164858 seconds, compared with
3.216320 seconds in the instrumented baseline. Configuration still stops at
the same 20-second bound: 786,432 bytes complete, with the next 65,536-byte
buffer outstanding for 10.523879 seconds across 86 polls. All source bytes stay
retained; no identity or bridge-opening marker follows. This paired digest
measurement does not qualify a complete core load. Mailbox/configuration
accounting and SDM state need further bounded diagnosis.

The contained engine now propagates rejected submissions and firmware error
responses even when they carry no data words. It retires on timeout or malformed
status and keeps the source pinned until reboot; only a genuine no-response
result continues polling. The matched image logs SDM limits and the first
transport error with its transfer position. The host decoder checks the limit
record's ownership/order, and failure records cannot qualify a completed load.
The [matched bounded trial](../../../docs/validation/2026-11-01-de25-transport-errors/README.md)
reported four credits, a 1,048,560-byte block limit and no transport-failure
record. It stopped at 20.040471 seconds with 786,432 bytes completed, 65,536
bytes outstanding and all 2,027,520 source bytes retained. No identity or bridge
opening followed. All eleven software groups passed, including the actual
engine and both firmware pins; the physical missing completion remains open.

The next candidate samples the real mailbox queues through the cold platform
path, with no new mailbox command or control authority. Its host regression
runs the pinned vendor parser and FIFO transport against a synthetic SDM from
all sixteen response-ring positions, including full-stream and job wrap,
delayed, missing and error replies. Only real acknowledgements advance the
transfer; passive queue records cannot do so. The bounded sampler and trace
decoder are tested off-board. The
[matched queue trace](../../../docs/validation/2026-11-01-de25-response-header/README.md)
samples a consumed thirteenth command, an empty response FIFO and idle local
parser/descriptor state while the completion stays pending.

An additional actual-parser case exposed the vendor API's `-2` alias: it means
both no response and a consumed SDM error 2. The contained completion path now
checks the returned header to distinguish them. A genuine missing reply stays
pending; a consumed error retires authority immediately with the source pinned.
The 80-case parser matrix includes both cases and clears the header on every
read so that an earlier ACK cannot turn later silence into an error. The matched
header-aware trial reports no error packet, and stops at 20.097686 seconds with
786,432 bytes completed, 65,536 outstanding and all source bytes held. No identity
or bridge opening follows. The physical cause remains open.

The next loader fills the firmware's bounded four-buffer window and processes
up to three ordered completions per poll, matching the pinned vendor client.
The secure gate checks every pending chunk in the same fixed arena and rejects
overlap, a fifth write, invented acknowledgements or malformed batches. The
original serial client stalls against an injected peer needing later data;
the queued client completes the core-sized stream. Actual-parser, strong-provider
and trace tests cover the window without hardware. The [matched windowed
trial](../../../docs/validation/2026-11-02-de25-configuration-window/README.md)
passed recovery, Ethernet upload and independent SD readback, then stopped at
the same 786,432-byte completed prefix with all four buffers outstanding
(262,144 bytes). The sampled command/response queues were empty and the parser
idle. The source remained held and no identity or bridge opening followed.
The original 15 MHz JTAG clock and USB permissions were restored and the UART
was verified free. Filling the window did not resolve the physical stall; its
cause remains open. This source also counts byte progress inside an unchanged
endpoint phase, keeping Tenfoot launch/return and wrong-identity recovery within
the unchanged two-second controller deadline.

After a caller-side transfer failure, the native probe now requests one
explicit retirement through the same firmware/CPU channel. The secure provider
checks the held cold owner and CPU context, irreversibly withdraws configuration
and bridge authority, then collects one bounded HPS-client `RECONFIG_STATUS`
reply. The original transfer failure remains fatal. All possible DMA buffers
and source bytes stay pinned even if the diagnostic reports a ready FPGA.
The separate reader has no retry, mailbox restart, configuration acknowledgement
or bridge opening. The [matched boot](../../../docs/validation/2026-11-02-de25-hps-failure-status/README.md)
confirms retirement and retention at 786,432 completed bytes with four possible
outstanding buffers. The HPS status request receives no words in 10,000 polls,
and the original 20.184137-second transfer failure remains fatal. A later JTAG
management-status read still responds with CONF_DONE and INIT_DONE clear.
The diagnostic supplies no HPS reconfiguration state or core-load acceptance;
the physical cause remains open.

The source-bound cold DDR configuration profile now binds the complete fixed
FPGA master-stop inventory to its compiled control map. Its secure callbacks
check the live reader identity/window and maintenance reset, stop the main
reader and inherited DDR probe, and require matching independent receipts
before closing control. Initial closed observations cover the cold HPS masters
without reading fabric; a completed stop receipt remains owned after control
closes, without MMIO through the reset aperture. Actual C/RTL and strong-provider
tests cover idle, active, missing-response and ownership/failure paths.

`decode_native_configuration(..., reader_memory=checked_map, from_storage=True)`
requires the distinct DDR ABI, exact pinned-memory record, read-only SMMU and
eight-region firewall observations, idle master-stop receipt, reclosed identity
and final explicit DDR-blocked record. It independently checks raw register
interpretations and rejects missing, duplicate, reordered or contradictory
records. The firmware reports the stop's owner/generation/reset scope. Neither
firewall coverage nor clear translation controls grants access in this mode;
the final record must still report unqualified translation/drain and held data.

The separate opt-in `--ddr-read` mode has a
[fixed-read hardware pass](../../../docs/validation/2026-11-02-de25-contained-console/README.md)
and a [current computer-payload pass](../../../docs/validation/2026-11-03-de25-computer-ddr/README.md).
It verifies the full pinned CPU pattern and fresh SMMU/firewall observations,
then releases DDR for one 32-byte/two-beat read and checks reader/master drain
and bridge reclosure. The current image's independently checked CPU mapping
must be passed to the trace decoder. The first current-payload attempt stopped
because its reused observer omitted that argument; its failed capture stays
retained, and the corrected observer passes a fresh bounded attempt.

This qualifies the fixed read-only contract. The 8 KiB window and 2 MiB arena
remain pinned until reboot, including on failure. General DDR access, FPGA
writes, scanout and repeated active-DDR launches remain unqualified; the
held-data computer-switching test is separate. The production oracle continues
to reject this diagnostic target, and `HPSContextVerifier` does not implement
`BootHandoffVerifier`.

## Repeated window audit

`window_reload_trace.decode(..., window_reload=True)` requires a complete first
window/SD-return trace before the explicit second-load marker. It then checks
the same closed HPS context, a newly completed timed configuration and fabric
activation, readiness, translation/firewall observations, two changing frames,
their local drains and a new firmware closure. The second source generation
comes only from the first fresh SD containment. Missing, repeated, reordered
or changed records cannot qualify a load. The original single-window decoder
continues to require twelve frames; the repeat audit explicitly selects two
for its second session. Neither decoder grants runtime admission or HDMI
qualification.

## Contained native boot procedure

1. Claim the top board in its current status file, verify serial/cable/UART,
   and verify the recorded board/card state. If factory Linux is running,
   record its identity and shut it down cleanly before moving cards. Preserve
   the original factory card and identify the spare before writing it.
2. With the combined spare card, load `network-recovery.sof` through volatile
   JTAG. Run one upload/readback of the matched native FIT using `upload.py`.
   Require the exact recovery checks and successful receipt before continuing.
3. Load the checked `native-probe.sof`, or the matched HPS-first composition's
   `native-hps-first.sof`, through the exact top JTAG cable. Require device ID
   `4362C0DD`, the valid `FES_NANO_SPL BEGIN`, verified-before-parsing FIT marker,
   SPL handoff and `FES_NANO_OWNED_BOOT provider=1 result=0 reset=f`.
4. At the matching `-fes-nano-<build_id>` prompt issue `fesboot` once. Require
   both ownership snapshots to report the same nonzero owner, generation 1 and
   exact flags `0xdf`; then `FES_NANO_HANDOFF PASS serial-only`, native entry
   markers and the selected program's complete marker sequence. A baseline
   probe prints `FES_NATIVE_GO_PROBE PASS`; `bootcontext` requires the independently
   checked context/package/build values in `FES_NATIVE_HPS_CONTEXT PASS`.
   `fpgacontext` additionally requires the complete secure transition and
   independently checked FPGA identity sequence described above.
5. Stop on a failure or incomplete marker sequence and retain its first trace.
   Restore the original cable clock and USB permissions, and close the UART.
   Follow the user's board-retention policy: retain and record the diagnostic
   under its continuing claim when requested, or restore the original card
   and `recovery-reference.sof`, verify the factory identity and release it.
   A physical power cycle remains a recovery option if volatile recovery fails.

The basic serial trial qualifies CPU/RAM and the observed handoff. Broader native
USB/network/HDMI, general active-DDR ownership and ordinary runtime admission
still need physical qualification. Read-only SD/package retention has its separate
tested profile; this serial-only procedure supplies no active SD drain proof.

## Persistent native request-service checks

`program_client.ProgramClient` checks one owned exchange with the contained
native request service. The platform supplies the exclusive console, its current
claim check and a synchronous, bounded exchange callback. The client never opens
a device, initializes hardware, retries a command or confers package admission.
Its expected catalogue and configuration maps come from the independently
checked image and freshly verified SD files.

Startup requires the actual matched SPL/loaded-BL31/EL3/loader/EL1 prefix and a
single complete Ready reply whose boot owner and catalogue match. Each command
is issued once. Replies have strict types/fields, one terminal JSON record,
unchanged boot ownership and the expected session generation. A launch must
include the complete real configuration/queue/fabric-activation sequence followed
by the held-data control observations. Return/Park must include the exact
same-owner master stop. Shared `configuration_trace.extract` retains the existing
fixed-probe transfer grammar; it supplies no boot or bridge authority.

The bounded `exercise` sequence runs three request-driven sessions using indices
0/1/0. It checks safe rejected requests, running fetch progress, held stability,
keyboard command acceptance, resumed progress, two owned returns and final Park.
Any uncertain I/O, deadline, protocol, transfer, closure or fetch observation
retires the client and stops the sequence. No native command follows Park or a
failed proof. Serial identity is the service publication after its internally
checked live opening; it is not a separate GP register dump or ordinary runtime
admission. The
[matched board trial](../../../docs/validation/2026-11-03-de25-service-hardware/README.md)
now passes all three same-package sessions and final Park; different physical
payloads and ordinary runtime admission remain open.

The request-driven fixed-window mode has a separate read-only
`window_service_trace` auditor and injectable `WindowClient`. They require the
matched boot prefix, both route gates, every configuration activation and each
independent reader/master/bridge return. Cached reply observations cannot
replace firmware evidence or mint a session. The bounded three-session client
uses the already qualified persistent-service limits: 100s launch, 20s
configuration, 5s control and no retry after an uncertain exchange. Physical
acceptance for the new window service remains open until its matched image runs.

`--purpose ui-catalog` uploads only `FES/UI.JSON`, bounded to 8192 bytes. It uses
the same recovery-session ownership, scratch-memory checks and fresh SD readback
as the selected-program files. It does not activate a core or start native boot.
The explicitly compiled `fpgaload --tenfoot-serial` mode consumes this friendly
catalog before SD containment; see the [native launcher description](../tamago/README.md).
Its serial state output is not an HDMI/display capability.


`TenfootClient(..., shared_usb=policy)` observes the explicitly shared startup.
The checked policy supplies the input protocol, descriptor hash/length and
complete HPS context/pin map from the matched artifacts. Startup requires the
secure USB setup, source observation and both complete pin-read passes before
one shared Ready record and the matching App input marker. UART-only mode
cannot adopt these records; the shared mode does not infer omitted observations.

The native diagnostic reports owner/generation and delivered input counts at
each existing pause/resume boundary. The observer requires the same confirmed
USB generation 2 through all core transitions, increasing report counts before
each transition and unchanged counts while paused. Native generations and
states must match the requested launch/return; App completion follows resume.
Quit additionally requires the same owner's generation-3 USB stop before Exit.
Missing or malformed records, counter regressions, late boundaries and native
configuration/closure failures retire the client with no further input.
`exercise` accepts a bounded passive settle callback; the shared hardware test
uses it to give the running frontend time to receive reports before each action.

Counts establish delivered reports, including idle reports. They do not establish
physical button changes, controller mapping or playable-core qualification.
The observer still validates the actual C configuration, firewall and independent
master-stop records. No physical combined result is supplied by its model tests.

The retained-USB boot provider can bind a coherent bridge observer through its
trusted, boot-local master policy. This samples the independent producer bitmap
between paired live bridge/peripheral reset reads, with the complete retained
USB master-containment checks described below: DMA/idle, owner, generation,
bus clocks and final live boot scope. Device pin/PHY readiness is checked separately.
It avoids re-entering those same full observations through nested bridge wrappers;
there is no cache of hardware state across calls. Only a provider supplying the
explicit combined callback selects it. Unbound providers retain their existing
observation contract, and mutation/stop paths keep independent qualification.
Changed callback identity, late hardware drift or a failed read retires the
boot without publishing a snapshot. The configuration deadline remains 20 seconds.
The register-fixture work bound is a software check; physical load timing still
requires the matched rebuilt chain on the board.

Device preparation scans the complete immutable clock/pin policy once per call.
The initial capture/recheck remains paired, and controller and PHY operations
still invoke fresh preparation at their boundaries. The removed scan repeated
immediately without an intervening operation; no hardware observation is reused
across calls. Register-fixture tests inject clock/pin changes during controller
writes and PHY reads and require terminal failure before readiness publication.
Native configuration errors separately identify deadline expiry and backwards
clock movement, including elapsed/step durations and the unchanged 20-second
limit. These are failure observations, not additional hardware permissions.

The physical USB board supplier also provides an optional complete confirmed
readiness observation. It performs the full live clock/pin, DMA/IRQ/host/PIO and
both PHY observations, then independently checks cold ownership. This subsumes
adjacent preparation scans in the generic observer, with no cross-call state
cache. Only an already-confirmed lease selects it; unconfirmed initialization
keeps its preparation sequence. A failed complete observation retires the
platform, and cannot select a weaker observation path. Tests cover late policy
changes, foreign/stale requests, changed lease fields and late successful
firmware completion that must not open a computer or release staging.

Retained USB master containment now uses a separate live observation from device
readiness. The cold composition's `usb_contained` callback and the shared
firmware gate require the same confirmed private lease and fresh complete
bus-clock/boot checks around DMA-disabled, IRQ-disabled, fixed-PIO and AHB-idle
register observations. They issue no USB or PHY writes, change no generation
and cannot publish USB readiness. Shared mode requires the boot-local
`service_ops.contained` hook and checks its identity against the trusted
supplier. Unbound/exclusive profiles neither require nor call that hook.
Idle loss after the bounded wait rejects
without another wait allowance. Owner/epoch/generation and active/confirmed
state are rechecked before success. Stale or foreign requests perform no I/O.

Explicit USB QUERY/CONFIRM and the device client's observations still use the
full board readiness path, including both PHY observations and host/VBUS
validation. A PHY mismatch can leave the master safely contained while that
independent query rejects device readiness and retires the shared source.
Known failed owners never regain containment or device authority. Every
computer CSR retains its before/after firmware ownership brackets; the native
one-second/64-poll computer guard and 20-second configuration guard are unchanged.

Read-only board policy sampling brackets each complete clock/pin group with
ordering barriers. Device preparation still reads and checks all 13 captured
clock fields and 164 sealed pins on every call, in the same order; no prior sample is reused.
Controller, reset and PHY transactions keep their existing individual ordering.
Sample errors close the group boundary, retain the original error/address and
previous successful value, and stop without further reads or writes. Successful
samples publish their final diagnostic address/value once per group, reducing
uncached diagnostic stores. Tests fault every supplier read, audit both group
boundaries and complete value sequences, and require the next invocation to
reject a newly changed pin after a successful sample. Native limits and fresh
per-CSR containment checks remain unchanged.

The physical supplier now provides an explicit `prepare_master` callback for
internal master containment. It samples all captured bus-clock fields and cold
ownership before and after controller checks. External HPS pin selection affects
device signaling, not an independently DMA-disabled and idle internal AHB master;
full pin/PHY checks remain on device preparation, QUERY and CONFIRM. Shared
composition requires this trusted callback before binding and preserves its
identity/context independently of any hardware observation; an absent callback
rejects without a full-prepare fallback. This is not a cached device-ready flag.
Tests check 38 fresh supplier reads including both complete clock vectors,
zero pin/PHY accesses or writes, unchanged generation, and no further I/O after
failure. Every clock and internal-master fault still rejects. Every changed pin
can leave internal containment valid, but must fail the subsequent full device
observation and retire the source before it can regain any permission.

### Paired Tenfoot board-test controller

`paired_tenfoot_trace.PairedTenfootClient` joins the selected native App's actual
`display=fpga-menu` startup, CPU startup checkpoints, source-bound menu frames and
returns, configuration queue/activation records, core fetch activity and final
`state=parked` exit. The inner App state remains its existing serial telemetry
schema. It is not rewritten to claim HDMI observation. The existing core-only
`tenfoot_trace.TenfootClient` continues to require its separate serial-only recipe.

Both collectors parse `FES_NATIVE_INPUT_ACTION` as typed input diagnostics,
separate from firmware mutation evidence. Each issued launch/stop requires its
single UART Select/Stop record; Quit requires Stop followed by Quit under the
returned generation. Source, state, owner, generation, order and counts must
match the issued operation. Startup or unsolicited USB actions reject, and a
paired Quit cannot precede verified menu restoration. Every existing boot,
configuration, fetch, USB pause/resume, reader/master drain and return receipt
remains mandatory. The diagnostic records supply none of those permissions or
receipts. Historical captures are preserved without adding missing records.

The paired client can explicitly observe one physical Stop with
`key('stop', input_source='usb-boot-keyboard')` (or the selected `usb-gamepad`).
This exchanges an empty payload, so it never substitutes a UART Stop. The
caller first waits within its separate human-action bound for that selected
USB action, then immediately begins the ordinary close/menu deadlines. Exact
source, owner, generation, ordering, configuration, drain and USB-return checks
remain required. A UART record cannot satisfy this physical observation, and
the default UART controller still rejects unsolicited USB actions. Source
selection is diagnostic context and supplies no permission to touch hardware.

The producer/collector regression exports the actual Go observer through a
test-only overlay with injected service status and zero firmware calls. Its
checked-in bytes are explicitly host data. Current synthetic App models use
that formatter with their own generation, and tests run both real collectors
through the full launch/return cycle and missing/malformed/reordered input and
firmware failures. This does not establish board input or core acceptance.

The caller must independently verify current computer and menu source seals,
selected packages, matched boot artifacts and the board claim before constructing
the client. `menu_policy` is only a pure check of the complete compiled companion,
original computer anchor and explicit 8 MiB union mapping. It cannot grant a
larger mapping to an ordinary computer-only build or authenticate stale sources.
Both role inventories, configuration descriptors and map digests stay distinct.

The injectable `exchange(payload, seconds, done)` sends each key once, preserves
the complete ordered UART capture and repeatedly calls `done` as bytes arrive.
Polling does not consume stateful menu evidence. The `owns` and monotonic `now`
hooks must remain valid throughout. Any missing, duplicate, reordered, malformed,
failed or uncertain transition permanently retires the controller; it sends no
recovery or retry command. All firmware and native rows must be accounted for.
Native presentation emits the App state before presenting its corresponding
frame. Startup, focus and return therefore require a menu frame after the Ready
state. A cached earlier frame cannot satisfy this gate. `ready_frame` is the
repeatable framing predicate for the startup capture; the constructor then
commits and verifies the complete startup evidence. During key exchanges the
controller continues reading through the corresponding completed frame.

`exercise` selects two freshly checked computers in order 0 → 1 → 0, verifies
menu drain before each launch, core close before each menu reload, then drains
the final menu and requires logical Parked generation 7. Configuration generations
come from verified preceding returns, including the menu transfers, rather than
the core-only generation formula. Shared keyboard/gamepad input additionally
requires its original boot/pin proof and input lease, pause before mutation,
resume after the complete verified core firmware opening/menu restoration and
STOP after final menu drain. The read-only fetch diagnostic can be printed
before or after Resume; it does not extend firmware's paused interval. Resume
before any remaining opening firmware or after completed App publication still
rejects. The unchanged captured launch regression preserves the original byte
stream and partial receive/deadline offers, alongside ownership/order mutations.

Shared transition and Stop records also require successful poll-call counters.
Polls must advance before every mutation and stay unchanged across its pause;
report counts remain monotonic, cannot exceed polls and cannot change during
pause. An explicitly selected keyboard can legitimately have no new reports
after `SET_IDLE(0)`, so its poll evidence replaces a demand for key activity.
The gamepad report-progress requirement remains. Neither count demonstrates a
physical button action. Missing/stale counters or lost ownership retire the
collector without retry; matching new native output is required.

Launch retains the original 100-second request budget and every configuration
retains its 20-second firmware counter bound. Return allows 5 seconds for core
close plus 100 seconds for the independent menu restore; final quit adds 5 seconds
for menu park. The predicate enforces the separate phase deadlines, including
when the transport has only partial lines. A transport must poll promptly; late
delivery of earlier evidence conservatively fails. Existing native driver
timeouts, memory reservations and core-only bounds are unchanged.

The returned summary remains controller evidence: physical display, real input
controls, ordinary admission and hardware acceptance are false. A newly matched
paired boot and a bounded TRWU8CBL trial remain separate required evidence.
