# Native contained-fabric endpoint

The separate `HPSContextVerifier.OpenComputer` path connects the native
[`computer` driver](../computer/README.md) to a source-derived 32-byte Avalon
window and the existing owned bridge service. It requires the original cold
closed scope and permits only a control-only opening. Every register operation
checks the same secure owner/generation before and after physical access.
`ComputerSession.Close` requests Hold once through a usable driver, retires
register access, verifies complete master-stop/bridge reclosure and rechecks the
secure source context. Failed commands and unknown closure are not replayed.
Its Hold receipt is command acceptance, not independent proof of CPU stop.
The matched firmware must explicitly enable `fpgaload --computer-control`;
ordinary cold images reject its bridge opening. This diagnostic path grants
no data release or ordinary runtime admission and needs physical qualification.

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
fresh closed ownership before issuing private operation 8. It carries no
address or digest in the request. The secure provider independently checks its
fixed arena and compiled digests. Preparation and transfer do not implement
`BootHandoffVerifier`, admit a Nano programming profile or activate a core.
The board's first-load arena remains pinned until reboot, including after
successful DMA completion. Active drain and repeated runtime loads remain
separate qualification work.

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

`NewStoredConfigurationTransfer` consumes a `packagestore.Handoff`. It checks
the retained format-2 archive against the compiled package/build/target/ABI and
payload identities, compares the contained SD receipt to fresh secure ownership,
and only then reserves DMA memory. Deferred preparation rechecks the handoff;
resume or changed ownership cannot reach START. No filesystem is reopened and
no failed reservation/transfer frees memory. This source-bound path does not
supply the runtime compatibility decision for arbitrary packages.

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
