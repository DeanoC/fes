# Native contained-fabric endpoint

`NewComputerHPSContextVerifier` explicitly selects the source-bound
`de25-native-computer-v1` / `fes.computer` 1.0 boot contract. It requires the
complete compiled control, computer and held-ROM maps. The default and ROM
diagnostic constructors reject this profile; the computer constructor rejects
diagnostic sources. `ProgramLoader` requires both the independent shared
profile/ABI policy and the native driver's fixed video, HID, ZX81 tape and ROM
inventory before containing SD. Neither inventory nor hardware addresses come
from a selection. A changed live secure boot context after containment retires
the service before reservation/configuration. Existing owned firmware prepare,
master-stop and storage-return checks remain mandatory on every cycle.

The source and build tools carry this profile through native entry and the
cold firmware's existing held-ROM path. Selecting its verified producer project
requires the selected-program/computer/ROM options; the builder links an explicit
native-computer selection into `fpgaload`. This is implementation and off-board
evidence only. Timing, physical video/input and the matched board boot remain
separate acceptance gates.

`NewROMHPSContextVerifier` explicitly selects the held-ROM diagnostic contract.
It validates complete compiled control, computer and ROM maps without I/O; the
ordinary verifier rejects that profile. Its `ProgramLoader` accepts only a
format-three Nano package with one retained 4/8 KiB firmware source and the exact
private activation receipt. Selected metadata binds those resources and receipt
to the package/build. It cannot change compiled addresses, stop policy or master
inventory. A content rejection leaves SD usable before containment.

The program arena privately copies the ROM after the aligned FPGA payload and
before its metadata page. The SDM transfer still exposes and sends only the
unchanged FPGA bytes. Publication uses the existing version-two ROM extension;
ordering, copy, descriptor or firmware uncertainty retires the service and keeps
its mapping and SD scope retained. Stale callbacks cannot publish into a later
lease. Catalog entries may carry one `sources` record (`id`, `role: firmware`,
`path`, `sha256`) and `rom_receipt_sha256`; these remain content selections,
with no hardware authority. The status receipt reports selected content only.
The running native identity carries that same receipt in
`ActivationReceiptSHA256`. Serial status and the Tenfoot observer require the
receipt to agree; return clears it. The existing session API publishes the ROM's
ID, firmware role, source size and digest from the private retained receipt.
Two firmware choices may therefore share an unchanged FPGA package and still
have distinct activation identities. No receipt is a live ROM-completion proof.

Host tests run three 8/4/8 KiB launch/return cycles through the real store,
linker, loader, arena and service. Every publication is parsed by the actual C
decoder against independent modeled secure context. Failure tests cover foreign
resources/receipts, stale ownership, staging/publication corruption and a modeled
firmware rejection. They do not execute physical SMCs or the ROM FIFO. Native
command/cross-builder selection requires the sealed ROM profile and an explicit
firmware source. A newly built sealed producer package, the matched boot chain
and hardware ROM acceptance remain open.

With a freshly built ROM SDK, set `FES_NANO_ROM_BUILD` to its absolute directory
and run `go test -race -run TestBuiltROMSelectionPreflightsWithoutFirmwareIO
./nativefabric`. This opt-in check reads the actual exported maps, package,
firmware, metadata and catalog through the retained store/loader with a modeled
active SD lease. Any firmware call fails the test. It proves artifact agreement
before containment, without granting boot or hardware acceptance.
`FES_NANO_ROM_CATALOG` can additionally select a private directory containing
the actual `FES/` upload files, so two firmware choices and their distinct
receipts are checked against the same compiled SDK resources.
For the standard package, use `FES_NANO_COMPUTER_BUILD` instead of
`FES_NANO_ROM_BUILD`; the test then requires the explicit computer constructor
and normal ABI/driver admission. Set only one artifact variable. This check
never opens firmware or contains the modeled SD lease.

To exercise two different standard packages, set
`FES_NANO_DISTINCT_COMPUTER_BUILD` to a native computer SDK directory and
`FES_NANO_DISTINCT_ROM_CATALOG` to a private catalog containing `FES/` files,
then run `go test -race -count=1 -run TestBuiltDistinctROMPrograms ./nativefabric`.
These opt-in artifact tests require genuinely different ZX81/ZX80 package,
build, core and FPGA payload identities, with their actual 8/4 KiB ROM sources.
Both must pass the same SDK's compiled platform and ordinary ABI/driver checks.
The real store, loader, backend, staging arena and transfer implementation run
ZX81 → ZX80 → ZX81, including stale-session rejection and owned storage return.
Each published descriptor passes the actual C decoder. Additional cases reject
crossed metadata/receipts and damaged archive/ROM bytes before containment;
modeled transfer, identity and return failures retire without replay or storage
resumption. Secure calls, register responses and SD ownership are simulated;
this is artifact/lifecycle integration evidence, not physical programming,
video, drain or timing qualification.

The shared-gamepad input loop now emits `FES_NATIVE_CORE_GAMEPAD` on the serial
platform after a changed key matrix has completed its owned write. Receipts
bind the boot owner, play generation, package/build, descriptor hash, raw report,
normalized gamepad state, copied key mapping and previous/new rows. Each binding
starts its sequence at one. Held launch controls, the initial release fence,
duplicate/idle rows and failed or partial writes emit no receipt. An output
failure stops polling and attempts the existing bounded park of that exact play;
it cannot replay input or resume SD. The ordinary lifecycle status is unchanged.

