# Contained firmware and FPGA control prototype

This is exploratory firmware and native integration for the Linux-free base.
It supplies a pinned TF-A source transformation, a freestanding C++14 control
adapter, and verification against actual FPGA RTL. These tools build firmware
components; the Nano tools assemble the matched experimental boot images.
Software tests and cross-builds remain distinct from hardware acceptance.

The [dispatch-aware repeated-loading trial](../../../docs/validation/2026-11-14-de25-dispatch-rearm-physical/README.md)
programs and boots its candidate, then stops during the first source's SMMU
preparation before SDM START or DATA. Its eight register-write intents do not
identify the failed queue check or callback. On initialization, preparation or
arming failure, the supplier now emits `FES_NANO_SOURCE_SETUP` and
`FES_NANO_SOURCE_SETUP_QUEUE` from the retained probe and nested queue objects.
The records preserve the original setup stage, error, last callback/register
observation, accepted clock history and nested queue error before the outer
service maps a failure to its generic result. They read no registers or timers,
call no ownership/supplier hook, grant no access and never retry. Successful
setup emits neither record. Sanitizer models distinguish several failures at
the same eighth write and execute the observer with every callback poisoned.
These new diagnostics require a new matched image before a future trial;
the older failed image and its hardware evidence remain unchanged.

The [new retained setup trace](../../../docs/validation/2026-11-14-de25-source-setup-physical/README.md)
identifies completion at 79.457 ms and failure of the second final snapshot at
100.672 ms, with preparation still within its original one-second lifetime.
Input preparation now selects its own final-validation policy: all publication
and completion observations retain the 100 ms deadline; both final snapshots
and the final guard remain mandatory under the original one-second preparation
origin. The callbacks still reject an expired parent or lost ownership, and
later arm cannot establish a new origin. Arm-only setup keeps its full-operation
policy. Retirement accepts only a timely completed setup with its selected
finite validation history; a retirement-policy record cannot stand in for it.
These changed firmware bytes require fresh matched-image and hardware checks.

The [preparation-only memory batching change](../../../docs/validation/2026-11-14-de25-private-table-batches/README.md)
targets the subsequent enclosing preparation deadline without widening it.
Only unpublished preparation table callbacks grow to a fixed 1 KiB; every
word/readback and every batch's ownership/time checks remain required. Source,
retirement and control observations keep their separate 32-word batches, and
MMIO/active queue operations stay individually guarded. Cold/warm cost models
and vendor AArch64 objects pass; larger stack frames are recorded separately
from whole-EL3 stack qualification. A new matched image and hardware check remain
required before accepting the changed firmware.

The selected format-14 source-continuation prototype returns to the native
configuration checks only after one successful retirement and a fresh live
engine/source/table/disabled-register check under the original one-second
retirement origin. It consumes continuation once and keeps all buffers. The
existing configuration controller still rechecks the full source and ownership
before completing fabric activation. Failed retirement/continuation admission
parks with the source retained; later configuration checks retain their existing
error path.
`--retain-configuration` selects a native arena that rejects a second program
reservation and initial-tape overwrite after success or failure. The SDK, entry
and cold profile bind that selection; format 14 rejects an unretained arena.
The [format-12 physical trial](../../../docs/validation/2026-11-13-de25-retained-continuation-physical/README.md)
passes retained continuation, then fails the later synchronous FPGA status
reply: the transfer header guard is still configured and correctly rejects
unselected replies. Format 13 adds an explicit `CONFIG_STATUS` transaction.
It requires the actual retained/continued engine, completed native configuration,
the same closed CPU/device/source owner and fresh disabled-register views.
Every retained table word is checked before and after the query. A read-only
mailbox check requires stable empty FIFOs, parser, descriptors and response
bitmap. Exactly one ATF/job-15, six-word successful reply may pass while that
transaction is pending; foreign, duplicate or late DATA replies fail before
payload consumption. Finish consumes the slot; new queries need fresh
admission. Transfer history, sticky failures and retained storage never reset.
The existing bounded synchronous parser and all independent readiness checks
remain in use. No ordinary DATA operation is enabled.

Format 14 adds a source-bound reader handoff. The complete reviewed HPS IP
and generated MPFE wrapper bytes bind both compiled roles to Secure stream 1;
the existing complete vendor adapter binds `ARUSER=0xE0`, `ARPROT=3` and
`ARCACHE=2`. Quartus maps upper AXI user bits to `AxMMUSECSID=1` and
`AxMMUSID=1`; the Non-secure transaction attribute does not change that stream
namespace. With both SMMUEN controls clear, Arm SMMUv3 selects the Secure bank
for that stream. Fresh Secure controls must remain zero while the retired
Non-secure controller keeps `GBPA.ABORT` set. No bypass control is cleared and
no SMMU register or translation table is written by the handoff.

The actual completed transfer, successful separately admitted control query,
retired/continued private engine, retained source/table binding and current
control-only MENU owner must all remain valid. The first preparation and
release stages each check complete retained tables and fresh register views,
with a second Secure/Non-secure observation after the last table scan. Changed
ownership, callbacks, source, phase, data containment or either bank rejects.
The unselected legacy reader gate continues to require its original all-zero
bypass state. The new serial observer requires the exact compiled route digest
and both ordered reader-use records; a bare abort snapshot supplies no grant.

The joined sanitizer model compiles the production readiness caller, mailbox,
reader gate, bank observer and retained-source hooks. It exercises successful
control/query/reader preparation and release, plus ownership, namespace,
register, table and late-change failures. These are injected software checks.
The [bounded matched hardware diagnostic](../../../docs/validation/2026-11-14-de25-retained-secure-reader-physical/README.md)
now passes first MENU handoff, drain and Ready Quit/Park, followed by exact
original-FIT restoration and the full baseline cycle. Memory remains retained
on every outcome. Independent interconnect drain, source reuse and repeatable
retirement-path core loading remain open.

The retirement controller now takes its empty command cursor from the actual
completed setup engine, supplied through the private platform hooks. It pins
that engine and its callback table through retirement and continuation, binds
the owner/epoch/generation, and preserves the queue wrap bit. It rejects an
incomplete, failed, stale or changed setup before using it. Queue and retirement
callbacks also reject changes to their source binding or supplier before the
next operation. The joined cold supplier still admits its existing first-load
profile; this change does not reopen a source reservation or reset any engine.
Sanitizer models execute 384 queue generations and 256 setup/retirement/
continuation generations, with cursor wrap and injected callback failures.
These are software checks for subsequent rearm integration, not repeated core
loads on hardware. The physical format-14 record above applies to its frozen
earlier firmware, not these changed producer bytes.

The selected full-input service also recognizes the private `ReleaseSource`
request before CPU overwrite. The configuration gate requires its actual
completed transfer and a newer closed owner, consumes one source-generation
attempt, and checks the complete controller and fresh bridge scope around the
secure platform callback. An attempted failed release cannot be bypassed by
configuration rearm. The retained supplier checks its actual completed transfer
and parser, both SMMU banks, complete retained tables and private controller
binding, the production window-return predicate and a freshly stopped SD cycle.
It pins the SD service and raw-I/O callback tables. Two complete table passes
bracket read-only empty-parser/FIFO checks; no response is consumed, error
acknowledged, queue reset, table/register written or master opened. Success
consumes the actual ledger and withdraws old reader/control permission before
returning to the native arena. A late response cannot restore that permission.

Release uses a fresh one-second clock under the owned call lock, independent of
the original transfer/retirement clocks. Models cover a 25-hour running interval,
deadline boundaries, source/owner/namespace/SD/parser changes, late mutations
and responses. They run the real transfer and SD engines and production return
predicates with injected retained reader state. The CPU arena separately checks
its old payload, metadata and staged ROM before and after the secure call.

All exported profiles explicitly set `FES_NANO_SDM_SOURCE_RELEASE=0` until the
generation transition and SMMU/SDM rearm are complete. The enabled supplier
currently qualifies the first MENU return; computer-side warm ownership is
still part of that integration. The actual platform and service compile for
AArch64 with the supplier manually enabled, without producing an admitted
profile, linked BL31 or boot image. This is not repeated hardware core loading.

The selected full-input retirement profile uses a bounded, read-only command
snapshot callback. Each queue snapshot checks the same 24-register superset as
the outer retirement engine, with fresh source/owner/callback validation before
and after the batch. Slot stores and producer publication retain their separate
guards and fresh pre-write view. Arm-only and fault profiles use the original
individual register reads. The command, retirement and source deadlines remain
100 ms, one second and 20 seconds. Cost injection and mutation tests are host
models; they do not qualify SMMU retirement or warm fabric drain on the board.

The format-9 input profile also supplies one bounded read-only progress batch
per completion poll. It checks the same 19 fixed capability/error/event/queue
registers, abort, both control values and the producer before sampling a fresh
consumer as the 24th register. Both the batch and its caller require fresh
ownership/time checks before and after; the caller rejects supplier substitution
and validates consumer range, wrap and monotonic progress before accepting it.
Command ownership also rechecks the phase and binding after its register view,
so a late changed phase cannot reach the next write. Slot stores, publication,
all five complete table scans, final snapshots and the original 100 ms/one
second/20 second origins and bounds are unchanged. Arm-only and fault setup
leave the progress callback unselected and retain their individual queue reads.

The previous format-8 hardware trial completes all input bytes, then fails the
command deadline after producer publication. A fresh physical reset is needed
when recovery programming stops with a broken JTAG chain; the original FIT and
full baseline cycle/Park are subsequently restored and independently verified.
Format 9 rejects the older profile contracts and needs a new matched chain and
bounded physical test. Host mutation/cost tests cannot establish hardware
completion, independent drain, ordinary activation or source-memory reuse.

`nano_mac` supplies a portable fixed MAC/window lease engine with an independent
peripheral generation. Trusted scope, grant, confirmation and independent drain
callbacks are mandatory. Failed mutations retain the reservation and retire;
bridge/SD/USB generations are untouched. The native `maccontrol` client has
joined tests through the actual owned SiP dispatcher. The producer installs the
MAC engine and entry guard, and secure startup admits only the accepted boot
owner/epoch at contained generation zero. The default setup remains unbound; no
physical MAC boot supplier is installed and board Ethernet stays disabled. See the [MAC lease contract](../../../sources/mister-packages/docs/embedded-mac.md).

`nano_firmware_mmu.py` is a hardware-independent static EL3 table prototype.
Its `emit` command prepares a private copy of the preprocessed, separate-NOBITS
BL31 linker script. The final linker resolves code/data boundaries and all six
tables inside the existing 2 MiB firmware DDR exclusion; no table consumes
OCRAM. Firmware code is read-only/executable, tables are read-only/execute-never,
and private state is writable/execute-never. Shared DDR remains noncacheable and
non-secure; peripheral windows remain Device memory. The supported DDR extent
is the matched Nano's 1 GiB, rather than the vendor's generic 2 GiB range.
Its `verify` command checks the actual ELF descriptors and section/load storage,
plus the existing complete cold-image RAM and chain checks. This tool does not
enable the MMU or change the current boot selection. The [static-link result](../../../docs/validation/2026-11-08-de25-el3-static-mmu/README.md)
records the linked prototype and remaining runtime and hardware work.

The `nano_mmu` prototype adds a one-attempt enable transition and fresh CPU/MMU
scope checks through `nano_mmu_platform`. It retains the vendor's SCTLR settings,
sets only M/C/WXN, verifies each selected register write, and retires partial or
changed profiles without a cache-off fallback. The explicit
`FES_NANO_FIRMWARE_MMU` selection connects these checks to the existing cold
callbacks and every owned SiP channel. Injected-register tests execute the real
adapter, boot/master ownership checks, shared USB, tape, configuration and return
engines. The matched BL31 and SPL/U-Boot producers now expose
`--nano-firmware-mmu`, requiring the complete selected OCRAM/shared-USB/tape
profile. The source-bound policy installs enablement after the original final
cold cache cleanup. It requires a private 64-byte write/clean/invalidate/read
diagnostic before publishing active state. The boot observer requires both
matching register records before accepting the loader. This bounded diagnostic
does not establish full RAM/cache coherency or hardware transfer performance.
The [matched boot build](../../../docs/validation/2026-11-09-de25-mmu-boot/README.md)
passes BL31, SPL/U-Boot and image composition; its hardware test remains open.
Previously tested board images still use the existing mode.
The first actual selected-MMU boot rejected the vendor helper's leaked SCR.NS
state before any MMU write. The [selected startup repair](../../../docs/validation/2026-11-09-de25-scr-restore/README.md)
pins that helper's source, preserves its invalidations, restores caller SCR and
returns normally. Policy format 2 binds this repair and its exact 44-byte linked
instruction sequence. Its bounded board test passed provider setup with restored
SCR, then stopped after the MMU `before` record. Subsequent register/cache progress
is unknown. The selected format 3 profile adds 26 ordered cold-only checkpoints
around admission, barriers, register writes and the private clean/readback probe.
The observer requires every checkpoint between the original register records;
partial, missing, duplicate or out-of-order steps cannot admit the loader.
These checkpoints add no timed tape or ordinary SMC work. The cold/runtime guard
is unchanged; cache enablement and transfer performance still need hardware proof.
The selected [format 4 transition capture](../../../docs/validation/2026-11-09-de25-mmu-fault-capture/README.md)
now uses a bounded leaf assembly path around SCTLR. It saves the original
runtime vector once in file-backed private DDR and keeps a stack-free fault
vector installed through the first C stack accesses and private memory probe.
A successful boot restores and reads back the original vector before its final
register receipt. Faults snapshot ESR/ELR/FAR/SPSR/SP and MMU registers, print at
most 384 bytes with at most 100000 ready polls per character, then park. A nested
exception uses a separate permanent-park vector. Poll bounds cannot preempt a
bus access that never returns. Neither path calls C or disables the MMU/cache.
The producer binds both new source files and checks actual linked leaf code,
vector routes, CPU/UART constants, bounds, writes/barriers and one-use data.
The observer requires both matching transition records as well as all 26 steps;
a fault remains terminal diagnostic evidence. Injected-platform and actual
assembly/QEMU checks pass; Nano qualification remains open.
The format4 board capture now confirms activation followed by an asynchronous
SError delivered inside `tf_log`. Its FAR is not qualified as a failing address,
and its raw SP is the handler's SP_EL3. The selected format5 diagnostic now also
captures SP_EL0 and takes sixteen fixed read-only CCU/OCRAM/ECC observations
before activation and after a fault. Optional RAS reads cover the current bank
only, after feature/CPU/count/selector checks; no selector or error status is
written. Prefixes identify reads that do not return. Nested faults still park.
The host requires the complete baseline inventory before accepting activation;
post-fault inventories remain diagnostic and never reopen boot authority.
Source and ELF checks bind the reviewed addresses, read widths, stack-free code,
fixed UART bounds and permitted system-register writes. Existing memory/CPU/
owner/tape logic and its one-second deadline remain. Hardware qualification of
the new inventory remains open.
The selected format6 diagnostic adds a separate late-MMU decode-recording
lifetime. It requires zero DSU detection/interrupt/status, enables only
CAIUUEDR.DecErrDetEn (bit3), and records intent in distinct private file-backed
DDR before the potentially faulting write. CPU/MMU and all-master reset/power
checks frame control changes. Normal and terminal cleanup restore control0,
verify readback, unchanged interrupts and preserved sticky status. A new error
remains terminal even if SError delivery is delayed. Interrupted or nested
cleanup never retries. This lifetime spans C execution; the early cache-off
logger remains scoped to its original startup phase. No status, interrupt,
selector, ECC or memory-permission bypass is introduced. Actual linked code
checks admit only the declared fixed control and private-state write sites.
Hardware qualification of this changed image remains open.
The fixed UART addresses are initialized once in scratch registers at each
write/restore/capture entry and remain unchanged within that leaf. Capture and
restore reinitialize them after intervening C code. Poll counters and byte
budgets remain per character/line. This removes repeated inline constants so
the diagnostic can fit the original sealed upload limit; code checks bind each
initializer and the permitted data-store register.
The selected format7 mapping follows the pinned vendor's secure Normal-NC
OCRAM policy after physical decode logging identified an illegal DII transaction
at the cached stack address. Stack/BSS bounds, security and execute permissions
are unchanged. Only private DDR remains write-back cached. The aligned64-byte
cache witness now belongs to initialized private DDR data; actual linked bounds,
zero file backing, separation from transition state and the live adapter's
data/table boundaries are required. A witness outside those bounds retires boot
before the probe and leaves every owned channel unready. Clean/invalidate and
readback remain required, with no ECC change, status acknowledgement, broader
memory permissions or tape-deadline change. Nano qualification remains open.
See the [runtime scope checks](../../../docs/validation/2026-11-09-de25-mmu-runtime/README.md).

