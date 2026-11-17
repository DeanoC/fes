# Native controlled-network startup wiring

Source `ed3cfbb17f9374c6070b84e7ceb7fe307349090f` connects the existing controlled
Nano Ethernet opener to the actual paired program launcher. The original boot
owner is independently captured from the accepted cold context before USB/SD
startup. The initial menu is constructed first, then `StartNetwork` binds the
qualified controller before backend/frontend ownership. A cached service status
is only compared for identity; packages cannot supply or replace the boot owner.
The board hook still requires the compiled MAC/PHY/DMA supplier. No shipped
profile is enabled; `nano/sd-profile.json` retains `network:null`.

`StartNetwork` derives a five-second startup context and a separate owned worker
lifetime from the caller's boot context. `OpenNetworkWithControl` passes startup
to XGMAC/firmware work and lifetime to `network.OpenDevice`. Startup cancellation
after successful return therefore does not cancel the IP stack. The lifetime
survives launch/return and is canceled after qualified terminal Park, ordinary
terminal cleanup or uncertain network failure. Worker cancellation supplies no
DMA fence, reset receipt or memory-release permission. Unknown startup, late
cancellation, missing/foreign control and platform panic retain resources and
retire without ordinary FPGA cleanup. A platform panic still propagates. The
opener must retain any partial resources that fail before it returns a control.

The initial source inspection missed the fact that `OpenDevice` inherits its
context for the full stack lifetime. Before repair, one selected parent job was
intentionally terminated by its exact recorded PID/start identity; all logs and
the initial cross-build are preserved as superseded evidence. No hardware was
used. The corrected regression starts the actual modeled Link with the supplied
lifetime, verifies that startup is canceled while the lifetime remains healthy,
then exercises launch/return and terminal Park. Failure tests cover original
cause preservation, cancellation, foreign controls, panic propagation and no
retry/extra firmware calls. Invalid owner, late frontend, canceled pre-admission
and absent opener reject before startup.

All seven selected software gates pass on the corrected source, including 1,935
parent cases and native lifecycle integration with actual patched TF-A C and
Verilated FPGA RTL under simulated bridges/memory/SDM. The network startup tests
run with the integration's race-enabled native tests. Those ownership models
are separate from physical MAC/PHY/DMA proof. The actual AArch64 SDK cross-build
also passes; all selected source hashes match and the generated profile retains
`profileNetworkEnabled=false`. This type-checks the real board/caller path but
does not execute an enabled NIC. `cross-build-proof.json` pins the ELF identity.

Both corrected and superseded artifacts, reports and source are retained at
`/home/deano/fes-retained/de25-native-network-startup-wire-20261111-r1`.
Archive hash/mode readback and independent FES source restore/fsck/pack checks
pass. The source bundle requires the named retained base; external native
compiler and module inputs remain toolchain dependencies. See the retention
proof and private manifest for exact identities.

No board command, FPGA programming, SD write, native deployment, matched boot-chain
build, PR, push, merge or old-output cleanup occurs here. The previously tested
`b3ed4389ad43d91b` image remains its own historical installation/reboot evidence.
This changed runtime requires a new matched chain and physical qualification
before claiming NIC coexistence or native network delivery. A native upload
listener/source integration, enabled MAC/PHY/DMA qualification and full
DE10-equivalent appliance/core compatibility remain open.