`nano/gamepad_delivery_trace.py` checks the reported mapping and a continuous
press/release chain from neutral. It explicitly does not establish physical USB
provenance, independently decode a HID descriptor, or prove that emulated
software consumed the keys. The existing automated three-cycle operator rejects
these new markers until an explicit physical-input procedure is supplied.
Physical button delivery still needs a matched native build and bounded test.

`ProgramLoader` separates the immutable `HPSContextVerifier` boot anchor from
each retained program. Its board-compiled platform record binds the device,
HPS I/O/PFG identity, complete HPS/EMIF parameter fingerprint, bootloader handoff
and complete master inventory. Selected metadata must match that record and
the archive's package/build/target/ABI. Control addresses, stop policy and
computer capabilities remain fixed by compiled maps. Required unsupported
interfaces and HPS masters reject before SD containment. Metadata is an unsigned
compatibility assertion; it does not establish live boot facts or authentication.
The version-two platform record uses `hps_configuration_sha256` for every byte
read by the pinned Agilex 5 boot consumers through offset `0x658`: pinmux,
clocks, peripheral power and the SPL's five SDRAM words. This replaces the
version-one whole-file compatibility field; version-one records reject.
Quartus can emit different text after that boundary for otherwise identical
boot configurations. Complete compiler-receipt and HPS I/O file hashes remain
exact and separate from compatibility. No consumed word is normalized or
omitted, and unknown section headers or truncated configurations reject.
The interface check now uses `runtimeadmission.DriverSupport` with an immutable
inventory constructed by the loader from its compiled personality. Control-only
loads cannot require ROM, video or input interfaces absent from that inventory.
This diagnostic check remains distinct from `RuntimeCompatibility`, which also
requires the normal FES ABI semantics and an independent profile registry.

Preflight derives whole/chunk digests from the selected archive, rather than
requiring its package ID, build ID, core ID or payload in the boot image.
The resulting single-use plan carries that private program into configuration,
live computer identity, opening and failure retirement. Boot ownership and SD
return are always checked against the original verifier. A completed transfer
cannot be opened twice or through another loader. This supplies no new data
release or ordinary physical programming profile.

The opt-in `fpgaload --selected-programs --computer-control --computer-reload
--computer-storage-return` frontend now runs a persistent `ProgramService`.
It reads the bounded catalogue from `FES/CORESEL.JSN` and waits for requests;
it no longer automatically executes a two-choice script. Each launch freshly
retains its selected archive and at-most-16-KiB metadata, preflights them while
SD is usable, contains SD, configures, verifies live identity and issues Run.
Return performs one owned Hold/closure and resumes SD before publishing Ready.
There is no compiled package catalogue or fixed session count.

The service is the sole lifecycle owner of its verifier. Launch requests need
the current Ready generation; control/return requests need that service's private
running-session handle. Copies share state, and stale, foreign or concurrent
requests perform no I/O. Content rejection leaves healthy storage usable. Once
containment begins, uncertain transfer/control/return/grant failures retain the
plan, arena and scope and permanently require reboot. Configuration retains its
20-second bound; computer operations keep their one-second/64-poll limits.
Each transfer owns a private 32 KiB hash scratch buffer. Every existing whole
and pending-chunk check freshly copies all covered DMA bytes into that buffer
before hashing; source bytes are checked independently. No prior arena hash
or scratch contents substitute for a fresh read, and firmware digest checks
remain unchanged.
Accepted return cleanup uses its own five-second context even if the requester
disconnects. Hooks must return within their platform bounds.

Startup uses only the store's cached active receipt for the application's owner
token. The real SiP denies boot-context reads during the SD device lease; live
source verification still occurs after SD containment before allocation/START.
A cached status or unsigned package attachment cannot grant boot authority.

`ProgramConsole` provides a bounded serial adapter for this contained profile.
It accepts one JSON object per line (at most 1,024 bytes), waits for its reply,
and emits `FES_NATIVE_PROGRAM_REPLY` with scope
`nano-contained-program-service-v1`. Copy `owner` and `generation` from the
current reply. Operations are:

- `{"op":"status"}`: cached state and selection indices, with no hardware I/O.
- `{"op":"launch","owner":"<16 lowercase hex digits>","generation":0,"program":0}`:
  load one catalogue index from the current Ready generation.
- `return`, `run`, `hold` and `activity`: supply `op`, the same boot `owner` and
  current running `generation`.
- `keyboard`: additionally supply exactly nine `rows` containing HID words.
- `park`: terminal return from a running session for bounded hardware handoff;
  it leaves SD contained and refuses later activation.

Unknown/duplicate/missing fields, arbitrary paths/addresses, invalid indices,
oversized input and stale boot/session tokens reject before I/O. UART overflow
discards the entire line. `Park` is for the diagnostic handoff; the usual Return
keeps the native service available for another selection. The cross-builder
exports example `PROGRAM.JSN` and `CORESEL.JSN` files for the sealed package.
Changing an eligible selection requires no per-core firmware/native rebuild.
The [matched service hardware test](../../../../docs/validation/2026-11-03-de25-service-hardware/README.md)
now passes three request-driven load/Run/Hold/return sessions, stale-request
rejection and final Park. All three use the same sealed package; different
physical payloads and ordinary runtime admission remain open.

