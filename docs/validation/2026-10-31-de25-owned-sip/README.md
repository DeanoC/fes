# Controlled Nano bridge SiP binding

Base: `2a0758a18bfdf7c38de205c7c1c5e138bbf18e52`. This continues the
[C bridge service](../2026-10-31-de25-nano-bridge-service/README.md) with an
opt-in BL31 binding and native Go wire adapter. No board, UART, JTAG or physical
card was accessed. The controlled firmware is a component for off-board study;
its actual boot setup leaves bridge ownership uninitialized.

## Result and boot-history boundary

The exact Terasic TF-A pin builds successfully for Agilex 5 with the controlled
service installed. The actual prepared SiP dispatcher and configuration engine
also pass native runtime and tenfoot launch/return and failure checks through
`FirmwareBridges`. Host tests inject trusted boot initialization, ownership,
registers, SDM and memory. They do not prove physical bridge drain.

The pinned path selects U-Boot SPL followed by BL31. TF-A's BL2 bridge-enabling
code is outside this selected build. The previous matched SPL configuration
has `CONFIG_SPL_FPGA` disabled and `CONFIG_SPL_ATF_NO_PLATFORM_PARAM` enabled.
These observations do not establish that bridges have never carried traffic.
The full U-Boot source can enable bridges after FPGA load, versioned SiP routes
can mutate bridges or submit mailbox commands, and PSCI reset code modifies
handshake state. No qualified boot-history handoff or complete master-stop
provider exists at this boundary.

[Altera's configuration-order documentation](https://docs.altera.com/r/docs/813773/26.3/device-configuration-user-guide-agilex-5-fpgas-and-socs/configuration-boot-order?contentId=YjpippjVMYa6y82j7lVn5g)
describes HPS-first versus FPGA-first sequencing. It does not record this
board's actual bridge history. The retained audit stores the exact pinned
source files, selected previous configuration and their hashes.

The installed setup reads and logs `BRGMODRST` but supplies no proof or
ownership callbacks. A private Snapshot therefore reports recognized
`NOT_READY`. Only qualified secure platform code may call the C-only bind
function; non-secure firmware calls cannot initialize ownership or claim
never-enabled history. The actual component has not been booted, so this is
source/build and host-model evidence of the intended unready behavior.

## Implemented control path

The generated [wire contract](../../../sources/mister-packages/docs/embedded-bridges.md)
uses private SiP number `0xc2000301`, cookie `0x46455342`, a reply tag, a
boot-scoped owner and a monotonic generation. That number is unused in the
inspected pins and outside their versioned dispatch ranges. Snapshot does not
query the older mutable `HPS_SET_BRIDGES` call: its zero operation could disable
bridges on old firmware. Unknown or malformed replies reject without retry.

The controlled top-level dispatcher admits only the private owned channel,
the existing read-only FES capability query and the four contained FPGA
configuration operations. Legacy bridge, alternate versioned and arbitrary
mailbox routes reject. Configuration and bridge operations share a lock and
the owned closed-state guard. Duplicate START and bridge mutation during a
configuration return BUSY; queued or submitted buffers block ISDONE. Engine
errors retire ownership. The public raw enable/non-graceful wrappers reject,
and disable becomes a closed-state guard. PSCI power/reset transitions
invalidate the service before changing state.

`FirmwareBridges` is the native `BridgeServices` wire adapter. The Nano SMC
transport admits its explicit private call, but the board boot path never
constructs it. Tests now use that production adapter and actual controlled
dispatcher, replacing the previous test-only plaintext bridge mutations and
duplicate snapshot decoders. `CPUHooks` and the existing controller retain
their containment, identity, deadline, reservation and recovery decisions.

## Validation

| Check | Result and scope |
| --- | --- |
| Controlled BL31 | AArch64 Agilex 5 component built; exact prepared service hash matches the host integration |
| Nano CPU image | Existing probe profile cross-builds with the new transport; no native execution |
| Standalone C | Owned wire cases and all 32 service cases pass with undefined-behavior checks |
| Owned SiP gate | Missing boot proof, legacy/versioned routes, invalid action, duplicate START, bridge opening during configuration and premature ISDONE reject |
| Native lifecycle | 13 cold/active/recovery/failure cases with actual vendor bridge functions and RTL; race detector enabled |
| Tenfoot actor | Five launch/return, identity recovery and bridge/ownership failure child cases; race detector enabled |
| Packaging gate | Explicit strict mode required; owned-unready, absent and unknown modes reject |

The host integration selects no sealed Nano artifact-dependent cases. Its
canonical package inputs are synthetic. It adds no physical programming profile
or production compatibility registry. FPGA endpoint RTL and generated FPGA
inputs are unchanged; no bitstream was rebuilt or requalified.

Failed and superseded attempts are retained with the final evidence. The first
BL31 attempt placed the helper before an early declaration and was corrected
to insert it before the actual handler definition. The host vendor subset uses
its existing warning policy; new codec/service C retains extra warnings and
undefined-behavior checks. Two new gate-test expectations were corrected to
follow the real engine: WRITE queues successfully, and completion first submits
then acknowledges the buffer. Neither required relaxing production checks.
Earlier successful builds using the old mutable call number are superseded and
are not current evidence.

The initial affected run completed 1,235 parent tests with 42 skips and one
failure: its consistency fixture still expected 29 generated files and omitted
the new definition. The fixture now expects 31 and includes that definition;
all 11 focused consistency tests pass. The original failure is retained.
The broad resumption reaches its 30-minute wall cap during Atari ST simulation,
after the preceding 13 commands return successfully. Additional core commands
run in separate groups, with shared Z80 build paths serialized; eight complete.

The new definition and oracle initially lack an affected-consumer registration,
so the conservative planner selects every shipped core. They now have an
explicit embedded-only registration. A regression checks the actual generated
consumer registry: exactly the C firmware header and Go adapter constants, both
owned by FES. Unknown neighboring definitions still select all consumers. The
final plan contains 11 commands and no shipped core simulation. All its checks
are covered by the completed prefix, the focused consistency repair and fresh
selection/runner tests. The remaining unrelated core work is cancelled and
recorded as incomplete supplemental work. Neither the initial failed run nor
the time-capped run is presented as a clean pass. The firmware and RTL checks
for the embedded control path remain required.

[evidence.json](evidence.json) records source hashes, exact output receipts,
checks and remaining qualification gates. The retained bundle is
`/home/deano/fes-retained/de25-nano-owned-sip-20261031`.

## Next integration gate

The `--nano-owned-bridges` build is explicitly `owned-unready`. U-Boot and card
packaging reject it. The default strict firmware transformations and physical
boot gate retain their behavior; the earlier stage-3 timeout remains open.

Before a runnable candidate can be selected, trusted boot/platform code must
establish bridge history, a fresh exclusive owner, complete master containment
and the optional fabric-handshake capability. It must then install qualified
ownership/producer-stop callbacks and bind them to the actual reset driver.
After that, rebuild the matched boot chain and revalidate its physical handoff
and drain on TRWU8CBL. These host results do not establish readiness for a
physical test.

Replay commands are in the [firmware README](../../../platform/embedded/firmware/README.md#controlled-nano-bridge-service).
