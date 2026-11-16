# Native bridge capability and ownership contract

Base: `8354b3088bbcf12dfe0c73e07b5da8631a72c462`. This implements the
platform-hook portion of the [acknowledgement investigation](../2026-10-31-de25-ack-wiring/README.md).
It is a hardware-independent native runtime prototype. No board was accessed,
no physical media was written and no firmware implementation was changed.

## Implemented behavior

`CPUHooks`' existing platform services now require `BridgeServices` instead of
three boolean bridge callbacks. The lifecycle controller and its endpoint
generation/cookie drain remain authoritative. Package compatibility still
uses the existing admission oracle, retained descriptors and live build-ID
checks; no shared wire definition or physical compatibility registry changed.

The platform supplies explicit bridge/control/hardware-drain masks and a
present/absent optional fabric-handshake capability. Unspecified capability,
empty/invalid masks, zero ownership or a zero generation reject. These are
trusted platform facts qualified against the actual booted firmware and
HPS/FPGA configuration, not claims accepted from a package.

A stable observation includes a boot-scoped exclusive owner, a monotonic
change generation, reset readback and master-containment state. The platform
must observe every relevant change, even reset-then-restore with identical
final bits. Every mutation compares its expected prior observation and
publishes the next generation. The hooks validate both its result and a fresh
observation before retaining the result.

| Path | Implemented requirement |
| --- | --- |
| Entirely closed, never enabled | Trusted boot-chain evidence plus containment of every master; no shutdown/handshake request |
| Entirely closed by these hooks | Reuse only the unchanged owner, generation, capabilities, reset bits and completed closure |
| Initially active | Stop all producers; require hardware drain; require the optional fabric ACK only when its qualified capability is present; assert and verify all reset bits |
| Unknown closed or initially partial reset | Reject; reset bits are insufficient evidence |
| Control-only access | Require owned closure, open exactly the control mask, and verify all data paths remain reset |
| Data release | Require the owned control-only state and the existing endpoint's identity/reader-enable sequence, then verify the requested release |
| Failure, stale ownership or unexpected readback | Retire the CPU hooks; the lifecycle enters reboot-required and cannot replay the operation |

The owner-bound control-only state can be closed again for bounded idle
recovery after an identity mismatch. This still requires a complete closure
result, including hardware-drain evidence. It does not grant an unknown
partially reset state the same treatment.

Before every configuration START, WRITE, COMPLETED_WRITE and ISDONE SMC, the
CPU hooks recheck the owned closed state. An absent optional fabric responder
does not waive either hardware drain or the endpoint's outgoing reader
generation/cookie acknowledgement. Unresolved transfers retain their DMA
reservation under the existing endpoint/controller rules.

## Validation

The focused tests cover both handshake capabilities, cold and retained closed
states, independent producer/fabric/hardware failures, ownership-generation
changes, missing master containment, reset/mask drift, readback failure,
generation exhaustion and invalid release order. Full native lifecycle cases
cover boot, launch, return, identity recovery, failed control/data opening and
lost transfer completion. A pending outgoing read prevents containment and
programming even when the optional fabric handshake is absent.

The native integration verifier rebuilds the selected Terasic TF-A function
bodies and the actual Verilator FPGA endpoint, reuses the production admission
oracle and canonical synthetic package/ROM fixtures, and runs the Go adapter
and tenfoot actor with the race detector. Tenfoot's launch/return and identity
failure now go through `CPUHooks` and this bridge contract. Actor-level missing
hardware drain, missing fabric ACK and changed ownership stop before a second
programming attempt and retain reboot-required ownership.

The bridge service replies, memory, CPU aperture and SDM remain simulated.
The artifact-dependent sealed Nano tests are not exercised by the generic
native verifier when their external package is absent; their adapters were
updated to the same required interface. See [evidence.json](evidence.json) for
the exact source hashes, checks, skips and retained records.

## Remaining physical integration

There is no Nano `BridgeServices` implementation and no default physical
binding. The selected Terasic firmware's unconditional bridge polls, firmware
capability tuple and native boot rejection gate are unchanged. In particular,
this implementation does not bypass the measured stage-3 timeout or prove the
prebuilt SOF's internal ACK wiring.

The next integration is a board/firmware adapter that can establish the actual
never-enabled boot history or execute the active close contract, select the
qualified optional-handshake path, independently prove hardware drain and
observe exclusive ownership across firmware operations. That adapter needs
its own hardware-independent register tests before another bounded board test.

## Replay

```sh
cd platform/embedded/tamago
GOWORK=off go test -race -count=1 ./nativefabric ./nativefabriccheck ./modelbackend
cd ../../..
python3 platform/embedded/tamago/nativefabric/verify.py \
  --reference out/terasic-git-restore-tfa/plat/intel/soc/common/socfpga_sip_svc.c \
  --reset-reference out/terasic-git-restore-tfa/plat/intel/soc/common/soc/socfpga_reset_manager.c \
  --output out/fresh-bridge-contract-check
```

The first command skips integrations requiring an RPC executable or external
package. The second builds its own RPC/fixtures and exercises the generic
native/tenfoot integration; its output directory must not already exist. The
affected-change suite also passed, including the upstream-v2.12 native
integration, parent Python regressions and generated-consumer consistency.