Host tests execute three different canonical packages, builds, core IDs and
payload sizes through retained selection, publication, transfer, Run, owned
return, SD resume and fresh selection. They cover incompatible metadata and
source/publication/write/completion/open/identity/Hold/close failures. An explicit
sealed-project test also consumes the actual producer export and retained FPGA
package. These are software results. The
[per-load firmware hardware test](../../../../docs/validation/2026-11-03-de25-program-binding/README.md)
uses one fixed diagnostic package twice. The later
[SD-selected frontend test](../../../../docs/validation/2026-11-03-de25-selected-upload/README.md)
passes four Ethernet uploads and fresh readbacks followed by two native load/Run/return
cycles with fresh SD selection between them. Both physical choices use that same
package. The later [distinct-package board test](../../../../docs/validation/2026-11-03-de25-distinct-hardware/README.md)
passes ZX81 → ZX80 → ZX81 against the same compiled platform, with six verified
uploads and fresh SD retention between sessions. The current compiled contract still names the
diagnostic ABI/profile. Normal Nano admission, ROM linking and native Tenfoot
remain open.

The separate `HPSContextVerifier.OpenComputer` path connects the native
[`computer` driver](../computer/README.md) to a source-derived 32-byte Avalon
window and the existing owned bridge service. It requires a completed
compiled-source transfer and permits only a control-only opening. Every register operation
checks the same secure owner/generation before and after physical access.
This includes the read-only version-1.1 fetch count/address pair. Ownership
loss during an observation retires it; a retained client cannot observe after
Close. The counter adds no bridge grant or data access.
`ComputerSession.Close` requests Hold once through a usable driver, retires
register access, verifies complete master-stop/bridge reclosure and rechecks the
secure source context. Failed commands and unknown closure are not replayed.
Its Hold receipt is command acceptance, not independent proof of CPU stop.
The matched firmware must explicitly enable `fpgaload --computer-control`;
ordinary cold images reject its bridge opening. This diagnostic path grants
no data release or ordinary runtime admission and needs physical qualification.
Each completed transfer permits one opening. A later opening requires both a
new completed rearm under its exact closed generation and the preceding native
session's successful Hold/containment/source-verified return. Failed openings,
failed returns, generic transfers and consumed completion bindings cannot grant
another opening. The native `fpgaload` caller supplies its actual completed
transfer; retaining an old client or forging a public observation grants no access.

This experimental Go endpoint connects the existing `fabricmodel.Controller`
to injected firmware calls, the generated FPGA identity/drain registers and
platform-owned reservations. The optional `modelbackend.NewWithEndpoint`
constructor drives it through the same actor and retained-package admission
used by tenfoot's model sessions. It remains restricted to the
`qemu-model-only` policy. The Nano has no registered physical programming
profile or automatically enabled board binding.

The separate cold DDR diagnostic accepts only the matching DDR control ABI and
sealed 8 KiB reader map. `ReserveReaderMemory` brackets the fixed pattern copy
with fresh closed ownership checks and retains any returned region on failure.
The board hook keeps its entire 2 MiB uncached arena until reboot; no completion
reclaims it. The descriptor explicitly leaves FPGA physical addressing
unqualified. It grants no bridge release, translation setup or runtime admission.
The separate fixed firmware read now has a
[current-payload physical result](../../../../docs/validation/2026-11-03-de25-computer-ddr/README.md):
one 32-byte read, forced reader drain, complete master stop and bridge reclosure.
That result grants no wider window access, buffer reclamation or ordinary
runtime admission.

The menu personality has its own `NewMenuHPSContextVerifier` and private
`de25-embedded-hps-first-menu-prototype-v1` / control ABI `0x10203043` contract.
It requires complete canonical control, memory and read-access maps before I/O;
the earlier constructors cannot admit it. `ReserveMenuMemory` retains an ordered,
zeroed, uncached 8 MiB arena at `0x95000000`, with two 4 MiB XRGB slots, under
unchanged closed secure ownership. The board allocator consumes one reservation
attempt and never releases it, including on failure. A separate private expected
image checks all frame bytes, unused slot bytes and trailing padding.

`OpenMenuSession` accepts only this verifier's fresh completed compiled-source
transfer and reservation. It opens control and DDR through the existing owned
firmware bridge service, then connects `scanout.MenuPresenter` to private,
ownership-bracketed CSR and spare-slot memory hooks. It retains one exclusive
session; copied, foreign, partial, stale and concurrent requests cannot gain I/O.
`Close` first attempts local video/endpoint stop, then independently requests
firmware all-master stop and exact bridge closure. Successful return also checks
source identity, drain coverage and every arena byte. Cancellation cannot prevent
known containment; uncertainty retires without replay or a successful return
receipt. Neither return nor failure frees the arena.

The C firmware selects that exact ABI/8 MiB pair explicitly and reads the menu's
maintenance hold at `0xcc`. Tests exercise native orchestration with injected
secure calls, and the actual C stop helper against the CSR/video/DDR RTL model.
These are host checks. The sealed producer, matched boot-builder selection,
storage resume/reconfiguration and physical clock/HDMI integration do not yet
select this menu personality; this is not a registered ordinary menu package.