`nano_tape` is a secure-side initial-tape engine for the existing
Avalon computer mailbox. The owned TF-A export includes its service; existing
profiles leave the channel unready. Its trusted supplier holds the secure
lifecycle lock with non-secure execution and other CPUs stopped, and all DMA
and data masters contained. Complete fresh ownership observations frame
preparation, each 512-byte chunk and completion; every CSR independently checks
live CPU/reset/access ownership before and after. These are required callbacks,
with no permissive fallback. The explicit `FES_NANO_INITIAL_TAPE` strong supplier
follows the existing secure ROM-step pattern. The explicit native SDK
`--initial-tape` selection now binds that flag through the entry and cold
receipts and selects the Nano CPU hook. It requires the matched firmware OCRAM
contract; the default selection leaves the channel unready.

The engine checks the selected full GP identity and adapter version before and
after, the acknowledged initial Hold, stable transport snapshots, one toggled
write per command, bounded ACKs, chunk ordinals/odd padding, CRC before begin and
commit, final media readiness and unchanged instruction-fetch observations.
It keeps one 512-byte scratch buffer, uses a qualified timer with a fixed
one-second limit per preparation/chunk/final step and a size-derived total
budget, and never issues Hold/Run, reset or recovery. Failure
consumes its frame, retires the object and leaves the receipt untouched; the
caller still owns physical containment. Success returns the new GPO so the
caller's next command can continue the same protocol epoch.

Portable tests execute the C engine with injected MMIO, ownership and time.
The captured-computer RTL verifier also links the actual C object: it compares
all tape bytes and held fetch counts and checks a subsequent native Go Run.
The actual native computer driver now accepts and validates that receipt through
its explicit initial-tape loader, and the native session frames the injected
platform call with live ownership observations. The manual test-only GPO update
is gone. These checks establish protocol/native behavior, not physical timing,
source-memory ownership or a firmware service grant.

`nano_tape_service` now captures the fixed-window descriptor twice, verifies its
source digest and private accepted build, and drives that engine with ordinal-bound
preparation,512-byte chunk and final calls. Every call keeps a one-second bound;
total elapsed time, including gaps, is limited to ceil(bytes/512)+2 seconds.
Start/last timer samples remain monotonic across the service/engine and each
call boundary. Each step revalidates the descriptor/source and closes its frame.
Pending replies expose verified progress only. A final digest and complete
ownership proof precede the receipt; failures and replays expose no success fields. The corresponding
native arena publisher reuses only a completed configuration lease, keeps the
entire mapping pinned and retires uncertain outcomes. Portable tests join its
actual Go descriptor and receipt decoder to this C service and endpoint model.
The [shared contract](../../../sources/mister-packages/docs/embedded-tape.md)
describes the fixed header and call registers. The real Nano dispatcher and
strong supplier now join behind the explicit build flag. Requests are confined
to the active phase1 computer under the existing SiP lock; menu/closed/default
profiles cannot use it. The supplier requires accepted configuration and ROM
completion, the runtime READ phase, inactive SD, confirmed shared USB, exact
CPU/reset/owner/generation and contained masters. Every CSR has fresh live guards;
complete observations frame preparation, each 512-byte chunk and completion. It uses
only the fixed descriptor/payload window and the private accepted build.

Selected failures retire the computer policy and invoke its existing containment
callback. Replays/foreign requests cannot reconstruct the service; only a newer
trusted configuration completion can prepare a healthy new object. Unused tape
objects do not block ordinary no-tape return/reconfiguration. Joined portable
tests execute the actual dispatcher, strong provider, pinned SHA, tape engine,
ROM and master-stop/SD-return path with injected registers. They cover repeated
roles, late source/CPU/reset/USB changes, ACK loss, expiry and five incomplete
compile selections. Physical timing and a freshly matched hardware boot remain unqualified. The
native selection requires the complete held-ROM computer, shared USB, owned SD
return and fixed arena. Mismatched compiled/link selections reject before
firmware access. A selected failure emits one `FES_NANO_TAPE_FAILURE` record
after containment, using only saved engine/frame counters and timer samples;
reporting adds no hardware reads or payload logging to the timed transfer.

For the next tape diagnostic the strong supplier also counts callback invocations
and samples ownership, CSR read/write and barrier costs once per64 calls; frame
ownership, source copies and SHA callbacks sample every call. The qualified timer
supplies those samples inside the unchanged one-second budget. The samples add
overhead and are diagnostic observations, not a timing qualification or a complete
partition of elapsed time. CSR read/write samples cover the callback; the explicit
completion barrier is sampled separately. None of these counters grants access or
changes protocol behavior. After a failed load and its containment attempt, seven
`FES_NANO_TAPE_COST` records and a separate `FES_NANO_TAPE_CONTAINMENT` result precede
the terminal failure marker. Successful loads do not print these records. Reporting
uses cached values and emits only once; an invalid clock sample remains labelled.

The next diagnostic also separates the selected ownership samples into the
first private-state check, shared-USB binding check, paired reset-register reads
and final private-state check. The existing checks and their short-circuit
order remain. These four `FES_NANO_TAPE_OWNERSHIP_COST` records count only the
same once-per64 outer samples; a failed prefix can leave fewer inner samples.
They retain the outer call/sample counts and report only after failure and
containment, before the terminal marker. They add timer overhead within the
original limit and provide no cached permission or hardware timing acceptance.

A containment status of zero records the existing direct control-reset callback's
successful readback: data reset bits were already set and all bridge reset bits
were set afterward. It does not prove fabric acknowledgement, producer drain or a
normal core return. The provider is retired regardless, so a later normal bridge
close can reject even after this reset readback succeeded. The previous hardware
trace lacks the new result and cannot establish that readback retrospectively.

The per-load descriptor now has a version-three mixed-role prototype. A trusted
`fes_nano_program_enable_roles` call, after installing the fixed ROM-extension
reader and before the first capture, selects it instead of fixed version one or
two. Requests cannot enable or downgrade that mode. Its final eight header bytes
follow all nine digest slots and bind menu/control ABI `0x10203043` or the current
held-ROM computer/control ABI `0x10203041`. Both use the same fixed 128-byte
extension slot: all zero for menu, exact 4/8 KiB ROM binding for computer.
Header and extension are sampled twice under the existing ownership guard.
Each capture clears prior role/ROM state; an uncertain capture retires it.
The captured role is metadata for the platform's control policy, not a live
configuration-completion or master-access proof.

The owned SiP dispatcher now has a separate, default-off
`FES_NANO_PROGRAM_ROLES` composition of the real menu and computer session
engines. `fes_nano_program_control_activate` is a secure completion callback,
not a request: it checks the captured role, bytes, owner/generation, complete
configuration state, inactive healthy identity probe and live closed bridges.
It runs before the configuration engine publishes `finished`. Only then does
it change the active role and identity or prepare the appropriate session
engine for a warm opening. Until that callback, a captured target leaves the
previous role and return policy intact. An invalid activation permanently
retires the dispatcher. Raw diagnostic probes cannot bypass selected sessions.

The joined dispatcher test executes the actual owned SiP entry, C descriptor
decoder, configuration engine, both session engines and bridge closure code.
It covers alternating roles in both starting orders, repeated loads of each
role, role-specific closures, completion/open/ROM/stop failures, activation
replay, sixteen invalid activation states and six incomplete compile selections.
Platform ownership/digest, SD-return, ROM and reader I/O are injected. This is
not execution of the mixed-role strong cold provider or a matched board chain.
The cold provider now calls the activation hook in the explicitly selected
mixed-role mode, after installing selected stop/ROM resources and proving the
preceding role's complete return. The explicit native `--menu-project` companion
selection binds both sealed inventories and their union memory through the
SDK/entry/cold profile and SPL. A freshly matched chain and physical test remain
required; the existing single-role selections do not enable mixed firmware.
The firmware exporter includes and hashes the new dispatcher composition.

The mixed cold provider keeps the normal computer boot context/control ABI and
8 KiB reader descriptor, while requiring the complete 8 MiB CPU reservation and
compiled two-slot menu geometry. Active role selects the reader/firewall size,
held acknowledgement register and SD-return predicate. Capture does not change
that role or the preceding stop object. Completion first consumes the old
held-data or window/SD return proof, then constructs a fresh selected stop,
prepares ROM only for a computer (or retires a completed ROM for menu), publishes
the selected identity/dispatcher role and clears stale window/rearm state.
Failed or unfinished ROM state cannot be recycled, and a menu cannot use the
computer ROM ownership callback.

Thirteen strong-provider scenarios execute actual cold boot acceptance, owned
SiP/configuration, SD platform, ROM FIFO driver, SHA-384 and master-stop callbacks
with injected MMIO/mailbox responses. Four five-load sequences exercise changing
roles in both starting orders and repeated menu/computer loads, with distinct
selected build IDs and actual 4 KiB ROM bytes checked. Nine failures cover
invalid role resources/ABI, CPU drift before START, ROM digest mismatch before
FIFO writes, held acknowledgement mismatch, corrupted configuration, bad SD
route/closure and a missing drain acknowledgement. Each refuses further launch
and SD I/O. A smaller CPU arena fails compilation. This is host execution of the
strong callbacks; it is not a matched boot-chain build or physical FPGA execution.

The same strong-provider fixture now also runs with controlled, confirmed shared
USB enabled, through the actual board/PHY, cold-device composition, retained
USB master and owned SiP code. The physical register model supplies the complete
164-pin fixture and a common 200 MHz USB/SD clock vector. USB queries remain
confirmed across active SD, menu DDR, computer control and independent returns;
five changing-role loads end in an owned USB stop. The nineteen shared cases
include the original thirteen plus DMA/IRQ ownership, lost AHB idle, clock drift,
partial USB reset and lost PHY readiness while the menu is active. These faults
retain the USB reservation and refuse further FPGA/SD I/O or replay. The ordinary
thirteen cases and smaller-arena rejection still run separately. This proves
the injected-register composition, not USB enumeration, controller input,
electrical behavior or physical acceptance.

Tests run five changing-role loads through the actual C configuration and
held-bridge engines, plus twenty rejection/retirement cases. Native Go encoding
and private arena publication are paired with the same C decoder, including all
nine chunks and stale publication callbacks. Mixed roles remain default-off.
The native builder/loader and entry/SPL select both matched inventories and the
union memory exclusion only with the explicit companion. The native service now
owns Tenfoot menu presentation and the core/return cycle, with actual App pixel
comparison in host tests. Physical App/core/menu acceptance remains open.

The explicit `--nano-firmware-ocram` option on both Terasic builders selects a
matched shared-ROM, explicitly selected two-slot menu or paired-role cold chain with private
stack/BSS in `[0x50000,0x60000)`. The menu selection validates its complete
8 MiB memory, read/control digests and CPU exclusion; it grants no USB or ROM path.
The version-5 layout contract fixes the stack at `[0x50000,0x58000)` and bounds
the actual ELF BSS/NOBITS end by `0x60000`, with 64-byte alignment. Different
firmware selections may use different BSS sizes. The ELF loader and early/exit
assembly check those actual symbols; cache maintenance still uses the exact
linked BSS range. Older layout metadata rejects and needs a fresh matched chain.
It requires the version-2 native CPU exclusion, the final one-shot SPL live
allocation check and a fifth private entry argument. Before vendor startup,
the allocation-free EL3 guard checks the sealed record/CRC/chain, SP and linker
layout, plus live reset/CPU containment. Failure parks before BSS or stack
initialization. Early stores are limited to an initialized DDR attempt latch
and the fixed UART sink already used by SPL. Entry, pass and rejection-phase
messages have the same 384-byte and 100,000-poll bounds as that sink. No stack
or calls are needed for these messages; unusable UART status parks the entry.
The full cold provider still validates history and live ownership before any
service grant. This is an experimental source-bound handoff, not cryptographic
secure-boot provisioning or ordinary package admission.

The selected assembly constants are compiled against the C producer's actual
headers with static assertions, including `FES_NANO_FABRIC_ABSENT == 1`.
Regression records come from the real cold begin/finish/checksum functions over
injected register callbacks. The initial hand-packed ARM control used the wrong
fabric value and missed that mismatch; its passing results do not establish
compatibility with valid producer records. The stopped image remains unqualified.

After another silent handoff stop, SPL now retains its original consumed
allocation snapshot. Only the rejection path prints those saved values and
parks; it does not resample or retry. EL3 phases distinguish arguments, record
fields, CRC, identity, layout and live containment. The host decoder validates
their order and can require entry/pass before vendor capture for the selected
RAM chain. These diagnostics alone never qualify boot or memory ownership.

