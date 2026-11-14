# Nano maintenance containment and inherited DDR probe stop

The Linux-free DDR diagnostic now holds its three maintenance JTAG Avalon
managers in reset, including the coherent HPS memory requester. A separate C
helper implements the inherited read-only DDR probe's part of master stop.
This supplies source/generation and RTL-model evidence. No board was accessed,
no SOF/RBF or sealed package was produced, and no physical data bridge opened.
Physical DDR mapping, complete master containment and runtime admission remain
unqualified.

The base is `4534d74bad19203851f4617c29546dc810b80742`. The generated-project source is `97d31dc534c4f0bcd429aa895c5c2462bee6f8ae`.
Final tested source is `e5aa8d617f9879587f00bc01d903a9848a3f2b3b`. The final delta changes only two parent test
expectations for the extra generated firmware header:
[source-delta.json](source-delta.json). All producer, firmware and generated
consumer bytes are identical across that delta.

## Resulting behavior

The DDR backend exports a constant asserted maintenance reset, independent of
CSR writes, local reset and clocks. Its generated route replaces only the
maintenance subsystem's incoming reset. All three manager resets and their
polarity are checked in the generated HDL: [reset proof](generated-reset-proof.json).
Read-only control register `0xb8` reports the local hold output. The default
on-chip backend retains its original maintenance reset route. Control/address
maps are preserved, while DDR mode's maintenance managers remain held; the SDM
programming/recovery path is separate.

The HPS DDR adapter still has the embedded reader, inherited DDR probe and
maintenance extender as incoming connections. The maintenance reset contains
all three upstream JTAG managers, including their separate coherent HPS path.
The legacy DDR probe retains its independent control map and drain requirement.
The existing controller separately owns the main reader's generation/cookie
shutdown. The hold indicator alone cannot supply global containment.

`nano_probe_stop.c` accepts qualified fixed-aperture callbacks and an exclusive
boot/reset/bridge-scope predicate. Two identical snapshots must match the
known probe identity/version. An idle probe is observed without a command.
An active probe receives CANCEL once; held addresses and accepted responses
remain outstanding until real RTL reports IDLE and equal accepted/completed
byte counts. Faults, inconsistent state, changed scope or operation identity,
uncertain I/O and a 128-poll timeout retire the helper. A completed receipt
can be checked again only against identical fresh state. It performs no
reset, bridge mutation, allocation or memory release. The physical cold
provider does not install this helper or permit DDR release.

The existing DDR probe definition now also emits the firmware C constants.
No definition, ABI or programming registry changed. `make generate` updated
that consumer; real consistency verifies 35 generated files and 37 fixture
copies. Both explicit parent inventory expectations have matching checks.

## Validation

All final checks pass with the scopes below; [summary.json](summary.json)
records their classification.

| Check | Result |
| --- | --- |
| Actual Quartus Pro project generation | Both variants pass; every vendor call bounded to 120 seconds: [DDR](generation-ddr.json), [on-chip](generation-onchip.json) |
| Generated handoff mutations | 17 DDR and 4 on-chip alterations rejected: [DDR](handoff-ddr.json), [on-chip](handoff-onchip.json) |
| Project RTL models | Both pass; DDR has 23 transfer/error cases and 3 identity/window rejections: [DDR](model-ddr.json), [on-chip](model-onchip.json) |
| Actual C stop helper and Avalon probe RTL | 129 cases, including all 108 read-failure positions: [report](probe-stop.json), [log](probe-stop.log) |
| Generated CPU control fabric | Three CPU reads and three writes with maintenance reset held, including backpressure: [report](control-fabric.json) |
| Affected software campaign | 11 commands covered; 10 executed on final source, one unchanged FPGA regression reused from the producer head: [plan](software-plan.json), [results](software-result.json) |
| Parent regressions | 1,265 tests pass, 2 skipped, on final source |
| FPGA regressions | 1,576 tests pass, 75 skipped, on the unchanged producer source head |
| Generated consumers | 35 files and 37 fixture copies match: [consistency](generated-consistency.log) |

The C/RTL cases cover idle/completed probes without writes, stalled addresses,
accepted bursts, missing responses, real Avalon response errors, wrong
identity/version, unstable or inconsistent counters, ownership loss, possibly
issued failed writes, reset/sequence loss and no replay after failure.
No timeout is treated as drain.

The handoff mutations include missing, duplicated or bypassed maintenance
reset, a live coherent manager, an additional DDR manager, changed source,
write pins, memory parameters and wrong routes/maps.

The extra generated-control smoke uses a private synthesis-view copy: only
vendor `synthesis translate_off` blocks are omitted because their simulation
assertions are unsupported by Verilator. Generated deliverable RTL is unchanged.
The exact transformations and lint exclusions are recorded in the
[synthesis-view audit](control-fabric-synthesis-view.json) and
[command](control-fabric.json). This proves modeled control ingress, not
physical CPU access, general timing or DDR acceptance.

Earlier failed attempts remain locally retained: the reset-wiring command
used the wrong argument form, and parent inventory expectations named the
previous consumer set/count. Failed generation and regressions are not
reported as passed. The reused FPGA regression remains attributed to its
actual source head; unchanged FPGA inputs were not retested after the
parent-expectation-only delta.

## Next physical integration

The generated DDR window is the proposed bus range `0x95000000..0x95002000`.
It is not a CPU mapping, live allocation, SMMU bypass claim or firewall grant.
No CPU reservation is created by these FPGA parameters.

Next, bind an explicitly reserved native CPU window to the qualified FPGA
address domain, combine the reader/probe/maintenance results with every HPS
master callback, and extend trusted cold ownership for the fixed DDR diagnostic.
Then build a matched sealed boot chain and run its bounded board test.
Ordinary package admission, repeated native core loads, physical launch/return
and peripheral/UI acceptance remain open.