The window personality has a separate constructor,
`NewWindowHPSContextVerifier`, which requires its compiled control and 4 MiB
memory maps before any firmware call. `ReserveWindowMemory` publishes a complete
1280×720 XRGB frame and zeroed trailing padding under fresh closed ownership.
The board retains the entire uncached arena through every failure and until
reboot. Normal fixed-reader construction continues to reject this personality.
The native `fpgaload --window-memory` image renders one actual tenfoot frame
before publication. Its entry and cold firmware bind the same complete map;
the stop helper checks the window ABI's maintenance hold at `0xc8` and drains
both FPGA masters. This establishes no DDR release, reuse or HDMI permission.
Native source tests and cross-builds remain separate from physical acceptance.

Adding `--window-read` selects a single owned control/DDR/return cycle.
The completed compiled-source transfer and immutable 4 MiB publication must
share the same closed owner and generation. Firmware checks the idle sealed
master inventory, adapter attributes, live translation controls and coverage
of the whole window before opening DDR alone. The native `WindowReader`
checks full-frame delivery and CRC through ownership-bracketed CSR accesses.
On success, cancellation or a known reader failure, firmware stops both FPGA
masters and drains the active DDR path before closing it with control.
An uncertain bridge mutation is consumed without replay. The allocation
remains pinned on every outcome; this one-frame receipt permits no buffer
reuse, HDMI or ordinary runtime admission.

`OpenWindowSession` also provides an owned `Present` implementation for frame
updates within that same admitted scope. The platform must supply the pinned
region's bounded `OrderFrameStores` hook before opening. Each update verifies
the previous complete arena, obtains the reader's real DISABLE acknowledgement,
copies at most four rows per chunk, and orders the stores. Expected hashes and
CRC cover the submitted XRGB bytes; complete readback also checks unchanged
padding. Each arena digest still reads every byte, now through a private 32 KiB
scratch buffer so the SHA message schedule works in cached CPU memory. No prior
arena contents or checksum substitute for a fresh full read. Ownership checks
bracket each copy chunk and publication. The next frame
uses a newer reader generation and a globally increasing drain cookie, then
returns only after frame delivery and its own local drain. `WindowPresentation`
records those observations; the allocation remains owned until reboot.

Only this fixed-source reader may reuse those bytes while the other sealed
masters remain contained. `Close` independently performs firmware master stop,
hardware drain and bridge closure once. Invalid frames reject before mutation;
uncertain writes, barriers, frame reads, drains or ownership retire the session.
Copied handles cannot take ownership. Concurrent presentation and Close reject
without I/O, and repeated Close returns its cached result. No failure frees the
region or supplies a successful presentation/return receipt.

The explicit `fpgaload --window-storage-return` option adds SD handback after
`--window-present`. The completed transfer retains its original private storage
handoff. `WindowSession.ResumeStorageAfterReturn` consumes only that session's
successful close, rechecks the entire pinned arena and live closed source, then
resumes its original store. A failed or uncertain check/grant is not retried.
The frontend freshly retains the selected package, contains SD again and reports
both return and new storage generations. The old handoff becomes invalid.

Matched firmware enables a separate cached window-return policy. It requires
its own completed configuration, full reader/probe stop, exact F2SDRAM0 drain,
source-bound identity closure and current SD live scope. Ordinary held-data
return checks remain separate. Build, entry, cold firmware and composition
receipts carry the opt-in; absent/mixed selections reject. The separate host
trace decoder requires the entire frame return before SD begin and a fresh
containment receipt. The
[exact-source board diagnostic](../../../../docs/validation/2026-11-04-de25-window-storage-return/README.md)
passes both phases with one attempt and no retries. Repeated physical window
launches, ordinary admission and the normal UI loop remain open.

`WindowSession.NewStoredReconfigurationTransfer` prototypes the next launch.
It consumes only its own successful close and completed SD resumption. The
fresh plan and handoff must belong to the original store and carry that exact
return into the new secure generation. The whole pinned frame arena is checked
before reservation and again before START. A successful new configuration may
open one new window session over the still-pinned arena; old sessions and
transfer copies cannot replay the opening. The actual C/RTL and native tests
exercise three cycles and failures. No new allocation release, normal runtime
admission or repeated-launch hardware acceptance follows from these tests.

Host tests cover twelve changing frames, independent pixel/CRC/hash checks,
publication and ownership failures, cancellation, token exhaustion and concurrent
return. Actual RTL tests cover local drain, held addresses/responses and an
accepted DISABLE whose response is lost. The real C/RTL stop test completes
eight generations and drains an interrupted ninth with a fresh final cookie.
These are software results. The [matched repeated-frame hardware diagnostic](../../../../docs/validation/2026-11-04-de25-buffered-window/README.md)
now completes twelve changing native frames, matching local drains and final
secure master stop/bridge closure on TRWU8CBL. Its 45-second transfer/read/return
bound remains unchanged. The prior [nine-frame stopped trace](../../../../docs/validation/2026-11-04-de25-repeated-trace/README.md)
remains failed and retained. The new result qualifies bounded updates under
this fixed reader scope. HDMI, USB hub input, native networking and ordinary
package admission remain open.