The selected cold entry also emits bounded progress around the pinned vendor
reset, CPU-data, cache, BSS, stack and setup calls. It generates private macro
copies and leaves the original header and warm entry unchanged. Removing the
trace statements and private names reproduces the original macro bodies.
The UART expansions preserve the arguments and retained registers without a
stack access. An explicit RAM-trace decoder selection requires all 15 ordered
vendor markers, with setup completion after the two EL3 capture observations,
before accepting the U-Boot checkpoint. Partial progress locates a stop; it
does not grant boot, RAM ownership or a firmware service.

The matched RAM profile also instruments the subsequent cold BL31 path with
31 ordered RUNTIME checkpoints. Source-pinned C transforms insert calls around
owned setup, GIC initialization, mailbox initialization/notification, libraries,
runtime services, next-image preparation and final console setup. Assembly
markers bracket `bl31_main`, the original data clean, qualified BSS clean and the cold
`el3_exit` branch. Removing those insertions exactly reproduces the input
sources after inverting the explicit private BSS specialization; warm entry
and the other original vendor operations remain unchanged.
Mailbox markers enclose the optional V3 initialization as well as notification.

The C-call diagnostic is a bounded UART leaf using the existing fixed sink,
100000-poll and384-byte message limits. It has no stack access, RAM store,
nested call, console-framework dependency or vector/cache operation. Ordinary
C callers retain their normal ABI stack handling. Partial, repeated, reordered,
misplaced or failed runtime progress cannot qualify a loader prompt; the host
requires the full sequence after cache/logging completion, enclosing the
provider/SD observations and preceding U-Boot. These markers locate returned
call boundaries without granting ownership or excluding a later vendor-vector
fault. They do not widen any boot, ROM or operator deadline.

Selected cold startup temporarily uses private exception vectors until stack
selection has returned; it restores the vendor runtime vectors before BL31
setup. On an exception, a stack-free handler switches to park-only vectors
before its first data access, captures ESR/ELR/FAR/SPSR and four working
registers, emits one fixed-width bounded UART line, then parks. A logging fault
therefore cannot re-enter the logger. The decoder retains at most one exception
record and rejects boot. These diagnostics preserve the vendor cache operations;
they neither retry nor establish RAM ownership or writability. The tables use
the vendor vector section, preserving its required BL31 entry ordering.

After that original exception line, a fixed post-fault inventory reads seven
architectural registers and 36 MMIO observations from the retained CS/R1 revB0
RC28 map. The generated code marks each read before issuing it, emits returned
values in order, and performs no status clear, selector write, initialization,
RAM access or retry. CCU status brackets its location reads. The existing
park-only vectors contain a failed diagnostic access.
The host preserves bounded partial progress and requires every pair plus END
for inventory completion. That means reads returned, not that a device is healthy
or that startup/ownership succeeded; the original exception remains terminal.

After the fixed inventory completes, the RAS2 diagnostic requires RAS v1,
the reviewed Cortex-A55 r2p0 MIDR and exactly two error records before reading
the original selector. The selector must be0 or1. It then selects record0 and
record1, observes feature/control/status/MISC0/status and confirms selection
after each record, then restores and verifies the original selector. Each of
the three selector writes has a completed before marker, an ISB and readback.
These are the only system-register writes added by RAS2; detection, interrupt,
status, ADDR/MISC1 and pseudo-fault controls are untouched. The four added CCU
detection/interrupt control observations are reads only.

The decoder retains at most20 actions. Selection changes/read failures remain
terminal, with explicit incomplete and potentially unrestored selector state.
A completed matching restore readback proves restoration even if END is lost;
END requires both records and restoration. Unsupported identity/count/selector
or mismatching readback emits SKIP. A fault before readback parks under the
existing fallback; it cannot promise selector restoration or grant recovery.
Neither END nor SKIP grants startup or ownership. Sticky values may predate the
exception, and a clear error record with detection disabled proves no health.

The selected A55 cold path brackets NOBITS cache maintenance with CCU
decode-error logging only (`CAIUUEDR.DecErrDetEn`, bit3).
It first checks CPU identity, reset/power containment and zero detection,
interrupt and error-status baselines. Enable and disable stores each have
barriers and readbacks; the separate interrupt register remains zero, and
the error-status register is never acknowledged. Restoration checks the
captured status is unchanged. ECC detection controls remain unchanged.

The selected RAM profile replaces that range's address-based PoC
operation with the pinned TF-A `dcsw_op_all(DCISW)` routine. This explicitly
invalidates the accessible data/unified cache hierarchy before initializing
OCRAM; cache-off is not treated as proof that caches are empty. The observed
CCU illegal-DII-access error makes an address-based operation on this range
unsuitable for the diagnostic. Ordinary DDR and warm-entry maintenance are
preserved. No general fallback or cache-operation skip is supplied.

Only the exact private NOBITS base, length and aligned end are admitted, with
fresh reviewed CPU/reset/power containment and MMU/data-cache-off checks before
and after the call. CSSELR must start and finish at0. SPL compilation requires
its data-cache-off configuration. The selected cache helper source is pinned
and captured in the build binding; its geometry handling, register-only call
path and retained-register preservation require linked instruction checks.
Two bounded CACHE receipts bracket the call and subsequent checks. A partial
receipt or exception cannot qualify startup or claim completed maintenance.

Register-held state distinguishes idle, possibly enabled and restoration in
progress. An exception prints the original fault and both existing inventories
before one guarded restoration attempt. A fault during restoration parks
without retry; missing readback leaves restoration unproven. The host requires
the ordered logging cycle before accepting later vendor startup markers.
Neither a completed diagnostic nor restored logging grants boot ownership.
The logging-only predecessor captured the physical CCU error and verified
restoration. The [bounded set/way trial](../../../docs/validation/2026-11-06-de25-setway-trial/README.md)
observed cache/logging and all15 vendor checkpoints return, then timed out
after the SD-platform setup line before the loader prompt. That evidence
belongs to its exact input; the added runtime checkpoints need a new matched
trial. Neither returned call markers nor off-board geometry execution qualify
physical cache coverage or a complete native core-loading cycle.

Version4 places the 256-byte per-CPU array in the sealed BL31 `.data` image in
DDR, with six independent C assertions for count, stride, alignment, size and
affinity-member layout. ELF admission requires aligned, writable, file-backed
zero bytes in the initialized-data interval below `80200000`, disjoint from
other firmware symbols. SPL loads those bytes before secure initialization.
The remaining private BSS shrinks by 256 bytes; no padding or runtime cache
skip is added. The original per-CPU `DC CIVAC` operation remains unchanged.
The matched loader reserves `80000000..80200000` through its device tree and
requires that exact, unique `LMB_NOMAP` reservation before CPU queries.

The cold exit clean specializes the exact compiled private BSS range
`58000..5ddc0`. It retains the original writeback
intent through the pinned `dcsw_op_all(DCCSW)` helper across the accessible
data/unified hierarchy. `DCCSW` comes from the actual vendor header, including
its clean/invalidate alias when `ERRATA_A53_827319` is selected; other selectors
are assembly errors. The DDR data clean and warm-entry operations remain intact.
The ELF admission checks the exact BSS endpoints, and the profile rejects old
versions or a missing exit-maintenance policy.

Before and after the call, the selected code checks the reviewed A55/EL3/
primary-affinity/endianness and MMU/data-cache-off state, DAIF masks, independent
CPU/reset/power and master containment, restored logging flag and CSSELR0.
Only exact compiled range arguments are admitted. Unexpected state parks
without a range fallback or retry. The macro uses no stack, private-data
store, detection/status/interrupt-control write or additional nested helper.
Its source binding and actual linked selector/helper/controls need fresh checks.

