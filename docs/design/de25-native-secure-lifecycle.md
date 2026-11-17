# DE25 native secure lifecycle investigation

This is the Linux-free, opt-in experiment tracked by [#1867](https://github.com/DeanoC/fes/issues/1867).
It describes the current implementation and the remaining qualification work.
It is not authenticated secure boot, a completed security review or permission
to resume a stopped board. Production Linux defaults are unchanged.

The retained physical failure used source
`e04af2f8a55263644422a412bd02ba69dea58b1c`. Its console SHA-256 is
`2bb22ae6c869d8693e5ea9887d3dcb9d138d3d64d4fa77640417e78113681cc3`.
Initial MENU and warm ZX81 configuration, retirement, continuation, a 917-byte
tape and running CPU fetch passed. Master-stop passed; source release for the
return to MENU failed at bridge generation 15. The old error discarded the
SMC result words and the supplier emitted no failure boundary. **The original
trace cannot identify the rejecting guard.** A timeout is one possibility,
not an established cause. Preserve the failed procedure and its verdict.

## Trust and authority

| Actor | Current responsibility | What it does not prove |
| --- | --- | --- |
| BootROM and SDM | Vendor boot/configuration processing and mailbox DATA consumption | Our package hashes do not establish a fused authentication policy or vendor reset guarantees |
| Vendor SPL / U-Boot | DDR setup, matched BL31 handoff, FIT selection and native entry | A healthy loader prompt does not retire a predecessor's DMA/source lease |
| Patched TF-A at EL3 | Serialized SiP calls; boot epoch/owner, bridge/device containment, retained source, SMMU command/retirement and role checks | A successful mailbox reply alone grants no native source overwrite |
| Native entry / TamaGo at EL1 | Checked load/layout, pinned mappings, store/admission, retained configuration/ROM, independent tape and frontend lifecycle | EL1 observations or increasing generation numbers cannot manufacture secure completion |
| FPGA endpoints | Build identity, CPU Hold/Run, ROM/tape delivery and master-stop/drain handshake | Build identity is not a cryptographic trust root; a stopped emulated CPU alone does not prove every fabric master idle |
| USB, SD and MAC masters | Device-specific DMA/IRQ and ownership state | A software pause/closed handle is not physical DMA containment |
| Host programming/restoration tools | Exact artifact/cable/session binding, one-use procedure and captured observations | Host process exit, USB re-enumeration and JTAG availability are not target retirement |

The operator, admitted boot chain and native EL1 runtime are trusted in this
experiment. Packages supply data and FPGA payloads, not arbitrary native EL1
executables. The exclusion/mapping checks assume that trusted runtime remains
in control of its own page tables. They are not isolation from a malicious
privileged native program. Secondary CPU release remains locked; SMP needs a
new ownership/cache contract before another CPU may share these resources.

The live authorities are deliberately different objects:

- `ProgramService` owns the native request/session generation and its terminal
  state. It serializes frontend operations and retains resources on uncertainty.
- The bridge service owns boot owner/epoch, bridge generation, access mode and
  closure. A source generation is the bridge generation at configuration; it
  remains fixed while later device/session operations advance the bridge.
- `nano_configuration` owns accepted byte counts, pending/issued work, completed
  source generation, release target and rearm admission.
- The transfer service and two probe/retirement slots own actual mailbox,
  SMMU and source history. Successful retirement keeps the source pinned.
- The SD service owns grant, return and stopped generations; shared USB owns
  its separate device generation. Neither equals the frontend generation.
- `ConfigurationArena` owns the canonical backing object and private successor
  callback. Copied receipts, public snapshots and copied arenas confer no reuse.

This table and the transition rules below are the review model, not another
runtime authority or a new receipt to synchronize. Consolidation should remove
redundant *checks of identical immutable inputs*, not merge these independent
ownership domains or accept one domain's token as another's permission.

## Required transition ordering

Let `C` be the completed source's bridge generation. All operations retain the
same secure owner and boot epoch. The concrete first-cycle values below match
the retained failure and the joined test. Native frontend generations are a
separate sequence and must not be used in these equations.

| Transition | Required state / evidence | Result |
| --- | --- | --- |
| Boot → initial source | Fresh sole CPU, matched boot record, all bridges reset, masters contained; SD acquired/prepared/stopped | Initial configuration at `C=4` |
| Source transfer | Descriptor/source bound; every DATA extent accounted for; status complete | No source overwrite or fabric opening yet |
| Retirement → continuation | Retained table and queue checks; abort/disable/invalidate completion; unchanged source and callbacks | Configuration may complete; source remains pinned |
| MENU active | Selected MENU build, control path then checked reader window; frame acknowledgement | MENU closure returns at `C+3` (`7`) |
| MENU stop → SD | Matching endpoint stop/drain, actual window closure; storage resume/prepare/stop | Source-release target `C+6` (`10`) |
| Secure release | Completed old source, unchanged boot/role/device/closure/queue/table state; fresh release operation clock | Old reader/control authority consumed once; no table/register/DATA writes |
| Native successor reservation | Exact original private arena callback; unchanged source/header/ROM before and after SMC; matching four-word reply and stopped context | New bytes may overwrite the same arena under its lock |
| Rearm → computer | Consumed old release, closed session, stopped SD, unused or successfully retired older slot | New configuration at `C=10`; previous slot retained while new slot is prepared |
| Computer active | Selected build/ABI and held ROM; optional independent tape completes before Run | Data bridges remain contained; only the control path opens |
| Computer stop → SD | Hold/stop/drain and held closure at `C+2` (`12`); SD resume/prepare/stop | Release target `C+5` (`15`), then MENU configuration |
| Returned MENU → next core | Same MENU closure and release equations, with new private leases | No old frontend, arena or SMC capability may replay |
| Final Park | Stop owned input/network activity, close current endpoint once, shut down owned device paths | Terminal native service; no subsequent source reuse or activation |

The actual source-return predicate checks role-specific closure, identity probe
completion, master-stop request/ack generation and cookie, zero pending work,
`fabric_generation+1`, all SD callbacks and the SD grant/return/stop record.
It does not accept a caller assertion of “closed”. MENU and computer have
different generation increments because MENU opened a reader data path.

Shared USB may remain acquired/confirmed at USB generation 2 while its DMA and
IRQ activity are paused and audited through the pinned supplier. Source
admission also checks the exact callback binding. It cannot be replaced by a
`paused` boolean. Runtime MAC activity is currently rejected by this source
supplier; U-Boot recovery Ethernet success does not qualify native networking.

Relevant implementations: [source supplier](../../platform/embedded/firmware/nano_sdm_source_platform.inc),
[release supplier](../../platform/embedded/firmware/nano_sdm_release_platform.inc),
[transfer service](../../platform/embedded/firmware/input_transfer_service.c),
[role dispatcher](../../platform/embedded/firmware/nano_program_control.inc),
[native reservation](../../platform/embedded/tamago/nativefabric/source_reservation.go),
[arena](../../platform/embedded/tamago/nativefabric/configuration_arena.go).

## Physical memory and reuse contract

Addresses below are half-open ranges. Geometry comes from the matched source
contracts; it is not a statement that another image has reserved the memory.
Live translation, attributes, exclusions and backing identity still need to
match at every boundary that uses them.

| Range | Actor / attributes | Reuse condition |
| --- | --- | --- |
| `0x00050000..0x00058000` | Private EL3 stack in secure Normal NC OCRAM | Only the owning firmware; no native allocation |
| `0x00058000..0x00060000` | Private EL3 BSS/controllers in secure Normal NC OCRAM | One boot epoch; linked size must remain inside the existing 32 KiB reservation |
| `0x80000000..0x80200000` | BL31 code, initialized data, vectors and six page-table pages; private WB DDR with page-specific permissions | Protected from native heap and explicit mappings throughout runtime |
| `0x80200000..0x82000000` | Broader boot/firmware exclusion, including U-Boot | Boot producer decides lifetime; native allocator never borrows it |
| `0x82000000..0x83000000` | Recovery download buffer, 16 MiB | Recovery-only fresh session; not concurrent native runtime storage |
| `0x83000000..0x84000000` | Recovery SD readback buffer, 16 MiB | Same recovery session and checked file readback |
| `0x84000000..0x94000000` | Native image, heap, stacks and pinned temporary-entry workspace; EL1 WB | Loader staging must be excluded from every destination segment until handoff; Go then owns its declared RAM |
| `0x94000000..0x94010000` | SMMU source tables, command/event queues; NC, XN | Actual selected engine prepares and validates; retained slot/queue history gates rearm; source release writes nothing here |
| `0x94800000..0x95000000` | Configuration payload, aligned retained ROM, final metadata page; NC, XN | Only atomic private successor reservation after secure release; neither tape completion nor configuration completion suffices |
| `0x95000000..0x95800000` | Two 4 MiB MENU frame slots; NC, XN | Owned scanout publication/acknowledgement and session closure; never overlap retained configuration or tape |
| `0x95800000..0x95a00000` | Independent 2 MiB tape reservation, 16 KiB payload, final metadata page; NC, XN | New completed canonical program lease required; error retains arena; success cannot release configuration |
| `0x96080000..0x98080000` | Maximum native entry image window | Native entry lifetime/exclusions checked before segment writes; no unproven concurrent reuse |
| `0x99000000..0x9a000000` | Verified boot FIT source copy, 16 MiB | Boot/recovery-only; disjoint from entry, scratch, heap, device arenas and boot reservations |
| `0xb0000000..0xc0000000` | SPL high DDR heap/BSS exclusion | Never native heap or source storage |

Gaps are not additional allocator capacity. EL3 maps most non-firmware DDR NC
and NS; EL1 maps its normal heap WB and device/source arenas separately. Alias
and cache maintenance obligations must therefore be checked for each sharing
path, not inferred from equal physical addresses. Special low secure MMIO
aliases for remapper/mailbox/TCU access are explicit descriptors, not normal
heap memory. Source hashing, native mapping observations and SMMU translation
retirement are separate checks.

SMMU source permissions have 4 KiB granularity. The final source page may also
contain padding or the beginning of its 64-byte-aligned retained ROM. The map
therefore does not provide byte-exact isolation of that tail. The guarded DATA
address/length ledger supplies the exact source extent, while the native arena
separately protects ROM/header immutability. Do not describe the page map alone
as excluding every adjacent byte. This observation is not evidence of the
historical failure and does not authorize weakening either check.

The boot-FIT allowance is 16 MiB. FPGA package/configuration storage retains its
independent 8 MiB arena (with ROM/metadata consuming part of it); increasing the
FIT allowance never enlarges that arena. The entry decoder retains a 32 MiB ELF
limit and protected initial stack. A new linked image must re-admit all ranges,
OCRAM usage and stack frames; host tests do not qualify that image.

Sources: [firmware RAM](../../platform/embedded/firmware/nano_firmware_ram.py),
[EL3 tables](../../platform/embedded/firmware/nano_firmware_mmu.py),
[board resources](../../platform/embedded/tamago/board/de25nano/resources.go),
[tape layout](../../platform/embedded/nano/initial_tape.py),
[FIT layout](../../platform/embedded/nano/fit_layout.py),
[entry contract](../../platform/embedded/entry/README.md).

## Clocks and cost

| Bound | Origin / scope | Current interpretation |
| --- | --- | --- |
| Native configuration: 20 s | Each `configureTransfer` operation, checked before/after synchronous calls | A late successful SMC still fails native admission; frontend/session lifetime is not this timer |
| Firmware transfer: 20 s | Before source digest; includes contained preparation and final source work | Separate clock from the native caller; nesting can expose an outer deadline first |
| SMMU preparation: 4 s | Original preparation guard | Expected-word batching reduces work; it does not renew the origin |
| Retirement stage: 2 s; completion: 1 s; final: 4 s | Original nested retirement/command origins | Continuation shares retirement's original final bound |
| Source release: 1 s | New release operation after session stop/SD return | Long gameplay must not expire it; backwards or exhausted clock fails closed |
| CPU control: up to 1 s / 64 polls | Each held control operation, inside bounded caller contexts | Distinct from bulk tape delivery |
| Tape | Size-derived total budget plus bounded individual steps | Historical 917-byte transfer used 1.801 s of its 6 s total; do not charge it to the CPU-control second |
| Host restoration: 1,200 s in the retained stopped trial | Whole host controller | Repeated source/tool hashing and JTAG queries across serial SD commands exhausted it; not evidence that source release took too long |

Firmware clocks require the pinned 400 MHz physical counter, converted to
microseconds by `/400`. The new diagnostic stores the last accepted and rejected
samples and first clock failure. It never reads a new counter while reporting.
Model ticks and operation counts are synthetic; no new physical release cost
has been measured and **no bound is changed by this investigation**.

There is a specific cost lead, without a failure attribution. The retained
physical first release (`source_generation=4`, target `10`) records
`elapsed_us=929971`, leaving 70,029 microseconds of its one-second budget. An
instrumented copy of the joined synthetic supplier fixture counts 296 SMMU
reads on that first release and 3,176 on the warm computer release (`10 → 15`),
with 10,240 table-word loads in each. The warm ownership predicate adds its
six-register secure SMMU view repeatedly. These counts exclude other CPU/device
checks and are not physical latency measurements or a calibration of the old
trace. They justify observing the failed release's own clock/stage before any
cost or budget repair, not declaring a timeout from the outer error.

Host ownership needs a separate follow-up: validate programming ownership for
the programming phase, then bind SD commands to the exact fresh serial/boot/
card session. Continue to check live owner/epoch/integrity where mutable, but
do not rerun full immutable tool/source audits and JTAG clock queries on every
SD command. That optimization needs a regression for cable/session replacement
and before/after file preservation; this milestone does not implement it.

## New release diagnostics and joined regression

Rejected native release now includes all four returned words and the expected
owner/generation. It still requires the exact unchanged success reply.

If the selected supplier is entered and fails, one
`FES_NANO_SOURCE_RELEASE_FAIL` snapshot records source/target generation,
transfer-service stage, first clock error, scope reason, accepted/rejected
clock samples and the first observed table/register mismatch. It grants no
reuse. Snapshot collection performs no additional source/SMMU, table,
ownership or counter callbacks; UART emission uses the existing console path.
There is no supplier record when an earlier configuration/dispatcher gate
rejects before entry; the native response and preceding trace remain necessary.

| Stage | Last boundary entered |
| --- | --- |
| 0 | Supplier preflight / first clock, before transfer-service release |
| 1 | Completed transfer/control ledger |
| 2 / 5 | First / second retained table pass |
| 3 / 6 | First / second mailbox-idle check |
| 4 / 7 | First / second retained register view |
| 8 | Final mailbox check |
| 9 | Service source authority consumed; supplier postcheck still required |

Clock error `-110` means exhaustion, `-34` a backwards observation, `-13` a
clock guard's ownership/context mismatch. Scope reasons use the existing
source supplier's numbered predicates; `34` identifies returned-session
validation. A nonzero mismatch address identifies the first already-read
register/table value; zero means none was recorded. These are observations,
not an alternate success or retry interface.

The new [native test](../../platform/embedded/tamago/nativefabric/secure_lifecycle_test.go)
connects actual `ProgramService`, store/loader, retained arena, transfers,
ROM handling, independent tape publisher, MENU/computer sessions and return
to the [C fixture](../../platform/embedded/firmware/tests/sdm_retire/lifecycle_rpc.c).
That fixture executes the real selected source supplier, transfer/parser,
vendor mailbox/DATA, SMMU preparation/retirement/release and SD return engines.
Source byte length, chunk, SHA-384, owner, source generation, role and ROM size
cross the boundary; release replies come from C, not a fabricated success.

The joined test covers four launches/returns per valid case, alternating both
ROM sizes, tape lengths 917/0/1/16,375, one very long simulated session, and final
Park. Seven rejected returns cover identity, cross-epoch context, changed table,
pending mailbox, incomplete ledger, elapsed and backwards clocks. Every
rejected return preserves the entire retained source/ROM/header backing and
prevents a second secure release. C asserts no release register/table/DATA
writes. Existing component suites cover additional faults and cancellation.

Important limits: source hash suppliers, physical MMIO/CPU/device completion,
native secure dispatch and activation are injected. C transfer execution is
joined at completed configuration boundaries rather than every individual Go
SMC. Tape completion is injected in this combined test; the actual C tape
engine has separate joined publication tests. The fixture's final Park is a
native/model result, not physical USB shutdown or HDMI acceptance. This closes
a previously mocked *boundary*, not every off-board deliverable in #1867.
The configuration payloads are synthetic 128-byte packages; component transfer
tests separately cover large payloads and fragmentation. These runs do not
measure the physical image's source hashing or transfer cost.

Valid generation-15 release succeeds in this model. Several distinct real
supplier predicates can produce the old outer error. Thus neither the model
nor its injected failures establish the historical board failure's cause.
The pre-change control passes valid cycles but fails the new diagnostic
assertions; the observability repair makes those assertions pass. No claim
is made that a source-release behavior defect has been repaired.

## Stopped states and recovery

| Stopped state | Retained authority / permitted next step |
| --- | --- |
| Content rejected before hardware transition | Keep healthy owned MENU/store; normal bounded rejection path may restore display |
| Partial/uncertain configuration or retirement | Retain source, arena, device and engine history; no retry/rearm/overwrite |
| Source release rejected, including generation 15 | Retain source and native terminal state; failed C source is not reusable; no guessed success/replay |
| Native instruction/USB/ownership failure | Retain resources and stop ordinary calls; a healthy adapter does not repair runtime state |
| Qualified fresh recovery boot | New boot/session authority permits only the recovery gate's explicit readback/restoration class |
| JTAG/SDM reset unproven or recovery gate fails | Stop, close owned host handles and preserve trace; physical intervention may remain necessary |

[PR #1938](https://github.com/DeanoC/fes/pull/1938) separately qualifies a
program-bound fresh SPL witness and original-FIT restoration/readback without
power cycling. It does not prove SDM reset, retire the old source for runtime
reuse or resume the old native session. USB reset only repairs host transport;
CPU-only reset may leave fabric/SDM/DMA state; a full volatile SOF/SPL boot needs
its own fresh witness and containment checks. A genuinely wedged SDM/JTAG path
can still need power removal. Current kit state belongs to its status ledger,
not this document or the historical issue description.
The original restoration controller still failed at its host deadline after
verified writing/preservation. A separate fresh read-only procedure completed
the original-FIT check. Successful restored data and that independent readback
do not turn the stopped controller into a hardware pass.

The vendor documentation reviewed on 2026-11-17 supports these distinctions:

- The [Agilex 3/5 Booting User Guide 26.3, Reset](https://docs.altera.com/r/docs/813762/26.3/hard-processor-system-booting-user-guide-agilex-3-and-agilex-5-socs/reset)
  says CPU software reset leaves SDM functionality unaffected and HPS cold/warm
  reset does not reconfigure the FPGA. The
  [Agilex 5 configuration reset table](https://docs.altera.com/r/docs/813773/26.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/device-response-to-configuration-and-reset-events)
  marks SDM reset/boot-ROM restart for a power cycle, not nCONFIG or the HPS
  cold-reset pin. A healthy JTAG-delivered loader does not contradict that.
- The [SMMU/F2H/reconfiguration erratum](https://docs.altera.com/r/docs/825514/current/agilex-5-es-device-errata-and-user-guidelines/fpga-to-hps-f2h-bridge-hps-warm/cold-reset-and-fpga-reconfiguration-issues-with-smmu-enabled)
  describes repeated-loading/reset failures. Its
  [2026-10-02 applicability table](https://docs.altera.com/r/docs/825514/current/agilex-5-es-device-errata-and-user-guidelines/altera-specific-errata-for-the-agilex-5-es-devices)
  lists ES 065 A/B R0 parts for that entry, not the sealed
  `A5EB013BB23BE4SCS` target. Symptom similarity does not establish applicability.
  This comparison is not a fresh reading of the kit's physical silicon marking
  or a review of every CPU/SMMU erratum.

Vendor guarantees still needed include end-to-end source-read completion after
the acknowledged SDM DATA path, the exact TBU/NoC transaction retirement scope,
and whether a supported non-power sequence restarts SDM itself. Keep these
separate from the reset-register observations already qualified in #1938.

## Remaining work before hardware qualification / issue closure

The [fresh diagnostic chain](../validation/2026-11-17-de25-secure-lifecycle/matched-boot-chain.json)
now passes SDK, entry, BL31, SPL/U-Boot, full-SOF composition and checked native
file admission on implementation `b700d743c7e0799cf2a78ed2fce710b23a59f5ae`.
Its native boot ID is `7876283624f9c3d4`; the 8,571,584-byte FIT and full SOF are
unprogrammed. Both FPGA core payloads are unchanged. This advances build/file
evidence only. The existing host restoration-cost problem must be repaired
before another bounded trial; repeating its stopped controller adds no proof.

1. Review this model against the vendor reset, NoC completion, SMMU and SDM
   guarantees. In particular, bound what retired source DMA completion proves
   and which resets actually clear outstanding transactions. Record unanswered
   guarantees explicitly rather than treating a passing model as a guarantee.
2. Preserve and recheck the prepared matched diagnostic artifacts before use,
   including OCRAM and every source/memory check. A later implementation repair
   requires a fresh complete chain. Do not inherit the historical image's board
   evidence or treat file admission as runtime authority.
3. In a new bounded procedure, capture the failed release's response and new
   supplier snapshot. Rejecting before supplier entry needs dispatcher/USB/
   configuration evidence, not a speculative source-clock adjustment.
4. Repair only the demonstrated guard/cost/ownership mismatch. Extend the
   actual supplier/native regression to reproduce that condition before the
   fix. Add remaining combined cancellation, mapping/acknowledgement and
   partial-completion boundaries; existing separate tests are not the same proof.
5. Qualify repeated `MENU → ZX81+tape → MENU → ZX80 → MENU → Park` on the exact
   matched artifact, including source reuse, USB shutdown and declared failure
   recovery. Record serial, physical display/input and ordinary HIL separately.
   Keep #1867 open until its full completion criteria are met.