Before an injected actor retires its current owner, it checks that the retained
package's exact target, programming profile and ABI version have an explicitly
supplied identity profile. The profile gives the FPGA control ABI personality;
there is no inferred mapping from a GP ABI tag. It also checks the admitted
programmed-byte digest. The endpoint decodes all 32 lowercase hexadecimal build
ID digits into the control registers' low-word-first order. Unmapped targets,
malformed IDs and digest mismatches reject without disabling the old reader.
The experimental HPS-first diagnostic profile also requires an injected trusted
boot-handoff verifier. It checks the exact package, programmed digest, phase-one
hash, HPS I/O hash and PFG identity before retiring the old owner and again
before firmware START. A changed or unavailable handoff fails closed; after a
reservation is staged, an uncertain failure keeps it owned. The supplied
handoff in tests is simulated, and the unsigned producer sidecar does not prove
which phase-one image actually booted the board.

The controller remains the only lifecycle owner. It supplies the outgoing
reader identity and generation, issues each command once and reserves the
next display generation. The endpoint implements the command phases:

1. Check the contained-firmware capability tuple before disabling the old
   reader, then wait for its matching generation/cookie drain acknowledgement.
2. Require the platform to prove containment of every other relevant master.
3. Reserve DMA-visible storage for the admitted bytes and issue the real TF-A
   full-configuration START/WRITE/COMPLETED_WRITE/ISDONE wire protocol. Up to
   four writes can remain outstanding; accepted BUSY is never replayed. CPU and
   SDM-relative addresses remain distinct. The reserved bytes are released only
   after their completion and successful configuration status.
4. Require control-only bridge access, compare the entire live FPGA build and
   ABI identity, enable the new reader generation, and separately release data
   bridges. A wrong identity permits the controller's bounded idle recovery.

Operations advance cooperatively. The actor limits work per tick and checks the
same monotonic deadline between phases and again after synchronous completion.
A deadline cannot interrupt a hook that does not return. An unknown firmware,
drain, bridge, register, transfer or cancellation outcome enters reboot-required,
prevents replay and retains issued commands and outstanding reservations.
`NewWithEndpoint` callers must retain the endpoint until a physical reboot,
including after constructor or Close errors. A known completed save failure
preserves the old core, with input retired, as in the existing lifecycle.

## Platform responsibilities

`ConfigurationTransfer` is the shared START/WRITE/COMPLETED_WRITE/ISDONE engine
used by this endpoint and the Nano's first contained phase-two loader. It owns
no lifecycle, board policy or bridge operation. Its caller supplies the actual
reserved DMA bytes, qualified transport, DRAM base and chunk size, checks the
monotonic deadline before and after each step, and retains uncertain storage.
Writes fill the pinned firmware's four descriptors until accepted BUSY or EOF.
Completion polls accept up to three exact, ordered relative addresses, then
refill the freed descriptors. Every outstanding source and DMA chunk is checked
before and after calls. A malformed batch advances no completion authority.
Mutation during calls, malformed replies and
cancellation permanently retire the transfer. The final bytes are rechecked
before release, and a failed release cannot be retried.

The source-bound `HPSContextVerifier.PrepareConfiguration` method validates the
compiled control and configuration maps, the staged whole/chunk digests and
fresh closed ownership before publishing one per-load descriptor and issuing
private operation 8. The SMC carries no address or digest. The descriptor contains
the observed owner/generation and boot-context digest, requested package/build
identity, payload size and whole/chunk digests. The secure provider samples its
fixed metadata page twice, retains a secure copy, and independently checks the
actual staged bytes throughout the transfer. It requires no compiled payload
digest table. Preparation and transfer do not implement
`BootHandoffVerifier`, admit a Nano programming profile or activate a core.
The board uses `NewProgramArena` to reserve the last 4 KiB of its 8 MiB mapping
for the fixed 640-byte descriptor. No package chooses that location. Publication
is single-use for an exact active lease, checks live CPU mapping/ordering on
both sides of the copy and retains uncertainty. Missing or changed metadata at
completion cannot release the lease. The firmware response must carry the new
bound-descriptor acknowledgement bit; an old fixed-source response rejects
before START. The changed matched image still needs its own hardware test.

The board's full arena remains pinned until reboot. `ConfigurationArena` permits
a fresh lease only after the preceding transfer has checked every completion,
final bytes and successful status and invoked Release. It never unmaps memory;
outstanding or uncertain leases cannot be overwritten. Stale/duplicate callbacks
cannot release a later lease, and copy/ordering failures retire the arena.
The Nano's ordering hook also checks its live EL1 translation state and mapping
before copying, after copying and before Release. Its transport brackets FPGA
MMIO and configuration firmware calls with identical observations from the
shared [ARM64 mapper](../board/arm64map/README.md). These checks supply no
physical ownership or ordinary runtime permission.

`PrepareReconfiguration` and `NewReconfigurationTransfer` use private operation
11 to request another transfer of that same compiled source after a newer owned
all-master closure. They retain the map, whole-image and chunk checks; the
secure provider separately requires a successful prior transfer. Its next
descriptor may describe different bytes and size within the same fixed platform
envelope, but the current native frontend still selects its compiled package.
Preparation
is deferred until START and checks that ownership still matches the captured
return. Failed transfers retain their region and cannot replay. This is tested
with injected firmware, including changed ownership, malformed acknowledgements
and transfer failures. Joined native tests drive three successive arena leases,
transfers, computer Run/Close cycles and old-client revocation. The matching
strong C fixture drives the second opening/close and uncertain readiness/open/
close paths. The separate
[bounded CPU mapping/two-load observation](../../../../docs/validation/2026-11-03-de25-cpu-mapping/README.md)
passes two configurations and owned Run/return cycles for its exact fixed
package. Arbitrary compatible packages and normal runtime admission remain open.