The [A55 TRM](https://documentation-service.arm.com/static/5e7e1405b471823cb9de57ae)
sections A6.2.3/A6.2.4 describe cache maintenance while data caching is disabled
and clean/invalidate behavior for set/way operations. Cache-off does not mean
empty caches. The qualification therefore requires the initial hierarchy
invalidation, linked cold control history with no cacheable data allocation,
fresh exclusive ownership and no cacheable aliases. Invalidation is accounted
for even when the vendor selector spells a clean. This source/manual reasoning
does not qualify physical cache coverage on the board revision.

Two bounded EXIT_CACHE markers bracket the operation and subsequent checks,
inside the runtime BSS-clean bracket. The decoder requires ordered completion
before BSS-clean return/exit and U-Boot; partial/faulted/misplaced evidence is
terminal and cannot grant boot. The new exit path still needs a freshly
matched bounded hardware trial. All original boot and ROM limits remain.

Only that selected profile may accept one non-executable, zero-file-byte NOBITS
load segment in the fixed range. Packaging independently checks the actual ELF
symbols, exact stack/BSS boundaries, initialized DDR latch and compiled chain
bytes. Unselected images retain DDR-only admission; an old private linker
prototype does not qualify merely by fitting. CPU/runtime, SDM/boot data and
remaining OCRAM users retain their separate ownership and hardware obligations.

The physical USB supplier has an optional complete containment callback. Paired,
ordered cold samples enclose both fresh bus-clock scans, both DMA/IRQ/PIO checks
and the bounded AHB-idle observations. Each controller read independently checks
that the USB module and packet RAM remain out of reset. Owner, epoch, generation,
private bridge targets and active/confirmed state must remain unchanged. The
shared composition binds this callback's identity at initialization; a missing
or replaced callback cannot substitute another observation after that binding.
Suppliers without it retain the separate preparation/containment path, and a
failed complete callback retires without trying that path. Device readiness
still performs its independent pin/PHY checks. No observation is cached across
calls, and no new wire operation, grant, write or timeout is introduced.

The C board model reduces full cold-register reads per containment observation
from 182 to 26, with both 13-register clock scans retained. It uses four more
physical reset reads to guard all controller accesses. Host fault tests cover
reset drift, DMA/IRQ/clock changes, private-state drift, every clock/PIO/reset
read failure, both cold samples and idle-poll exhaustion. This improves one
nested ownership layer; it does not establish a passing physical ROM transfer.
The ROM driver's original per-access checks and one-second limit remain intact.

`nano_rom.c` transfers ROM images through trusted platform hooks.
It snapshots an admitted 4/8 KiB input before MMIO, checks its expected CRC,
then verifies the endpoint version, image size, sealed build words and held CPU.
Owned, ordered FIFO writes have one qualified 400 MHz timer budget of one second
and at most 64 status polls per beat/completion. Ownership brackets every access;
the final counters, CRC, identity and held completion must all match. It cannot
open a bridge, release the CPU, reset/retry the endpoint or grant package admission.
Uncertain I/O, owner loss, clock/poll or live-state failures retire its private
object; the enclosing platform retains resources and owns containment.

The actual C component and CSR/RAM/machine RTL are exercised together by
`sources/misteross/prototypes/de25_board_control/verify_rom.py`. Both ROM sizes
execute a streamed program after explicit release; damaged source, stale identity,
wrong epoch, partial/invalid writes, lost Hold, failed I/O, clock/poll bounds and
CRC failures cannot produce an accepted completion. The model also exercises the
actual opt-in strong-provider include and existing computer control gate, with
modeled platform operations and an explicit exact-image digest oracle.

`FES_NANO_COLD_ROM` adds the ROM transfer inside the existing control-opening
operation under its shared secure lock. The successful open reply is withheld
until ROM completion. Failures retire the activation, clear reply provenance
and invoke the existing containment callback. No new SMC operation, data-bridge
release or CPU Run is introduced. The strong provider requires the same finished
configuration, captured build, owner and next generation, control-only scope,
contained masters, held emulation CPU, locked boot CPU and 400 MHz timer. It
hashes the driver's private snapshot with the pinned SHA-384 primitives before
endpoint access. A newer successful configuration may construct a fresh ROM
object after the preceding ROM completed; it cannot reset a failed transfer.

A shared-USB ROM profile requires the accepted private boot/master binding and
its complete coherent bridge observer. That fresh observer supplies the full
cold/USB/master proof for each ownership callback, so the ROM path does not run
an adjacent duplicate cold audit. Missing, foreign or substituted observers
fail before endpoint access; non-shared profiles keep their explicit cold audit.
The driver still brackets every ROM read/write with fresh ownership/access and
timer checks. For the qualified shared profile, a private 16-byte protocol step
now places complete cold/USB/master proofs around the bounded FIFO transaction.
Its per-access guard retains the same private owner/generation/binding checks,
plus live CPU, bridge and peripheral-reset safety. The secure lock and parked
secondary CPUs prevent non-secure software interleaving. Device/clock drift
inside the step must fail the complete end proof before any step is credited;
no CPU Run or load completion can precede final counter/CRC/identity checks.
Only status polling, the exact received/written counters and four ordered data
words are allowed inside a step. Abort consumes its private permission without
hardware or timer reads. Missing or failed hooks cannot select another path.
Legacy callers keep the original per-access full proof. The one-second and
64-poll bounds are unchanged. A pre-I/O CRC-only rejection retains the unbound
transport's prior preflight behavior; the native control gate still retires
that activation before allowing any retry. The joined C fixture runs the actual ROM predicate, trusted boot
constructor, board/USB composition and strong CPU/master callback: 47 cases cover
binding/state/CPU drift and preserve independent device readiness. It measures
52 cold-register reads per callback versus 104 with the duplicated audit; this
is an off-board access count, not physical transfer timing.

The private binding matcher now receives context and callback identities
directly instead of a temporary callback table. The ROM guard also calls the
same shared validator directly. The table factories remain the one-time boot
and service binding APIs. Inside one active-step ownership callback, that shared
validator performs the binding comparison once before any MMIO; the earlier
duplicate comparison is omitted. Full observations at both ends of every
16-byte step and all CPU/reset/bridge checks before and after each register
access remain unchanged. No permission or hardware observation is cached
between callbacks. The measured predecessor spent roughly equal time in full
and inner ownership checks; this change needs a fresh physical timing test.

A failed strong-provider ROM load emits one `FES_NANO_ROM_FAILURE` record after
the existing control gate invokes containment. It records the failed ownership
scope, transfer phase/result, private driver error, access counts, last observed
read offset/value, write offset and timer samples. These are cached during the
original checks and accesses; reporting performs no new endpoint, ownership or
timer reads and cannot retry the transfer. It does not log ROM payload words.
Detailed per-ownership cost profiling has been removed from this timed path
following the matched physical comparison. It required thousands of uncached
counter/accumulator updates while the ROM deadline was running. The timer
callback now reads the counter once and caches that sample for a later failure
report. Every ownership call still reads and checks the live timer frequency;
the initially verified frequency is cached once, and a later mismatch records
the unexpected frequency on failure. No live CPU, callback,
USB, clock, reset, bridge or producer checks were removed. The 16-byte steps,
per-access brackets, one-second deadline and 64-poll limit remain unchanged.
Test-only counters continue to charge the injected clock for actual full/inner
callbacks; those costs do not represent a physical speedup. Timing, successful
ROM loading and full launch/return still need a newly matched hardware test.
The existing failure reply still clears provenance and retires the activation.

The shared-USB held-ROM profile compiles eight ownership/transport objects with
per-object `-O2` after the vendor's `-Os`. `prepare_nano_owned.rom_compile_policy`
selects only `nano_cold`, `nano_cold_platform`, `nano_boot`, `nano_bridges`,
`nano_usb_platform`, `nano_usb_board`, `nano_usb_composition` and `nano_rom`.
The build receipt records the object list and exact Make rules. This selects
compiler optimization without modifying their C source, cache controls, MMIO
observations, callbacks or deadlines. The earlier scalar SHA specialization
retains its own independent compilation policy. Other objects and profiles
retain their existing flags. Effective Make-rule tests check the exact target
scope; the C ownership/failure models also run with the proposed optimization.
The matched ARM code, image size and physical ROM speed still require checking
before this candidate can supply new hardware evidence.
The C/RTL fixture checks containment-before-report, one emission, unchanged
access counts on repeated reports, and both ROM sizes across integrity, scope,
I/O, clock and poll failures. This telemetry is off-board tested; the underlying
hardware failure remains unresolved until a fresh matched image is observed.

This opt-in profile requires a version-2 program binding: the existing 640-byte
header plus a fixed 128-byte ROM extension in the same reserved metadata page.
Two guarded samples capture the extension's size, aligned offset, CRC, SHA-256
and SHA-384. The ROM immediately follows the aligned bitstream within the existing
8 MiB reservation and cannot overlap its metadata page. The native encoder and
actual C decoder have paired tests. Legacy profiles require version 1; ROM
profiles require version 2. The descriptor supplies byte integrity, not package
authentication, a new address or platform authority.

The firmware preparer includes these sources. The FPGA producer has an explicit
ROM variant with the aperture/conduits. `nano/hps_context.py` derives its complete
ROM resource record from the sealed generated CPU route and held computer,
including the image size, source inventory and master-stop binding. Native entry
and cold-chain preparation require that record for the ROM producer profile.
The cold header emits `FES_NANO_COLD_ROM`, the verified CPU aperture, image size
and resource digest only with that complete contract. Ordinary profiles emit
the disabled selection and reject an attached ROM resource.

The native library now has an explicit ROM-profile verifier and retained-package
publisher. It checks the private activation receipt and stages the ROM separately
from the unchanged FPGA payload, then publishes the version-2 descriptor through
the existing owned configuration path. Host tests decode each actual publication
with this C decoder using separately supplied modeled ownership, generation,
boot digest and chunk geometry. The firmware calls and ROM FIFO remain modeled
in those lifecycle tests; the provider/RTL tests above cover a separate boundary.
The native command and cross-builder select this verifier and link its ROM
resource only for the sealed ROM profile with an explicit firmware source and
shared-linker receipt. A freshly built matched boot chain and its physical
acceptance are still required.

The opt-in window SD-return path now connects the retained full-frame closure
to the existing read-only SD lease. The secure provider checks its completed
configuration, identity, reader/probe stop and exact DDR drain records. Only
its matched `FES_NANO_WINDOW_STORAGE_RETURN` profile installs that SD return
callback. The original held-data return predicate stays strict; neither reset
bits nor non-secure status supply a closure. No new wire operation or device
poll is added. Tests join the actual C dispatcher and both reader RTL models,
including missing capability, stale proof and live-scope failure. The
[matched bounded board test](../../../docs/validation/2026-11-04-de25-window-storage-return/README.md)
passes frame return, fresh SD retention and containment for its exact source.
Ordinary package admission and physical repeated-window qualification remain open.

The matched SD-return profile also prototypes rearming after its own complete
window return and SD stop. The dispatcher consumes a private secure grant before
descriptor capture, then permits only configuration operations until completion.
Completion rechecks the captured closure and SD owner/epoch/return/stop records,
creates fresh master-stop state and withdraws the old closure. A new opening
requires the new completed configuration. Tests execute two and three C/RTL
cycles and inject stale ownership, changed receipts, damaged bytes and failed
configuration/open/return operations. The new repeated path has no hardware
acceptance; the earlier one-window capture does not qualify it.

The reader-stop helper caches its first and last stable, identity-checked
samples during the existing bounded stop. A failed cold stop logs their status,
pending address/burst counts and ownership words as `FES_NANO_READER_STOP`.
No additional reader MMIO, stop command, poll or wait is issued for this report. The samples
remain separate from the complete all-master drain receipt, which is absent
after an uncertain or failed stop.

The cold configuration provider now captures a per-load program descriptor
instead of compiling each payload's expected digest table into firmware. Native
code publishes the fixed 640-byte descriptor in the last page of the existing
8 MiB configuration reservation. Payload capacity is therefore 8 MiB minus
4 KiB. Neither the descriptor nor the SMC supplies a DMA address, bridge mask,
master inventory or other platform capability.

`nano_program.c` checks two identical descriptor samples under the live closed
owner/generation and compiled boot-context digest, then retains its own secure
copy. Whole-image and exact indexed-chunk SHA-384 checks use that copy before,
during and after the contained transfer. Completion installs the requested build
identity for the independent live endpoint check and the next master stop.
The acknowledgement's bound-descriptor bit distinguishes this protocol from
the earlier fixed-source response; mixed native/firmware versions reject before
START. Malformed, changed, stale or uncertain bindings cannot retry.

This is byte integrity and ownership, not package authentication or a boot
attestation mechanism. The native selection frontend still restricts the load
to its compiled diagnostic package. The new provider and metadata publication
have host tests, including the actual configuration/bridge engines, pinned
vendor SHA, paired native/C encoding and changed program sizes across owned
returns. They need a new matched boot build and hardware test. The
[previous fixed-package preflight trial](../../../docs/validation/2026-11-03-de25-stored-preflight/README.md)
does not qualify these changed bytes. Ordinary package admission remains open.

Altera permits phase-one and phase-two images from different projects of the
same device when their PFG/SDM firmware and HPS I/O/DDR settings match; see
[hardware compatibility in HPS-first mode](https://docs.altera.com/r/docs/813762/26.1/hard-processor-system-booting-user-guide-agilex-3-and-agilex-5-socs/hardware-design-compatibility-in-hps-boot-first-mode).
The producer's assembler hash check is a build-time compatibility check. An
unsigned package sidecar does not establish the running boot configuration.

The separate network-recovery build now uses a private vendor source copy and
an explicit `fesphy` command. It resets/configures the known KSZ9031 before the
host accepts link readiness. Network startup consumes that prepared object;
it cannot reset the PHY after the host's samples. Failed preparation is retained
and cannot replay. The transformer locks and reconstructs the complete original
driver/header/Makefile, and packaging checks its actual prepared source hashes
and linked command/function. Paired C tests reproduce the old first-start reset
and exercise the changed path and preparation failures. The 4,000 ms vendor
negotiation deadline is unchanged. This repair has off-board evidence and still
requires its own matched recovery boot/network test.

The opt-in `fpgaload --computer-storage-return` profile now permits a read-only SD
lease after the computer's verified held-data return. Bridge control publishes
its private closure only after producer stop and exact reset readback. Opening,
failure or a completed new configuration invalidates that closure. The SD service
requires that closure, the same secure owner and the unchanged live platform
scope; an active lease blocks FPGA operations. Rearm completion independently
checks that SD stopped under the same return. No request supplies a closure or
digest, and data bridges stay reset. Strong provider/dispatcher tests exercise
warm success, wrong ownership, active SD and corrupted return provenance. These
checks are software evidence; the matched warm-return board trial is pending.

The separate `hpsio` cold serial profile compiles a fixed map from the sealed
FPGA build's `hps_bootloader_handoff.bin`. Its read-only provider samples all
164 Agilex 5 pinmux registers twice, checking the expected owner/generation,
closed bridges, contained masters, boot CPU, locked secondary CPU release and
EL3 cache/translation state around every read. The pinned vendor initialization
omits two generic FPGA handoff entries; this observer excludes those exact
entries too. No request supplies an address or value. A mismatch or scope loss
retires the observation and returns no successful digest. Its two-part result
is separate from source context and grants no bridge opening or runtime
admission. Off-board tests cover every read position and the actual strong
provider/dispatcher. The [bounded top-kit test](../../../docs/validation/2026-11-02-de25-hps-io/README.md)
passes both exact live samples under unchanged closed ownership. This qualifies
the fixed pinmux observation and grants no general boot or runtime admission.

The matched native SPL now verifies the loaded BL31 bytes and records the exact
branch target, argument pointers and CPU control registers after cold containment.
Its compiled digest/size and parameter ranges come from the checked BL31/FIT
build. A mismatch rejects before entry. This read-only observation preserves
vendor BL31 startup and supplies no bridge or runtime permission; it is intended
to narrow the unresolved SPL-to-EL3 stop before the first console marker.
The [bounded trial](../../../docs/validation/2026-11-02-de25-atf-handoff/README.md)
stops after SPL's handoff line without reaching this final observation. The
remaining interval includes its barrier, return and loaded-memory guard;
neither a precise fault location nor BL31 entry is established. Firmware/package
upload and independent SD readback pass; native DDR and runtime admission remain
unqualified for that earlier artifact. The subsequent
[contained-console trial](../../../docs/validation/2026-11-02-de25-contained-console/README.md)
passes native boot, sealed FPGA configuration, the fixed read-only DDR request,
reader drain, complete master stop and hardware reclosure. It records active
data scope and leaves optional fabric acknowledgement unqualified. This is
hardware evidence for that fixed diagnostic contract; general DDR use and
runtime/emulation-core admission remain open.

The optional source-bound SMMU serial profile now runs a read-only observation
inside the trusted cold provider. It samples the non-secure controls and only
reads the secure control bank when `S_IDR1.SECURE_IMPL` reports it. Every read
is bracketed by closed-bridge ownership checks; unstable samples or lost scope
fail without replay. The cold DDR profile also observes these controls after
SDM readiness. `bypass_controls_clear` describes the sampled words only: MPFE
traffic attributes, address/firewall proof and complete master drain remain
separate requirements. No observer result grants a bridge or DMA capability.

The cold DDR profile also observes all eight F2SDRAM firewall regions twice
through `nano_ddr_firewall.c`. It checks the RC28 register layout and whether
an enabled region covers the explicitly selected 8192-byte, 4 MiB or private
8 MiB menu bus window. The joined menu-dispatcher regression uses this actual
observer and rejects a region covering only the first 4 MiB slot. Missing,
unsupported or changed window requests reject without a usable
observation. The provider requires
closed cold ownership, EL3 with MMU/cache off, and MPFE out of reset around
every read. Unstable samples, access failures and lost scope retire the
observation without replay. The helper has no write or bridge callbacks.
The serial decoder recomputes layout/coverage from the raw region words;
coverage stays an observation, with access unqualified. See the Altera
[F2SDRAM firewall description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/f2sdram-firewall?contentId=JZo73fDtBno9kTQOAdevlQ).

The opt-in source-bound `fpgaload --ddr-read` candidate connects
`nano_ddr_read.c` to the strong cold provider. Two fresh translation/firewall
and full-pattern checks precede the DDR-only release. The fixed read-only
controller accepts immediate or delayed drain completion and verifies 32
returned bytes, checksum, final data and the generation/cookie acknowledgement.
The complete master-stop callback then precedes hardware drain and combined
reclosure. The closure records that DDR was active; it never claims continuous
data containment or the unqualified optional FPGA warning acknowledgement.
Failure recovery asserts control reset only and retains the DDR reservation
until reboot. The RTL fixture executes the actual controller and endpoints;
the strong-provider fixture injects ownership, register and memory failures.
These are software diagnostics until the matched bounded board trial passes.
The [first bounded candidate trial](../../../docs/validation/2026-11-02-de25-ddr-read-candidate/README.md)
stops after SPL handoff before secure startup, so physical read/drain remains
unqualified despite the passing software checks.

The cold setup now registers the vendor console before copying the SPL record,
with `FES_NANO_EL3_CAPTURE` markers immediately before and after that copy.
Vendor console, stream-ID and assembly startup ordering remain intact; capture
still happens in secure early setup before BL33 or any bridge authority. A
returned copy is a progress marker only. The existing provider must separately
validate the saved record and live ownership before exposing its services.

The [early-capture trace trial](../../../docs/validation/2026-11-02-de25-el3-capture/README.md)
passes its software checks, matched build and both network uploads, but still
stops after SPL handoff with neither new marker. The private top-only JTAG
service programs successfully and restores its original clock. The earlier
EL3 entry/console interval remains unresolved; no DDR read/drain or runtime
admission is qualified.

The [contained large-block TRWU8CBL trial](../../../docs/validation/2026-11-02-de25-large-blocks/README.md)
now completes the exact sealed 2,027,520-byte phase-two image through native
CPU/firmware calls, reports ready and verifies the live build identity with
control reclosed. All data paths remain held. This first cold load does not
establish runtime admission, queue turnover, active-master drain or launch/return.

The shared [contract](../../../sources/mister-packages/docs/embedded-control.md)
owns the register offsets and private firmware query. Its generated C++ and
Verilog consumers come from `make generate`; do not edit them by hand.

## Firmware source

`prepare_tfa.py` accepts only two reviewed versions of
`plat/intel/soc/common/socfpga_sip_svc.c`: upstream TF-A v2.12 blob
`3c223c1498e3a412e075e4f921cd78acd3d0fcd6`, and the Terasic DE25 Nano v2.12
blob `288baffb283787dbd49729983800ce764243adda` from commit
`580dcb5c4e10aa7daff2bf69d493c076a6d3e665`. The board's boot log identifies a
Terasic v2.12 build, but the source tag alone does not prove the exact firmware
bytes currently running. The transformer writes a fresh private directory under
this worktree's `out/`, preserving the source's BSD copyright header. It does
not patch a shared checkout, fetch a board image or deploy firmware.

The contained variant:

- Rejects partial/authentication flags; full configuration uses flag zero.
- Disables bridges and checks the result before CANCEL/RECONFIG.
- Checks CANCEL failure and RECONFIG response shape/zero credits or block size.
- Propagates asynchronous submission failures and every completion error,
  including zero-word errors and errors after an earlier acknowledgement.
- Treats `MBOX_NO_RESPONSE` with no response header as pending; timeouts retire
  the engine. Failed transfers retain their buffers; the engine initiates no
  response flush, replay
  or restart.
- Makes ISDONE leave every data bridge closed, including repeated status polls.
- Handles START's null remapper argument while preserving WRITE's CPU-to-SDM
  relative-address conversion.
- Exposes the local experimental capability query documented in the contract.

The source-bound `fpgaload` cold profile additionally installs
`nano_configuration.c`. Private operation 8 prepares exactly the sealed core
in its fixed, boot-pinned uncached DDR arena. The strong platform uses the
already pinned vendor SHA-384 primitives to compare the whole core and every
chunk with compiled digests; it does not use the convenience wrapper that
touches the watchdog. The gate brackets these reads with live cold ownership
checks and requires EL3 translation and data cache disabled. A completed secure
SD stop may advance the never-enabled generation before preparation; the gate
captures it once, then rejects any later generation change or active lease.
The combined strong-provider fixture executes SD resume/prepare/stop, transfer
and identity/reclose through the actual dispatch and SHA code, with modeled
register/PHY events. Its success is software evidence for this new combination.

Private operation 11 can rearm that fixed source only after a successful prior
transfer and a newer owned all-master bridge closure. The secure state checks
the exact owner/generation, cleared never-enabled history, contained masters,
no active lease and no pending transfer before consuming the new generation and
checking the compiled whole-image digest again. Failed or ambiguous transfers
remain terminal. The computer scope passes this explicit request and its armed
transfer calls to the existing configuration gate after a completed return.
Only successful completion of that rearm permits a new control-only opening,
once for its exact configuration generation. The preceding probe scope must
have closed successfully; its attempted flag and all error state remain intact.
Readiness is checked again before reopening. Injected tests cover three
successive rearm/transfer/control-close cycles and stale, busy, incomplete,
corrupt and lost-ownership paths. The changed firmware has no physical reload
qualification. The native board arena stays pinned until reboot, but grants a
fresh lease after checked transfer completion; uncertain leases cannot be reused.

Configuration completion now invokes a required trusted platform callback after
final status/digests and before publishing `finished`. Callback or subsequent
ownership failure retires the transfer and keeps its storage. In the matched
held-DDR computer profile the callback creates a fresh master-stop activation
from the same compiled reader/probe inventory. A rearm requires the preceding
successful stop/close and the exact newer configuration generation. Failed old
authority cannot be renewed, and active-DDR reload is rejected. The new reader
and probe receipts are read again for the new image; the previous activation's
cached receipt never authorizes its close. `FES_NANO_FABRIC_ACTIVATION` records
that lineage, and each activation emits its own master-stop record.

The joined strong-provider/pinned-SHA fixture covers this matched reader plus
computer profile through both loads and both stops, including corrupt bytes,
stale generations, cache loss and renewed readiness/open/close failure. Running
the same fixture against the preceding source reproduces its failure at the
second opening, while the changed provider completes both inventories. These
are host checks, not a two-load board pass.

Only that cold configuration profile also selects `nano_digest.py`: it verifies
the exact pinned SHA source/header, inserts three GCC loop-unrolling hints and
adds `-O3` to the BL31 `sha.o` target alone. Removing the hints reproduces the
original source bytes. The build receipt records the original/generated hashes
and compilation policy. Other profiles retain the original digest source and
flags. This scalar specialization changes no digest, cache state or verification
coverage. The [matched board trial](../../../docs/validation/2026-11-01-de25-scalar-digest-load/README.md)
measures a 2.761x faster first whole-image digest, then stops on an outstanding
configuration completion. Complete core loading remains unqualified.

Only this explicitly generated profile enables the four existing configuration
operations after preparation. Closed bridge masks, generation 1, never-enabled
history and contained masters must remain unchanged. WRITE accepts only its
next fixed chunk, including the final short chunk; accepted BUSY cannot replay
it. Completion addresses use the reviewed SDM-relative namespace. Configuration
does not open control or data. A later independent readiness query precedes
the secure live-identity probe and control reclose.

The cold diagnostic reports accepted SDM credits/block size once after START.
On a transport failure it records the original status and submission/completion
position before stopping. These are observations; no raw mailbox command or
additional configuration authority is exposed. Empty error responses used to
look like BUSY, and rejected asynchronous sends used to look submitted. The
actual-engine regressions cover both defects, delayed responses, malformed
statuses and late errors following an acknowledgement. The
[matched board trial](../../../docs/validation/2026-11-01-de25-transport-errors/README.md)
reported four credits and a 1,048,560-byte block limit, but no transport-failure
record before the same 786,432-byte completion stall. The fixed deadline and
buffer retention held. Error propagation is repaired; the missing completion
still needs mailbox queue/parser diagnosis.

The next cold diagnostic adds sampled `FES_NANO_CORE_MAILBOX` observations.
The exact pinned mailbox source and header are checked before appending a
read-only observer. It reads CIN/COUT, RIN/ROUT, the incoming doorbell and status,
plus parser state and descriptor counts. No FIFO contents or write-only
register are read; no mailbox command, clear, response consumption or restart
is added. Samples follow START/WRITE and completion polls 1, 2, 16 and 64 per
buffer. They are non-atomic observations and cannot acknowledge bytes or grant
bridge authority. The `job` field reports the next send ID. The
[matched board test](../../../docs/validation/2026-11-01-de25-response-header/README.md)
still stopped with one missing completion after 786,432 bytes. Four pending
samples show CIN=COUT=0x19, RIN=ROUT=2, no incoming doorbell, an idle parser and
no local descriptors. The source stayed pinned and the deadline stayed fixed.

The actual pinned FIFO transport, mailbox-v3 parser and contained engine pass
80 host cases covering every response-ring starting position, a full
2,027,520-byte stream, command/response and job-ID wrap, delayed ACKs, an empty
error reply, an SDM error 2 and a missing reply. Missing replies reproduce a pending transfer
after 786,432 bytes; supplied replies complete. The observer changes neither
registers nor the service database in those checks. This narrows the software
search without proving what the physical SDM is doing.

The vendor API also returns `-2` for both `MBOX_NO_RESPONSE` and an SDM error 2.
The latter consumes a real response header despite having no argument words.
The contained engine now uses the existing header-returning API and clears its
local header before every read, including reads following an acknowledgement.
Only `-2` without a header remains pending; `-2` with a packet retires authority
and preserves the original error code. The actual-parser regression reproduced
the old 786,432-byte stall with an injected error 2 and passes with this repair.
This establishes a software defect. The matched header-aware trial observed no
error packet and stopped at 20.097686 seconds with the same missing completion.
SDM transfer-window and DMA/configuration progress need further investigation;
the observed queue state does not identify their root cause.

The next candidate follows the pinned vendor client's queue protocol: fill up
to four accepted WRITE descriptors, stop writing on BUSY, and consume up to
three completion addresses per poll before refilling. The secure gate keeps a
bounded contiguous window inside the same immutable arena, checks every pending
chunk around calls, and validates the whole completion batch before advancing.
Errors retain all possibly issued buffers until reboot. No new address range,
bridge permission or deadline is introduced. The 144-case actual-parser matrix
includes queued lookahead, batched completions, persistent silence and an error
after a valid ACK. A strong-provider test also verifies two queued chunks through
the real digest/dispatch path. The [matched four-buffer trial](../../../docs/validation/2026-11-02-de25-configuration-window/README.md)
still stopped after 786,432 completed bytes, now with 262,144 bytes outstanding.
All four descriptors were submitted; sampled command and response queues were
empty, with an idle parser and no local descriptors. The unchanged deadline
stopped the attempt and all source bytes stayed owned. This rules out the
one-buffer limitation as a sufficient explanation; the physical cause remains
open. The same source also repairs endpoint progress scheduling for consecutive
successful writes, and passes Tenfoot launch/return and identity-failure tests
under the existing two-second controller deadline.

The cold configuration channel also provides explicit retirement after a
caller-side failure. It checks the same held owner/generation, withdraws all
configuration/bridge authority, then issues one bounded HPS-client
`RECONFIG_STATUS` request. Its separate six-word reader does not restart the
mailbox, use the active parser, acknowledge DMA buffers or open bridges. It
accepts no addresses from the caller. A successful retirement reply still
requires every source and outstanding descriptor to remain owned until reboot.
The [matched retirement trial](../../../docs/validation/2026-11-02-de25-hps-failure-status/README.md)
confirms retirement at the original 786,432-byte stall with all four buffers
held. The HPS status request reaches its 10,000-poll bound with no response
words; the original deadline failure is preserved. A subsequent JTAG general
status query still responds and reports incomplete configuration. The HPS
reconfiguration state and physical cause remain unknown.

Invalid scope, changed bytes, uncertain replies or failed completion retire
authority and keep memory owned until reboot. No reset-bit repair or implicit
retry stands in for drain proof. Other cold profiles still deny configuration,
and the capability query still supplies no runtime activation authority. This
native diagnostic caller still selects one load per boot; the reusable arena
and generation-bound computer opening have not qualified physical repeated loads.
Active drain, ordinary launch/return and production admission remain unqualified.

The Terasic source additionally sends an SDM isolation request during START.
The contained variant rejects unsupported flags and disables bridges before
that request, and treats an isolation failure as an error. The vendor's BUSY
response carries SDM progress in an extra word; the private profile normalizes
that word only for BUSY, while preserving error details. Its query is handled
before the vendor's versioned SiP dispatch. Both sources are tested against
the same native lifecycle and RTL model; the Terasic checks additionally cover
isolation ordering/failure, progress status and actual SiP dispatch.

The pinned Terasic
[bridge reset implementation](https://github.com/terasic/arm-trusted-firmware/blob/580dcb5c4e10aa7daff2bf69d493c076a6d3e665/plat/intel/soc/common/soc/socfpga_reset_manager.c#L391)
reuses return variables across enable and disable handshakes. A later successful
poll can overwrite an earlier timeout. The Terasic variant also transforms that
exact reset-manager source to preserve the first reported error, and tests the
actual vendor and modified functions with simulated handshake results. Those
tests do not establish that physical bridges or their traffic have drained,
or that a partial enable can be safely reversed. The bounded
[Nano bridge trial](../../../docs/validation/2026-10-31-de25-bridge-trial/README.md)
returned firmware error `4` with the required reset bits already set. Native
handoff and physical drain remain unqualified.
The [subsequent firmware-stage trace](../../../docs/validation/2026-10-31-de25-firmware-first-failure/README.md)
identifies stage 3, `f2s_fpga_ack_set`, with `-ETIMEDOUT` (`-60`): the initial
FPGA acknowledgment was absent while the request bit was set. The board trace
alone did not establish the reason for the missing acknowledgment.
The [reference-source investigation](../../../docs/validation/2026-10-31-de25-ack-wiring/README.md)
finds that the vendor source disables the optional fabric handshake while the
Agilex5 routine requests it even for an already-reset bridge. Its actual-C
model reproduces the failure; this does not qualify skipping hardware drain.

The contained Terasic probe now records the first failed disable poll and
immediately samples `HDSKACK`, `HDSKREQ` and `BRGMODRST`, before subsequent
writes/polls. Later errors cannot replace the first snapshot. The existing
`HPS_SET_BRIDGES` service clears this record before each call. The six
Agilex5 stages use the pinned reset-manager definitions:

| Stage | Name | Required `HDSKACK` mask/value |
| --- | --- | --- |
| 1 | `s2f_ack_clear` | `0x400 / 0` |
| 2 | `lw_ack_clear` | `0x200 / 0` |
| 3 | `f2s_fpga_ack_set` | `0x4 / 0x4` |
| 4 | `f2s_flush_ack_set` | `0x1000 / 0x1000` |
| 5 | `sdram_flush_ack_clear` | `0x800 / 0` |
| 6 | `sdram_fpga_ack_clear` | `0x4 / 0` |

Only the dedicated local probe requests extra return registers by passing
`FES_NANO_BRIDGE_DIAG_REQUEST` in the normally unused `x3`. The original `x0`
status is unchanged. Ordinary callers retain the original `SMC_RET1` path.
This is an opt-in diagnostic extension of the contained Terasic source, not
a change to the shared programming/capability contract or upstream TF-A.
The version tag and packing are defined in `nano_bridge_diag.h`:

- Returned `x1`: tag `0x46424431` in the upper 32 bits, first stage below it.
- Returned `x2`: signed 32-bit error above the sampled `HDSKACK` value.
- Returned `x3`: sampled `HDSKREQ` above sampled `BRGMODRST`.

Stage zero means no failure snapshot; zero snapshot words do not mean those
registers were read as zero. IDs 7/8 describe the vendor's non-Agilex5 NOC
branches and are not executed by this candidate. The snapshot consists of
separate reads after the failed poll, not an atomic capture or a substitute
for the original error. Poll counts/timeouts, reset writes and first-error
propagation are preserved. The dedicated loader rejects unavailable or
inconsistent diagnostic identity/state even if the ordinary checks pass.
The profile assumes the existing exclusive trusted-runtime sequencing.

It preserves accepted-BUSY and completion-address semantics. The existing
`HPS_SET_BRIDGES` service still exists; this is an exclusive trusted-runtime
sequencing contract, not a security boundary against another SMC caller. No
production programming registry is changed. Other source versions reject.

```sh
python3 platform/embedded/firmware/prepare_tfa.py \
  --reference out/reference/socfpga_sip_svc.c --output out/contained-tfa
```

The validation record includes a successful `PLAT=agilex5 bl31` AArch64 build
from the actual TF-A v2.12 tree with this prepared source. BL31 alone does not
configure Nano DDR, select its UART/PHYs or provide a complete boot chain.

## Controlled Nano bridge service

`nano_probe_stop.c` supplies the inherited DDR probe's part of a platform
master-stop callback. A qualified adapter supplies its fixed control aperture,
exclusive boot/reset scope and bounded I/O. It observes two identical snapshots,
leaves an already-idle probe alone, and issues CANCEL once for an active probe.
Only IDLE with equal accepted/completed byte counts and no fault can complete;
held addresses and missing responses stay outstanding. Polling stops at 128
iterations. I/O errors, changed scope or operation identity, faults and timeouts
retire the helper without replay. Successful repeated checks require the same
fresh receipt. Call it while control is open; a closed control aperture cannot
be sampled as evidence. The existing controller separately owns its reader's
generation/cookie stop, and the platform still has to contain every other
FPGA/HPS producer.

The source-bound cold DDR configuration provider now installs
`nano_fabric_stop.c` through the existing master callbacks. Its control map
binds the inherited probe's separately decoded CPU aperture and rechecks the
generated DDR master inventory, both remaining maintenance managers' reset routes and the
generated sources against the seal. The native map decoder and cold header
reject an omitted, changed or foreign stop map.
The DDR producer disables the unused coherent F2H interface and removes its
translator/JTAG manager; an enabled hard port must not be left unconnected
when reset containment lets synthesis remove that maintenance path.

Before the live identity match these callbacks report only the independently
contained HPS producers, while every data bridge stays held. Closing control
checks the exact main-reader ABI/build and bus window, live maintenance-held
bit, generation/cookie acknowledgement and inherited probe receipt. An idle
or already-drained reader needs no command; an enabled reader receives one
DISABLE, then a bounded wait for the same generation and cookie with zero
pending traffic. The inherited probe independently receives CANCEL only if
needed. A final fresh observation of both receipts precedes completion.
The private menu selection explicitly requires control ABI `0x10203043`, an
8 MiB arena with two 4 MiB slots, and its read-only configuration lifecycle.
It uses the same independent stop algorithm with the menu's maintenance-held
register at `0xcc`; the 4 MiB CRC window uses `0xc8`. The complete cold-provider
and owned dispatcher compile under that menu contract. `verify_menu.py` tests
the actual C stop helper against the menu CSR/video/DDR RTL, including active
and already locally drained readers, inherited-size rejection and ownership
loss. Its maintenance probe is injected idle/held there; the separate complete
fabric verifier covers the inherited probe RTL. These are host checks.
Matched menu boot files and the first serial firewall stop are retained in the
[bounded menu trial](../../../docs/validation/2026-11-07-de25-native-menu-trial/README.md).
That attempt accepted no menu frame; physical DDR and HDMI remain unqualified.

The configured stop cookie is a nonzero minimum. If frame updates have advanced
the endpoint's request cookie, the callback selects the next full 64-bit token
from the validated snapshot before its single DISABLE. Exhaustion fails before
any write; cookies never wrap. The C/RTL window test completes eight reader
generations and then drains an interrupted ninth using this fresh final token.
After control closes, the original cold ownership guards the retained receipt;
the provider performs no FPGA reads through the closed aperture.

Any changed identity, maintenance bit, CPU/cache/bridge ownership, unfinished
traffic or I/O failure retires the callback without reset or command replay.
The existing identity recovery can still assert control reset while all data
paths remain held. `verify_fabric_stop.py` joins the actual C implementation to
both actual reader RTL models, including held addresses, partial responses,
idle/drained cases and late receipt changes. Strong-provider tests separately
exercise the fixed MMIO bindings and retained closed receipt. Those are
software checks; neither these callbacks nor their serial marker supplies
physical DDR/firewall/addressing qualification or runtime admission. Generic
cold data release remains denied.

The [matched TRWU8CBL observation](../../../docs/validation/2026-11-02-de25-ddr-prerequisites/README.md)
now passes the idle callback, live identity and read-only SMMU/firewall path
with control reclosed and all data held. It exercises no FPGA DDR request or
active traffic drain.

`verify_probe_stop.py` joins the actual C helper to the inherited Avalon probe
RTL and its independent burst/response bench. The DDR board-model verifier
runs it against the selected project's probe source when a project is supplied.
Those checks inject ownership and memory responses; they grant no physical
drain, mapping, allocation or bridge authority.

`nano_bridges.c` implements the owner/generation checks behind the native
platform's bridge contract. Its operations are injected and must be serialized,
bounded-return and qualified by trusted boot/platform code. There are no default
MMIO addresses, ownership callbacks or never-enabled boot proof. Unknown closed
and partial initial reset states reject. Lost ownership, incomplete producer
containment, driver errors and changed reset/master readback retire the service.
Control-only and data opening validate their exact masks and order. A data
opening selects a nonzero subset of mask `13`; the native platform supplies
that selection explicitly over the generated 1.8 bridge channel. Package
metadata cannot select or qualify it, and the platform enable callback can
reject any unqualified path. The cold provider still allows only its fixed
control probe and denies generic data release.

Configuration uses `fes_nano_bridges_configuration_guard` to check an already
owned closed state without issuing another shutdown. In control-only recovery,
data paths stay reset: the service closes only the control path and explicitly
returns retained data containment, with zero fresh fabric/data ACK claims.
An active data shutdown requires the hardware-drain sequence for every active
hardware path (`12 & ~before.reset`). It closes only active paths and reports
no fresh acknowledgement for a path that remained reset. DDR-only mask `8`
therefore keeps reset `5` while active, closes mask `10` and reports hardware
drain `8`. Stopping and independently proving every producer remains mandatory.

The opt-in `nano_bridge_driver_source` transformation preserves the pinned
vendor driver and first-error checks while gating only optional fabric warning
writes/polls on an injected, qualified capability. It covers enable as well as
disable. Hardware flush/fence polls stay mandatory. The normal source
transformations and default `build_terasic.py` mode do not select this driver
or service.

The pinned FPGA-to-DDR disable branch waits for `F2SDRAM0ACK` to clear, while
FPGA-to-CPU disable waits for its flush acknowledgement to assert. Host tests
inject the actual expected values from the pinned headers and reject a poll
for an unopened path during partial close. This checks driver control flow;
it does not prove physical DDR drain. Independent stop/drain evidence for the
complete DDR master inventory remains required before a physical binding.

```sh
python3 platform/embedded/firmware/verify_nano_bridges.py \
  --reference out/terasic-git-restore-tfa/plat/intel/soc/common/soc/socfpga_reset_manager.c \
  --output out/fresh-nano-bridge-service
python3 platform/embedded/tamago/nativefabric/verify.py \
  --reference out/terasic-git-restore-tfa/plat/intel/soc/common/socfpga_sip_svc.c \
  --reset-reference out/terasic-git-restore-tfa/plat/intel/soc/common/soc/socfpga_reset_manager.c \
  --nano-bridge-service --output out/fresh-nano-bridge-integration
```

The first command executes the actual driver C with injected registers. The
second exercises the controlled SiP dispatcher, C service and driver through
the production Go wire adapter, existing controller and tenfoot actor, with
actual FPGA RTL. Register, ownership, memory and SDM responses are models.
See the [service validation](../../../docs/validation/2026-10-31-de25-nano-bridge-service/README.md)
and [SiP binding validation](../../../docs/validation/2026-10-31-de25-owned-sip/README.md).

`prepare_nano_owned.py` installs the opt-in controlled channel in a fresh export
of the exact Terasic pin. The [shared wire contract](../../../sources/mister-packages/docs/embedded-bridges.md)
defines a private call distinct from the older mutable bridge call. Querying
the service cannot disable bridges on the inspected older firmware. The
controlled dispatcher admits only that channel, the existing capability query
and the four contained configuration operations. It blocks alternate legacy,
versioned and arbitrary-mailbox routes. Bridge and configuration operations
share a lock and the same ownership guard; an outstanding configuration blocks
bridge mutations, and pending buffers block ISDONE. Public raw bridge wrappers
cannot bypass that guard. PSCI power/reset transitions invalidate ownership;
the cold serial profile instead denies CPU_ON without side effects, as below.

```sh
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/terasic-git-restore-tfa \
  --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --nano-owned-bridges --output out/fresh-owned-bl31
```

This builds a controlled BL31 component, not a bootable candidate. Its default
secure provider supplies no evidence, so Snapshot returns the recognized
`NOT_READY` reply. Reviewed secure boot code can provide a record, independent
qualified platform policy and complete producer callbacks through
`fes_nano_platform_boot_record`; `fes_nano_bridge_platform_handoff` consumes
that evidence once. Non-secure callers cannot initialize it. The pinned HPS-first path
does not establish the required never-enabled history or whole-board master
containment. Closed reset bits alone cannot supply that proof. The default
host tests inject trusted initialization. The optional cold serial build below
installs a separate actual SPL producer and strong EL3 consumer.

`nano_boot.c` validates the record's chain identity, owner, epoch, exact version
and complete producer inventory against independent secure policy. The record
cannot grant its own qualification. Unknown/partial initial state, missing
qualification and failed/replayed handoffs reject. Consumed inputs are copied;
later caller mutation cannot change the authority. This is a secure C interface,
not a signed boot attestation or a new SiP initialization command.

Its master-stop callbacks first stop new requests and require an independent
drained-producer bitmap for all HPS masters, other CPUs and fabric producers.
Only then do they assert functional resets followed by OCP resets and check
fresh readback. The installed TF-A adapter supplies the real PER0 register
operations, barriers and bounded delays. Board-specific ownership, quiesce and
drain observation remain unqualified; no default fabric/master proof exists.
Control-only access may arm a new reader while data bridges remain held; an
active close instead needs a fresh producer-stop receipt. Ownership loss,
timeouts, restarted producers and readback failures retire the provider.
HPS peripheral resets stay held: this provider targets the serial-only native
probe. Qualified per-device resume is still required for native USB/SD/network
operation after programming. The separate recovery loader does not select it.
See the [handoff and recovery validation](../../../docs/validation/2026-10-31-de25-trusted-handoff/README.md).

Build receipts distinguish `strict`, `owned-unready` and `owned-cold-serial`.
Recovery packaging requires `strict`; the native loader now requires its exact
matched `owned-cold-serial` profile. Both reject the default unready variant.
Earlier producer receipts remain historical evidence.

### Closed-bridge SD lease

The [SD wire channel](../../../sources/mister-packages/docs/embedded-sd.md)
shares the bridge owner and generation. `nano_sd.c` requires secure-only
initialization with scope, controller resume, clock/PHY preparation and
independent drain/reset callbacks. A leased SD controller blocks bridge
snapshots, mutations and FPGA configuration before all-master cold callbacks.
Successful Stop restores the old cold scope before publishing containment;
any uncertain transition retires both services and retains the lease.

The owned dispatcher recognizes the SD channel under the same platform lock.
Default owned and cold serial builds leave it NOT_READY; they install no
SD authority. The [native session](../tamago/sdcontrol/README.md) binds the
actual PIO driver to this service through injectable transport/registers.
C/Go tests inject scope and MMIO and exercise read/quiesce/resume, changed cards
and retained failures. `nano_sd_platform.c` supplies the actual Nano callbacks;
`nano/sd-profile.json` selects them in the matched cold chain. Secure setup
verifies the original owner, closed bridges, retained CPU/master scope, direct
SD pin routing and live 200 MHz clock derivation before touching a contained
controller. Resume releases only the SD controller and its ComboPHY reset,
checks reset readback and applies the pinned Nano PHY timing. Prepare repeats
that timing after the Go host reset, before enabling the card clock. Stop
independently checks DMA/interrupts/errors, drains PIO, stops the clock and
asserts/readbacks only the controller reset; unused ECC remains held.

The timing comes from the exact Terasic Nano DTS at `d0f8813`, rather than
the different generic bare-metal defaults. Clock and pin routes are checked,
never reprogrammed. Failures retain their first observations and retire access;
the diagnostic printer uses saved values only. Register models exercise these
actual callbacks, including two full leases and failed reset/PHY/clock/drain
paths. Physical reads remain a separate gate. The matched cold diagnostic
still advertises zero FPGA programming features and prevents secondary CPU release.

That separate [read-only native SD gate](../../../docs/validation/2026-11-01-de25-sd-platform/README.md)
now passes on TRWU8CBL for candidate `db7c01de309270e7`: two FAT32 reads across
firmware containment/resume and final containment, with independently expected
file hash and card capacity. Default unready/serial builds still grant no SD
authority. This qualifies the closed-bridge diagnostic only.

### Actual cold serial producer and callbacks

```sh
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/terasic-git-restore-tfa \
  --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --nano-owned-bridges --nano-cold-serial-entry out/nano-entry \
  --output out/fresh-cold-bl31
```

This opt-in source-bound diagnostic links `nano_cold_platform.c` as a strong
provider. It consumes a bounded, aligned OCRAM record passed directly by SPL
to early EL3 setup, before non-secure execution. There is no later SiP or
non-secure initialization route. A checksum detects record corruption; it
does not authenticate an attacker-controlled boot chain.

The matched SPL uses `nano_cold.c` and the shared real register adapter to
sample SDM boot kind, reset cause, parked secondary CPUs, bridges and unused
masters before initialization. Only positively identified POR/cold/nCONFIG/JTAG
entry paths qualify. SD is the sole active device, with all DMA disabled.
After loading the sealed FIT and native entry, SPL independently observes
SDHCI idle, masks interrupts, disables the card clock, then asserts functional
and OCP resets with barriers/readback. It supplies a copied one-use record
bound to the exact entry, producer sources, owner and epoch. Full U-Boot never
reopens SD after BL31 receives that record.

Its private version-2 record also captures CPU power status, MPU power
request/status and boot configuration at each ownership boundary. The selected
primary must agree with actual EL3 MPIDR, both CPU-channel masks must name only
it, and every spare controller must retain its zero reset default with no
pending release or cleared spare reset-history bit. The active primary's
reset bit is excluded from the live-state test, following the
[factory CPU comparison](../../../docs/validation/2026-11-01-de25-cpu-inventory-reference/README.md)
and [initial EL3 power observations](../../../docs/validation/2026-11-01-de25-el3-power-inventory/README.md).
This is a trusted fresh-entry parking policy, not a generic power-off decoder
or hotplug/drain implementation. It does not accept the old six-word tuple.

The cold profile's actual PSCI CPU_ON frontend returns `PSCI_E_DENIED` before
framework bookkeeping or any hardware write and preserves the owner. The lower
platform callback returns `PSCI_E_INTERN_FAIL`, as required by TF-A's internal
callback contract, and cannot release a CPU. A strong release-lock function in
the frontend's translation unit is mandatory for the early provider; the matched
U-Boot builder requires its installation receipt. Other power/reset transitions
continue to invalidate ownership. Version-1 records/cookies reject. The shared
bridge wire ABI is unchanged.

The actual `owns`, `quiesce` and `quiescent` callbacks verify continued cold
containment through live registers; they do not access an SD controller held
in reset. A changed boot/reset scope, restarted master, active bridge or
replay retires the proof. This is not an active-device drain provider. The
cold dispatcher admits read-only Snapshot and optional source-context reads,
denies generic bridge mutations and configuration operations, and advertises
zero configuration features. The dedicated identity diagnostic below has its
own contained secure transaction.
The general controlled service and strict recovery behavior are unchanged.

The explicit `fpgaload --computer-control` matched build adds one exception for
the sealed internal-RAM ZX81 computer. After complete checked configuration,
the dispatcher rechecks read-only SDM readiness, borrows the existing secure
probe/platform scope and admits one owned control-only opening per completed
configuration. The original
owner/generation and completed configuration state must agree. While open it
permits only ownership snapshots and its exact held close; SD, configuration,
source-context reads and data release reject. Close runs the existing complete
master-stop callbacks. Failure retires authority and invokes the established
control containment callback without replay; a completed scope permits
read-only source/snapshot checks and the separately guarded fixed-source rearm.
A completed rearm can open the computer again only with its newer configuration
generation; a changed bridge generation alone cannot grant access. Uncertain
renewed readiness, opening or closure cannot replay or produce another grant.
The separate source-derived computer map is
bound into the native/entry/SPL/BL31 chain. This mode does not grant an ordinary
runtime profile, DDR access or physical core-execution acceptance.

The dedicated native `bootcontext` build derives its source descriptor from a
verified sealed HPS-first project before building SPL. The existing cold chain
binds that descriptor, native entry and producer sources. SPL/BL31 compile the
same context digest; private bridge operations 4/5 expose it only under an
unchanged owned, closed scope. A missing digest returns NOT_READY. This is
source consistency, not measured boot or live FPGA admission; it supplies no
new master-stop, control-opening or configuration permission. See the
[wire contract](../../../sources/mister-packages/docs/embedded-bridges.md).

The separate `fpgacontext` build also binds the actual compiler-produced
lightweight control route. Its secure one-shot operation opens only that
control bridge, reads the generated endpoint identity and idle registers
twice, checks the sealed ABI/build, and closes control before replying. The
provider's private expected mask changes only around the serialized secure
transition; all data bridges, spare CPUs and peripheral masters retain their
cold containment. Generic non-secure Control/Release/Close operations and
SDM configuration remain denied. Default builds have no identity provider.
The diagnostic requires EL3 translation and data cache disabled, matching
the inspected vendor startup; a different CPU memory setup rejects without
altering it. A failed transaction independently asserts control reset and
retires ownership, preserving the first failure and cleanup result. The
held-data close supplies no fresh fabric acknowledgement or active data drain.
This is an aperture qualification diagnostic, not a runtime admission provider.

Private operation 10 adds a bounded on-chip reader cycle through that same
source-bound secure probe. It shares the identity attempt, so read-only probing
and reader exercise cannot be replayed within one boot. After the initial live
identity match it enables reader generation 1, offers a held address, disables
with cookie 7 and returns two response beats separately. A held address or a
partial burst cannot acknowledge drain. Only the final exact ACK with zero
pending requests succeeds; two final identity/state snapshots precede control
reclose. Commands and response beats issue once, read polling is bounded, and
failure preserves the original error and retires ownership. Fixed offsets and
values are owned by firmware; the non-secure request supplies no register or
address. Its distinct reply tag is checked by native `ProbeReader`. The actual
C service and Avalon RTL pass their joined host tests, including I/O failures,
premature ACK, faults and ownership loss. This operation remains physically
unqualified and supplies no DDR, scanout, other-master drain or data release.
The native loader now selects this reader operation after contained SD-package
configuration. Its changed matched boot chain needs separate physical
qualification; the preceding physical pass used identity-only probing.

The probe now checks SDM readiness before opening control. It sends only
`CONFIG_STATUS`, keeps a zero-initialized six-word response on the secure
stack, requires exact transport/length and the complete ready flags, and
rechecks the original closed ownership. An unconfigured fabric returns
NOT_READY with generation 1 and no bridge or endpoint access. A malformed
query retires authority. This read avoids the legacy `FPGA_CONFIG_ISDONE`
frontend and does not authorize configuration. Per-register before/after
markers localize a subsequent access stall. A hung CPU transaction may prevent
the cleanup callback from running; the host retains that stopped trace and
does not infer closure from its timeout.

The separate `fpgastatus` diagnostic invokes private operation 7: one fixed
configuration-status query under the original closed owner, followed by an
unchanged Snapshot. It does not invoke the identity operation. The firmware
returns a readiness bit with the unchanged owner/generation and never opens
control or touches an endpoint register through this query. Default providers
remain unready; malformed replies and lost scope retire authority.

The [Nano guide](../nano/README.md) describes matched builds, a preserved
recovery card and repeatable network uploads of `FES/UPLOAD.BIN`. The
[current validation](../../../docs/validation/2026-10-31-de25-cold-upload/README.md)
records real cross-builds and injected host tests. The later
[physical upload/cold trial](../../../docs/validation/2026-10-31-de25-upload-cold-trial/README.md)
passes the upload core/transport but rejects the first SPL CPU/reset proof.
The documented sticky POR flag is now accepted alongside SDM cold reset;
independent boot-kind proof and every CPU/ownership gate remain required.
Fresh diagnostic samples cannot replace failed early proof. The
[later native Go test](../../../docs/validation/2026-11-01-de25-native-go-boot/README.md)
now passes the matched cold serial handoff on TRWU8CBL, including both owned
snapshots, EL2/EL1 entry and the actual Go probe. That is contained serial
diagnostic acceptance. Native SD coexistence with active FPGA data, USB/network resume,
active FPGA drain, signed secure boot and physical launch/return remain open.
The [version-2 initial-parking trace](../../../docs/validation/2026-11-01-de25-initial-parking-v2/README.md)
now accepts the observed core-0 startup with the independent power/selection
checks, then unconditionally parks before DDR/storage/BL31. That result does
not exercise the matched secure firmware, SD drain or native handoff.

## Board-specific off-board candidate

The two build commands below require clean checkouts of the exact Terasic
commits and an AArch64 GNU cross compiler. `build_terasic.py` exports the pinned
TF-A Git tree into a fresh private directory, applies the source transformation,
builds BL31 and checks its ELF load segments against the vendor's BL31 bounds.
`build_terasic_uboot.py` builds the matching DE25 U-Boot source
out of tree, using that BL31 receipt. It extracts the ATF image from the FIT
and requires its bytes to match the selected BL31. It checks that U-Boot's
initial FIT load does not overlap BL31's actual loaded memory, including BSS.
The dedicated Nano loader identity also binds the exact BL31 digest, so a
firmware-only rebuild changes both SPL and full U-Boot's expected identity.
Packaging checks that binding. This does not establish the subsequent
relocation or native memory handoff.
U-Boot's host build also
needs Python setuptools/pyelftools, SWIG, dtc, bison, flex and OpenSSL headers.

```sh
git clone --depth 1 --branch de25_nano_revA_v1.0 \
  https://github.com/terasic/arm-trusted-firmware.git out/vendor-tfa
git clone --depth 1 --branch de25_nano_revA_v1.0 \
  https://github.com/terasic/u-boot-socfpga.git out/vendor-uboot
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/vendor-tfa --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --output out/vendor-bl31
python3 platform/embedded/firmware/build_terasic_uboot.py \
  --reference-repo out/vendor-uboot --bl31-build out/vendor-bl31 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/vendor-uboot-build
```

These commands create `bl31.bin`, `u-boot.itb` and an SPL hex file only; they
do not write a boot image or access a device. The generated SPL is not paired
with the separately sealed FPGA phase-one image. The FIT and receipts are
content-hashed diagnostics, not signed boot images or hardware acceptance.

The pinned [vendor BL31 source](https://github.com/terasic/arm-trusted-firmware/blob/580dcb5c4e10aa7daff2bf69d493c076a6d3e665/plat/intel/soc/agilex5/bl31_plat_setup.c)
selects EL2 for BL33 when available. In the pinned
[U-Boot ELF loader](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/lib/elf.c),
`bootelf` enters the loaded image directly; the board defconfig does not select
the EL1 switch. It therefore does not satisfy the native image's EL1 and clean
MMU/cache contract by itself. The separate [entry prototype](../entry/README.md)
uses U-Boot's `booti` cleanup and a guarded EL2-to-EL1 transition, tested with
the vendor source built for QEMU. A Nano-specific resource handoff, matched
boot chain and recovery procedure remain required before attempting a board boot.

## Adapter and endpoint

`Control` uses synchronous, ordered, bounded-return read/write hooks for the
relative CSR aperture. It compares two snapshots, verifies all 128 build-ID
bits, and binds disable/drain to the old generation and a nonzero cookie.
The host-only `ContainedPath` fixture exercises the existing lifecycle order:
query firmware, disable/drain, contain, configure, open control-only access,
verify fresh target identity, then wait for a separate explicit data release.
The product adapter exposes only per-phase control operations, with no second
lifecycle, save or recovery policy. It allocates no memory and uses no OS or
C++ exception/RTTI support; that adapter cross-compiles for AArch64.

The runtime owns its admitted bytes and all reservations throughout this
transaction. Program hooks must retain their buffers through asynchronous
completion or unresolved failure. The test path verifies that failure cannot poll, retry or release;
there is no destructor which frees hardware resources. A failed bridge/MMIO/SMC
operation can have an uncertain outcome; callers must retain ownership and
recover physically, rather than assuming that a failed release stayed closed.
A deadline cannot preempt a firmware call or bus access that never returns.

Platform hooks must prove containment of **all** other masters, program only
retained admitted bytes, and perform masked bridge readback. Control-only access
must reach only the HPS-to-FPGA control aperture while FPGA-to-HPS/DDR masters
stay contained. The reference mask is the inspected TF-A lightweight-bridge
mask; its actual Nano routing and reset behaviour need board qualification.
Only after identity verification may a separately authorized release open the
data paths required by the admitted target. The test's all-bridge release is
a simulated platform operation, not a board default.

The RTL is a single-clock, single-AXI-ID read guard/control endpoint, not a whole
scanout engine or an address firewall. The upstream reader must supply legal
aligned INCR requests and a safe DDR window. Board glue must supply clock/reset,
CSR interconnect, CDC where needed, the real DDR port and the scanout/HDMI path.
Reset clears identity of the active generation and cannot acknowledge an old
drain. Reinitialization needs a known quiescent/reset memory interface.

## Verification

```sh
python3 platform/embedded/firmware/verify.py \
  --reference out/reference/socfpga_sip_svc.c \
  --cross-cxx out/de25-os-tools/zephyr-sdk-0.17.4/aarch64-zephyr-elf/bin/aarch64-zephyr-elf-g++ \
  --output out/embedded-control-check
```

Without `--reference`, the verifier downloads that exact public Git blob and
checks its identity before use. `--output` defaults to a fresh private directory.
Without `--cross-cxx`, it runs host RTL/firmware integration and explicitly marks
AArch64 adapter compilation not run. The affected-test planner schedules this
standalone consumer; no shipped core currently instantiates the new endpoint.

The verifier compiles the actual prepared START/WRITE/completion/status/remap
function bodies with simulated platform calls and undefined-behaviour checks,
then links them with `Control`, the host-only sequence fixture and Verilator's actual RTL model.
It exercises an identical-image reload, wrong identity, queued bursts, held AR,
early/wrong RLAST, RID/RRESP faults, reset, stale generation/cookie, CSR and reader
backpressure, simultaneous AR/response, unknown firmware, transport/start
failures, lost asynchronous responses and deadlines. This executes the patched
C functions, not an EL3 SMC instruction or actual SDM. The Nano backend remains
read-only. Source/command/ELF hashes and that evidence boundary are recorded.

## Trusted USB-only PIO handoff prototype

`nano_usb_platform.c` captures the cold boot owner and epoch and checks a private
generation before I/O. Only USB0's module and packet-RAM/ECC resets may be
released (PER0 bits 3 and 11, mask `0x808`). Both must be released before any
controller access and both must be asserted before Stop publishes containment.
Partial release, partial stop or a reset changing during the lease retires it.
This follows the [Agilex 5 USB reset sequence](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/taking-the-usb-2.0-otg-controller-out-of-reset?contentId=9Cx0WYhpfh7XsISMD5WQCQ).
DMA and IRQ ownership must remain disabled. SD, USB1, other HPS masters and all FPGA bridges stay held. The
separate cold USB scope leaves `fes_nano_cold_owns` strict; it grants no combined
SD/USB or USB/FPGA permission. USB must stop before those operations under this
prototype. Combined operation still needs an explicit ownership contract.

Handoff has two phases. Acquire checks board preparation, rejects DMA/IRQ or
active-channel state, validates the controller and selects ULPI SDR. It reserves
the controller and advances its generation, but leaves `confirmed` false. The
host driver must then initialize the controller and port. Confirm independently
checks live readiness and publishes a new confirmed generation only on success.
A prepared lease can stop after failed host startup without claiming readiness.
For example, acquire 0 -> prepared 1 -> confirm 1 -> confirmed 2 -> stop 2 ->
contained 3. The old prepared generation cannot stop the confirmed owner.

Stop halts every implemented channel with finite polls, masks channel interrupts,
clears the controller's port-power request, flushes RX/TX FIFOs, observes AHB
idle and verifies controller reset under the original cold owner. An already
contained stop checks preparation and boot ownership without accessing the
controller or requiring a live PHY/VBUS observation. Stale requests do no I/O.
Partial operations retain the reservation and retire the provider/cold scope;
they cannot retry. Port-power readback does not prove board VBUS is electrically
off. All callbacks and driver accesses require one serialized platform owner.

`nano_usb_board.c` supplies the preparation and live callbacks. Preparation
checks two complete passes over the 164 sealed HPS I/O registers, derives the
matched 200 MHz L4_MP bus clock from 13 live PLL/NOC registers and compares the
captured clock state. It does not require the independent SDMMC clock gate.
The pin table and policy digest are copied at initialization. Preparation never
reads USB registers while the controller is held in reset.

Live confirmation checks PIO/no-IRQ/host mode and port state, then reads two
USB3320 snapshots through DWC2 GPVNDCTL. It checks PHY identity, synchronous
interface/function mode, VBUS drive and the internal VBUS/session comparators.
It never writes PHY registers or reads the clear-on-read interrupt latch. The
external VBUS-indicator mode is rejected because USB3320's EXTVBUS signal can
read permanently high. These are live protocol checks, not voltage, timing or
electrical qualification. PHY/cable changes, foreign viewport requests, changed
clock/pin state or interrupted I/O retire the scope.

`nano/usb_readiness.py` derives the compiled pin header through the existing
strict `hps_io.derive` parser and the actual compiled handoff/build receipt. It
never imports register expectations from an edited saved policy descriptor.
The selecting boot producer must first verify the sealed project and context;
the policy digest identifies those compiled expectations and is not a signature
or runtime authority. The USB SiP dispatcher is installed with an unbound default provider, which
returns NotReady. The default cold profiles keep the provider unbound and USB contained. The
explicit `usbcheck` selection installs the trusted MMIO binding described below;
physical USB readiness remains unqualified. Existing storage/Tenfoot images keep
USB disabled.
Next integration needs the selected secure/native binding and a fresh matched
boot chain before a bounded USB test; mixed SD/USB/core operation remains open.

Register provenance is the pinned Terasic U-Boot
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413` Agilex5 DTS/reset/clock source and
Synopsys DWC OTG register layout. The board manual identifies USB3320 and its
ULPI clock: [Terasic manual, section 3.8.5](https://mm.digikey.com/Volume0/opasdata/d220001/medias/docus/7704/P0804.pdf).
PHY registers and comparator behavior follow
[Microchip DS00001792F, sections 5.6 and 7.1.1](https://ww1.microchip.com/downloads/aemDocuments/documents/UNG/ProductDocuments/DataSheets/USB3320-Data-Sheet-DS00001792.pdf).
The GPVNDCTL layout is also present in the
[vendor DWC OTG header](https://nest-open-source.googlesource.com/manifest_repos/u-boot/+/5e15fb1fe70ac2857e056ad5d238ad8e3373fdb5/drivers/usb/host/dwc_otg_regs_294.h).

Run `python3 -m unittest discover -s tests -p 'test_nano_usb_*.py' -v`.
Undefined-behavior checks exercise the actual C provider/cold scope, compiled pin
policy and independently modeled clock/PHY/channel events. Cases include three
acquire/drain/reset cycles, preparation/confirmation/abort, stale generations,
every pin/clock mutation, live-mode/VBUS/viewport failures, copied policy and
interrupted I/O. Existing SD tests cover the shared cold scope. Host register
models and freestanding objects supply no hardware USB acceptance.

## USB lease wire and native driver binding

`nano_usb_service.c` connects trusted USB callbacks to the generated
[`fes.fabric.embedded.usb`](../../../sources/mister-packages/docs/embedded-usb.md)
channel. Trusted initialization supplies the original cold owner/epoch and
callbacks; non-secure callers cannot initialize it. The default ClosedOnly dispatcher reserves the
exclusive device lease before acquisition and blocks SD, bridge and configuration
calls until a checked stop. Lost or inconsistent state retires ownership and
retains the reservation. A successful callback must also produce the expected
generation and phase before firmware returns a receipt.

The native `usbcontrol` client validates those receipts and wraps one synchronous
report driver. `OpenKeyboardWithControl` binds it to DWC2 without a fallback
when firmware refuses ownership. Driver enumeration precedes live confirmation;
reports follow confirmation. Startup failures stop a known prepared lease once;
ambiguous replies retain resources until reboot. The default boot profile does
not select this path; the explicit `usbcheck` diagnostic selects it.

`--usb-input gamepad` selects the typed gamepad driver under that same lease.
The selected input is bound into the native link command, entry receipt and
cold chain identity; it grants no additional device or FPGA access. The native
diagnostic records the exact HID report descriptor and normalized reports, then
requires the usual checked stop. A failed schema check may record the descriptor
but cannot establish readiness. The observer receives the independently checked
input selection and rejects records from another protocol.

The C RPC fixture runs the actual dispatcher, USB service, cold policy and board
checks against modeled registers/PHY responses. Separate Go tests run the actual
DWC2 driver with a modeled USB device and firmware replies. These checks cover
prepare/confirm/stop, stale sources, failed enumeration, lost replies and blocked
SD/FPGA calls. They do not establish physical USB or simultaneous USB/core access.

The cold chain identity includes every input from the owned firmware installer,
including the USB wire service and program actor. A producer-inventory regression
checks this closure so adding firmware code cannot silently omit it from the
matched boot-chain identity.

## Selected cold USB boot diagnostic

`tamago/run.py usbcheck --board de25-nano --profile nano/probe-profile.json`
(with an explicit sealed `--hps-first-project` and toolchain/output arguments)
selects the runtime-owned USB path. The unchanged profile enters with USB held
and advertises no static readiness. The native link assignment, entry receipt
and cold profile bind that selection; the entry rejects a missing selected
client/driver path. No USB provider is inferred from a clock value or caller.

The cold producer installs `nano_usb_boot.inc` only for that selection. Its
trusted callbacks use the existing EL3 MMIO/barrier/wait operations, the accepted
cold provider and the complete sealed pin map. Board/clock checks initialize
while the controller remains reset. The runtime then obtains the boot owner
through context/pin verification, acquires the prepared lease, initializes the
real DWC2 driver, confirms PHY/VBUS readiness and polls for five seconds before
a checked stop. Objects remain rooted after failures. Startup failures abort
only a known prepared lease; uncertain replies retain ownership.

This is a bounded USB-only diagnostic, not combined USB/storage/core permission.
The DWC2 model supports a boot keyboard through a bounded USB 2 hub tree.
The existing keyboard/controller-through-hub connection still needs physical
hub/input qualification; gamepad decoding and native launcher integration remain open. No physical USB test is established here.

## Explicit shared USB PIO service

`fes_nano_usb_service_init_shared` binds the compiled USB transition callbacks
and exact cold/master/bridge ownership through `fes_nano_usb_master_service_ops`.
The shared service maintains a separate USB reservation and emits SharedPio,
with current DataHeld as an observation. Its dispatcher gate observes live USB
before permitting other owned providers. Unconfirmed USB blocks them; the SD
lease still blocks FPGA access. Core stop/drain and qualified returns remain
mandatory. USB Stop is refused while a core's bridge is open, and does not clear
an active SD lease after the core has returned. Provider binding changes or
uncertain state retire the boot authority and retain resources.

The default USB diagnostic remains exclusive. The native builder's explicit
`--shared-usb-input boot-keyboard|gamepad` selects a composed cold startup only
with `fpgaload --selected-programs --tenfoot-serial` and the complete computer
reload/SD-return path. Entry packaging verifies exact shared-scope linker
assignments, source hashes, sealed pins and all selected memory/control maps.
The common receipt policy is part of the producer source identity.

Secure startup initializes SD, then the physical USB board/platform, cold device
composition and independent FPGA producer callbacks. It binds that exact master
policy to the already accepted private boot object before publishing the shared
USB service. Any missing SD setup, denied binding, bad clock/PHY or duplicate
initialization retires the provider. The selected-startup test runs the actual
board readiness and boot include with an injected PHY/register model and three
SD cycles. ARM64 compilation and these checks do not qualify physical combined
USB/SD/FPGA operation.

The shared master provider requires a complete, trusted producer snapshot.
`fes_nano_cold_snapshot` samples the independent producer bitmap between fresh
cold-state observations, then checks USB DMA/IRQ/PIO/clock/idle containment
before its final cold sample. The strong adapter checks CPU identity, EL3,
translation/data-cache state and the secondary-release lock on both sides.
Only then are producer bits published. Private device reservations, generations,
callback identities and paired bridge/reset reads must also remain unchanged.
Missing or substituted producer callbacks reject without a fallback.
The returned reset values come from those paired cold samples; the consumer
performs no MMIO outside the strong callback's CPU guard.

This complete observation replaces nested full ownership calls in the shared
read-only bridge observer. It caches no hardware permission and changes no
per-CSR native before/after checks, deadlines, stop/reset callbacks or explicit
USB device readiness. Software tests cover late CPU/reset/device drift, producer
errors, callback substitution and publication after failure; physical timing
and a complete shared core-launch cycle still require a matched kit test.

The exact zero-argument read-only bridge snapshot uses this complete observer
once inside the dispatcher, with shared-service validation before and after.
An unconfirmed USB controller or active SD lease remains busy. Other operations,
malformed snapshot requests and mutations retain the ordinary USB gate. The
reply uses the same bridge codec and carries no extra capability. Dispatcher
tests compare its actual hardware-access counts with one complete observation
and exercise live failures, malformed requests, configuration, explicit USB
readiness and retained reservations.

The format8 mailbox-view diagnostic adds one private EL3 alias:
virtual `0x90000` maps only the physical mailbox page at `0x10a30000` as
secure Device, read-only and execute-never. The original peripheral and shared
DDR mappings remain. Its stack-free transition reads CIN, COUT, RIN, ROUT,
response doorbell and status before activation, through the original mapping
afterward, and through the secure alias. Six fresh copies of the existing
CPU/MMU/all-master guard frame the three views. Actual linked code checks bind
each fixed base, packed offset set, six-read loop, guard and phase position.
No FIFO word, mailbox command, acknowledgement, reset or retry is added.
A register access that never returns cannot be preempted by the polling bound.

The hexadecimal UART formatter is shared through an exact local assembly call.
Each call copies its argument and preserves/restores LR through scratch x8;
the 96-byte callee is stack-free and makes no calls. The linked-code verifier
requires the complete callee, every call target and the surrounding scratch/LR
instructions. Its original 16-digit output, 100000 polls per character and
384-byte line budget remain. Unselected calls, changed bounds, stack use or
unreviewed writes still reject. This keeps the matched diagnostic below the
original 8 MiB FIT limit without removing guards or extending deadlines.

Both new view records and their exact linked proof are required for the selected
MMU profile. Partial/faulted, duplicated or misplaced views cannot admit the
loader. Values remain read-only observations and never establish command
consumption, FPGA configuration or runtime authority. The observer belongs to
the cold-chain source identity and compiled producer closure. Hardware
qualification of this changed image remains open.


The selected format9 repair makes that exact secure mailbox page writable for
its existing privileged driver. No other descriptor, memory layout, device
window or shared-DDR permission changes. The pinned vendor header selects the
original physical mailbox base with SCTLR_EL3.M clear and the secure virtual
base with it set. This is a read of CPU state, with no mutable mode latch,
mailbox command, retry, acknowledgement or reset added. The original vendor
transport and owned service source files remain byte-identical. Selection
requires the complete installed MMU binding and exact transport/header inputs;
the final generated header hash is checked after compilation and belongs to
the cold-chain source identity. Existing ownership, response, first-failure and
one-second tape checks remain. The read-view observer still performs only reads;
its contract now describes the same writable page used by the driver. Physical
command acceptance and full launch/return qualification remain open.

The bounded `6c9e1aaecb129336` board test subsequently received a successful
FPGA_CONFIG_COMP response through this secure mapping. It then stopped at the
unchanged initial-menu deadline before the CANCEL return checkpoint. No FPGA
data WRITE or menu Ready was observed. The selected START observer v4 adds three
UART boundaries: phase 12 before the original remapper call, phase 13 after it,
and phase 14 after the original response-clear call. Existing phase 5 remains
the CANCEL return. Every original call, argument, device access, ownership check
and timeout stays intact. The bounded phase order is
`0,1,2,3,4,12,13,14,5,6,7,8,9,10,11`; missing, duplicate, reordered or nonzero
boundary records cannot complete the configuration proof. These records locate
progress and provide no hardware admission authority. The next precise stalled
call and full launch/return cycle still require physical evidence.

The subsequent `8bc1ed9a9478e26d` hardware trace reached the pre-remapper marker
without its post-call marker, while FPGA_CONFIG_COMP again succeeded. Actual
linked descriptors show that the original remapper uses NS Device accesses to
System Manager. Its exact failed instruction and live firewall setting remain
unproven. The selected format10 candidate adds one private secure Device/RW/XN
page at VA `0xa0000` for PA `0x10d12000`, preserving every other descriptor.
Only the original remapper's one read and two conditional writes select this
alias when SCTLR_EL3.M is set; before activation they keep their physical
addresses. All conditions, register values, null-pointer handling, address
translation, ownership checks and deadlines remain. Source transformation is
pinned and reversible; final service bytes are checked after compilation. Both
the firmware inventory and cold-chain identity inventory include the new
`nano_remapper.py` input. The first off-board candidate exposed an omitted cold
identity entry; that unprogrammed image is retired and a fresh matched chain is
required after correcting the inventory. The subsequent `e3de630956f0c154`
hardware trial completed every START boundary, acknowledged all menu image
bytes and reported configuration ready. It then stopped before the first menu
frame, after the SMMU/firewall inventories. The exact stalled instruction remains
unproven. Other System Manager users still used the original NS view, and the
SMMU secure-bank ID read zero through its NS mapping; that sample cannot pass
the unchanged later secure-bank admission gate.

The selected format11 prototype replaces the remapper-only transformation with
`nano_secure_mmio.py` and `nano_secure_mmio.h`. It changes only address selection
in the pinned private BL31 MMIO accessors. Existing System Manager reads and
writes on PA `0x10d12000` select the existing secure Device/RW/XN alias at VA
`0xa0000` after MMU activation. This includes raw bridge control and the cold
boot-kind read as well as remapper setup. Existing reads of the SMMU secure bank
on PA `0x16008000` select a new secure Device/RO/XN alias at VA `0xb0000`.
Writes to that bank retain their original physical path. Before MMU activation,
both pages retain physical addresses. Unrelated addresses return unchanged
without reading SCTLR_EL3. These selectors add no device operations or retries.
Access widths, bit operations, values, offset allowlists, ownership checks and
all deadlines remain. The original observed service is restored unchanged.

The read-only secure view follows the Arm SMMUv3 security contract: secure-bank
registers other than S_INIT are read-as-zero/write-ignored for NS accesses
([Arm SMMUv3 architecture, section 3.10](https://documentation-service.arm.com/static/63d7a2d5e4378a55c5e045b9)).
This architectural rule supports the address repair; it does not establish the
current physical register values. A host test executes the actual SMMU observer
with protected fake secure/NS pages and exercises its ownership failures. It
restores the existing conditional 26-read path when SECURE_IMPL is visible,
without bypassing any register or window checks.

Both producer inventories bind the helper source and header. Final compilation
also verifies the installed MMIO headers through the MMU source binding. The
private image grows one page, relocating linked code/tables/data; its 2 MiB DDR
exclusion and canonical permissions remain. After normalizing relocated table
pointers, the only external mapping change is the single RO SMMU alias. QEMU
executes all ten complete linked selector instances before and after MMU enable,
rejects nine changed-code controls, and checks 36 descriptor queries plus 19
changed-descriptor controls against the actual production tables. These checks
exercise address selection and translation; they perform no Nano MMIO and do
not qualify physical cache/device behavior. A fresh matched hardware test is
still required before claiming that startup or ordinary core admission works.


The live MMU scope API passes only its observation context and callback as
scalar arguments. It preserves every live register observation and predicate,
with the original cold enable callback table. The selected ARM build has an
eight-instruction wrapper with no stack/callback-table copies. Mutable MMU
lifecycle/failure state retains secure NC OCRAM storage; memory descriptors and
the tape deadline are unchanged. See the scalar-scope validation record for
source/compiled checks and the remaining physical trial.
