# Native contained-fabric endpoint

This experimental Go endpoint connects the existing `fabricmodel.Controller`
to injected firmware calls, the generated FPGA identity/drain registers and
platform-owned reservations. The optional `modelbackend.NewWithEndpoint`
constructor drives it through the same actor and retained-package admission
used by tenfoot's model sessions. It remains restricted to the
`qemu-model-only` policy. The Nano has no registered physical programming
profile or automatically enabled board binding.

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
   full-configuration START/WRITE/COMPLETED_WRITE/ISDONE wire protocol. One
   write is outstanding at a time; accepted BUSY is never replayed. CPU and
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

`CPUHooks` composes an explicitly supplied absolute CPU control aperture and
SMC transport with the platform's containment, save and memory services. It
checks aperture alignment, overflow and control-register bounds before an
access. Nano provides an unconfigured AArch64 transport using its ordered MMIO
and existing SMC assembly; the boot path never constructs it. A cross-build does
not qualify an address, and a bus fault or stalled SMC cannot be recovered by
the transport. There is no default base address or bridge service.

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