`ResumeStorageAfterReturn` consumes this verifier's private successful computer
Close, checks the exact live source/closure again, then requests the store's SD
return operation once. A public observation cannot manufacture that authority.
Fresh retention and containment carry the return into
`StoredConfigurationPlan.NewReconfigurationTransfer`, which checks the newer SD
generation against
the current secure snapshot. Failed or uncertain grants cannot replay. Joined
software tests cover three configuration/Run/return cycles, two SD reopens and
fresh selections. The secure provider separately verifies its own closure and
stopped SD provenance before accepting the next activation. This path still
selects the compiled diagnostic package. The
[bounded recovery/warm-SD hardware sequence](../../../../docs/validation/2026-11-03-de25-uart-drain-warm-sd/README.md)
passes two native load/Run/return cycles with fresh SD retention between them.
The [new preflight image's matched hardware sequence](../../../../docs/validation/2026-11-03-de25-stored-preflight/README.md)
also passes. Ordinary Nano admission remains open.

`HPSContextVerifier.ProbeReader` adds a distinct owned firmware operation for
the sealed diagnostic's on-chip reader. It checks the same compiled source and
control map, requires the reader-specific reply tag and verifies same-owner
control reclose. Secure firmware compares the initial live identity, enables
one generation, holds an AXI address across disable and observes both response
beats before accepting the exact generation/cookie ACK. It then compares two
final identity/state snapshots. Read-only identity success cannot substitute
for this result. The real C probe and actual Avalon RTL cover the successful
cycle, early/wrong ACK, partial burst, faults, timeouts and failed I/O without
replaying commands. A second joined fixture uses the actual strong provider,
SD lease, configuration dispatcher and pinned digest against that same Avalon
RTL, including ownership/cache/data-scope loss after a write. This supplies no
DDR/master drain, boot admission, data release or physical launch/return
acceptance. The native `fpgaload` caller now selects this reader operation;
its changed matched boot chain needs separate physical qualification.

`PreflightStoredConfiguration` checks a retained `packagestore.Selection`
before its store is contained. It compares the format-2 archive with the compiled
package/build/target/ABI, payload size, whole SHA-256/SHA-384 and every chunk digest.
It makes no firmware call, filesystem read, reservation or SD stop, so rejected
choices leave storage usable. The resulting `StoredConfigurationPlan` privately
freezes the maps, payload and exact selection. It supplies no hardware authority
or runtime compatibility decision for arbitrary packages.

After `Store.Contain`, `plan.NewTransfer` consumes a current handoff for that
same selection, compares its SD receipt to fresh secure ownership, and only then
reserves DMA memory. The plan is consumed once, including on failure; copied
handles and concurrent callers share that state. It retains a region even when
the reservation callback returns it with an error. Deferred preparation rechecks
the handoff, staged digests and live source before START. Resume or changed
ownership cannot reach START. No filesystem is reopened and no failed
reservation/transfer frees memory. Keep the plan, transfer and platform arena
until reboot on an uncertain outcome.

`plan.NewReconfigurationTransfer` consumes a fresh plan for a retained archive after
this verifier's successful computer return. It requires the matching newer live
closure and unchanged inactive SD lease. The older storage receipt proves only
continued retention; it never substitutes for current FPGA ownership. Deferred
preparation rejects a store resume before START. The native diagnostic selects
one initial load by default; `--computer-control --computer-reload` selects two
fixed-package load/Run/return cycles using the same retained handoff. With
`--computer-storage-return`, the caller reopens SD after its successful return,
freshly retains and preflights a selection, then contains SD before consuming
the next plan. Each
transfer retains its 20-second deadline and each computer probe its five-second
context. Strict framed trace checks require both cycles, fresh firmware
activations/stops and fetch counters, continuous CPU timing and the same owner.
The changed matched boot chain and two-load board trial remain separate gates.

`NewColdConfigurationTransfer` takes the DRAM base and block size from that
compiled map and performs preparation immediately before START, requiring the
original held source. The map permits only positive 64-byte-aligned blocks up
to the pinned engine's `0xffff0` limit. Retirement also checks the transfer
against this map. Neither path takes these parameters from an uploaded package.

Hooks are trusted and serialized. `Resolve` supplies identity only for the
unpackaged idle/software-model fixtures. Admitted packages instead use the
retained descriptor and an explicit `IdentityProfile`; callers must qualify
each association against its actual producer. The test-only DE10 tuple is not
a Nano profile and does not prove that an existing core includes the endpoint.
`Reserve` must provide an exclusively owned, coherent, DMA-visible
region whose mapping and bytes remain valid until its release. Its address and
DRAM base are explicit inputs, not defaults. The platform must prove bridge-mask
readback, clock/reset and DDR readiness, and containment of every other master.
Control-only access must expose the CSR aperture without releasing data masters.
These proofs are not supplied by the experiment or by the capability tuple.
The tuple is an experimental compatibility marker, not firmware authentication.
The mapping and package validation are not a cryptographic signature or a proof
that a particular bitstream physically contains the declared build identity.

`CPUHooks` composes an explicitly supplied absolute CPU control aperture,
data bridge mask and SMC transport with the platform's containment, save and memory services. It
checks aperture alignment, overflow and control-register bounds before an
access. Nano provides an unconfigured AArch64 transport using its ordered MMIO
and existing SMC assembly; the boot path never constructs it. A cross-build does
not qualify an address, and a bus fault or stalled SMC cannot be recovered by
the transport. There is no default base address or bridge service.

The CPU platform services now supply an explicit `BridgeServices` contract in
place of boolean bridge callbacks. Its snapshot carries the qualified bridge
and control masks, the hardware-drain mask, an explicit present/absent fabric
handshake capability, and a boot-scoped exclusive owner with a monotonic change
generation. Unspecified capabilities reject. The platform must qualify these
values against its running firmware/HPS/FPGA build and observe every relevant
change, including firmware operations and reset-then-restore by another caller.
Package metadata cannot assert these platform facts; the existing retained
package, boot-handoff and live build-ID checks remain separate requirements.

`CPUHooks.Contain` sends no shutdown request for an entirely closed snapshot
only if the platform proves a never-enabled boot state with all masters
contained, or the same hooks retain an unchanged completed closure. Unknown
closed or initially partial reset states reject. An active close atomically
compares the supplied prior snapshot, stops every producer, uses the optional
fabric handshake when supported and requires an independent hardware-drain
result. The returned ownership generation, masks and master containment must
match a fresh observation. A known control-only state can be closed again for
the controller's bounded identity recovery. `PreserveDataContainment` is valid
only for that same-owner state: the service closes the control path and returns
`DataRemainedContained`, with zero fresh data/fabric ACK claims. The hooks reject
that claim for an active data path and reject invented fresh ACKs on the held
path. Active data shutdown still requires independent hardware drain.

Control-only opening and data release validate their order, exact masks and
fresh readback. `NewCPUHooks` requires the platform's nonzero data-path mask;
zero, a control bit or a path outside the observed capabilities cannot authorize
a release. The mask is never obtained from package metadata. A platform choosing
DDR-only mask `8` retains reset bits `5` after release and requires a fresh
hardware drain only for bit `8` when closing. Every producer must still stop;
held paths cannot supply invented new drain acknowledgements. Unknown initial
partial reset states reject, including the same reset `5` without owned history.
The firmware adapter sends the explicit mask over the generated 1.8 wire.
The physical cold provider still denies generic data release; these hooks do
not qualify a DDR mapping or provide the missing master-stop binding.
Configuration SMCs require an owned fully closed state before
each call. Any bridge failure or unexpected state/ownership change retires the
CPU hooks; later calls cannot replay the transition. This is a platform
containment receipt under the existing controller, not another lifecycle owner
or a new production compatibility registry.

The [contract validation](../../../../docs/validation/2026-10-31-de25-bridge-contract/README.md)
exercises cold/closed, active, launch/return and failure paths without hardware.
`FirmwareBridges` implements the contract over an injected synchronous SMC
transport using the generated [owned-channel wire contract](../../../../sources/mister-packages/docs/embedded-bridges.md).
Its constructor takes a read-only snapshot on the private call number, validates
the reply tag, capabilities and ownership, and rejects an unknown service or
`ErrBridgeNotReady`. It never probes the older mutable bridge call, initializes
ownership or supplies boot proof. The existing `CPUHooks` still validates the
meaning of each closure and fresh state before advancing the controller.

The [controlled C Nano service](../../firmware/README.md#controlled-nano-bridge-service)
now has an opt-in real SiP binding. Host tests exercise its actual dispatcher,
prepared TF-A engine, vendor bridge functions and tenfoot actor through this
same Go adapter. Trusted initialization, registers, ownership and SDM remain
injected. The controlled BL31 setup leaves the service unready, and U-Boot
packaging rejects it. There is no automatically enabled physical binding. The
default firmware's bridge polls and native boot gate retain their strict
behavior; the physical stage-3 timeout remains unqualified. A runnable board
adapter still needs qualified boot history, handshake selection, hardware drain
and complete ownership/master containment observation.

The FPGA endpoint covers one AXI read port; it is not a complete scanout engine
or an address firewall. Native input neutralization, saves, frame handoff,
real memory/cache ownership, full firmware boot and FPGA timing still require
board integration and qualification. The existing Nano probe remains read-only.

## Repeatable host check

```sh
python3 platform/embedded/tamago/nativefabric/verify.py \
  --reference out/reference/socfpga_sip_svc.c \
  --output out/native-fabric-check
```

Omitting `--reference` fetches and checks the exact pinned public TF-A source
blob. The verifier rebuilds its actual configuration functions and the actual
Verilator RTL, generates synthetic package/ROM fixtures through the existing
runtime compatibility oracle and linker, and runs the native endpoint and
actual tenfoot model actor with the race detector. Its platform transport and
SDM/bridge operations are simulated; it does not execute an EL3 SMC or touch a
board. The six small `nativefabriccheck` cases also run in the TamaGo `fabric`
QEMU diagnostic using software hooks.

`python3 platform/embedded/tamago/nativefabric/verify_producer.py` builds the
standalone simulation producer with two ABI values, checks record/artifact and
live-ID mismatches, and tests its produced model through this Go adapter. The
producer emits a host executable and diagnostic descriptor, not an FPGA image or
a FES package. Its verification executes that local host executable.

The DE25 board-control overlay has a separate Avalon wrapper and a bounded
on-chip responder. Its standalone verifier drives that actual wrapper through
this adapter and tests a mismatched identity, enable, disable and drain. A
generated-project run normally uses the board producer's diagnostic fingerprint.
The opt-in sealed diagnostic builds a canonical ID into both FPGA endpoints
and packages a full SoC RBF. Its separate verifier passes those retained bytes
and exact package/build IDs through the native CPU hooks, prepared TF-A C and
RTL in host simulation, including mismatch and interrupted-transfer cases.
It supplies no physical CPU mapping, DDR containment or physical admission.
The HPS-first variant packages the FPGA core half, with a separately hashed
phase-one half and the matched HPS I/O hash. No board has booted that pair.


`HPSContextVerifier.RetireConfiguration` permits one diagnostic withdrawal of
a failed cold transfer with possible outstanding buffers. It compares the
original source and a fresh held-bridge snapshot, then requests retirement
through the generated owned firmware channel. It never replays a transfer or
releases memory. Acknowledged retirement and its signed diagnostic status
cannot establish completion, drain or runtime admission. The native `fpgaload`
probe uses it after failure while preserving the original panic and 20-second
transfer deadline; the post-retirement status capture has a separate bound.

## Persistent native window ownership

`WindowService` adopts one live `WindowSession` after a complete presentation.
The platform transfers exclusive ownership of that session and its original SD
store; it must retain the service until reboot. Each `WindowHandle` implements
the existing board display interface, while its private generation prevents a
returned or foreign handle from presenting into the next scope. Return and
launch use the existing window/SD/reconfiguration operations and pinned arena.
`WindowConsole` keeps the current handle private and accepts only bounded
status/present/return/launch/park commands for the platform-selected package.

See [native service selection and limits](../README.md#persistent-native-window-requests)
for the explicit build mode. The native request loop uses the real Tenfoot
renderer, but the full App/backend integration and physical acceptance remain
open. The prior bounded diagnostic image supplies no acceptance for this mode.

## Session-bound native keyboard input

`ProgramBackend.BindKeyboard` creates one private local keyboard binding for
an already running play. It uses the existing checked `ProgramService.Keyboard`
operation to neutralize all nine HID rows before handing ownership to the
caller. The binding waits for a complete all-keys-up USB report before sending
presses, so the menu's held launch key cannot enter the newly loaded core.
Copies share decoder state. Return, replacement and confirmed disconnect revoke
the binding; old reports and disconnect callbacks cannot touch another play.

`ProgramKeyboard.Poll` accepts the existing `input.USBReports` interface,
including the DWC2 driver's raw boot-keyboard reports. It drains at most eight
reports, preserves held keys across rollover and no-report responses, and
returns source errors without inventing a disconnect. A confirmed unplug uses
`Disconnect` to neutralize the current play once. Partial native control writes
retire the service through its existing failure path, retain resources and keep
SD contained. The platform must retain the backend and service until reboot.

The host tests exercise full Tenfoot launch, keyboard presses/releases, return
and relaunch through the real Go service and register driver with modeled
firmware/registers. This adapter opens no physical USB controller, grants no
input-ready advertisement and supplies no hub support. The native serial image
still uses serial navigation; attaching this source to a physical platform,
qualifying the PHY/host/hub and verifying playable input remain separate work.

`ProgramInputPlatform` now connects that binding to the actual `tenfoot.Platform`
loop. One source supplies menu commands while Ready and complete core snapshots
while Running. Backend ownership remains locked across classification and report
processing; loading/returning never route reports. Return resets the menu
reader and requires an all-keys-up report before shortcuts resume. This can
require an extra press/release when a device sends reports only on changes;
physical handoff behavior still needs qualification.

A source implementing `input.PollingControl` is drained and paused before each
accepted launch/return and resumed afterward. The Nano owned USB sources expose
that interface through the actual DWC2 driver. Draining discards the current
report and completes any pending packet or hub-health request before the long
transition; it does not reset the device, re-enumerate, change endpoint toggles
or release its secure lease. Loading can then take longer than a packet's
deadline without leaving that packet active. Model-only sources without this
interface keep their existing synchronous routing behavior.

Every paused transition also fences menu input, including a rejected package
whose preflight leaves the same Ready generation. A fresh neutral report is
required afterward. A pause/resume failure blocks later launches; an existing
owned play is parked with an independent bounded cleanup. Failed native
configuration/closure keeps USB drained and reserved. These callbacks grant no
shared firmware capability and select no composed board startup.

A failed source is terminal for this input owner. Future launches are rejected.
If this owner has a live play, a separate five-second cleanup requests the
existing checked Park operation for that exact private session. It holds the
core, stops masters and closes bridges without resuming SD. Closure failure
leaves the service retired and resources retained. A stale cleanup cannot park
a replacement play. The normal underlying platform retains output, pacing and
explicit diagnostic controls; this adapter adds no running-core quit shortcut.

The Nano Tenfoot command attaches this wrapper only when the firmware profile
explicitly selects USB. `OpenKeyboardReports` preserves the existing one-attempt
DWC2 initialization guard; the existing normalized keyboard opener uses the same
entry point. Current controlled-SD boot profiles still disable USB and keep their
existing exclusivity checks. No PHY, VBUS, hub, combined SD/USB permission or
input-ready advertisement is granted by this connection. Host tests run actual
Tenfoot USB-menu launch/core-key/return cycles and source/closure failures through
the modeled firmware transport; they supply no physical USB acceptance.
