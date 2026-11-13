# Linux-free DE25 experiment publication

This change preserves the exploratory base behind
`FES_EXPERIMENTAL_DE25_EMBEDDED=1`. It does not select it for ordinary builds,
images or releases. The historical development branch is
`codex/de25-embedded-os` at `d580a60d93ad5dad87f0c311b762d0df2bd2beb5`.
The publication integrates that history with main at
`f2c2e82541a5ad448deb1acf66a31623fb786768` and promotes the latest retained
firmware source into `platform/embedded/firmware`, without generated boot
binaries or machine-local execution approvals.

The common code includes the injected Tenfoot loop/model backend, retained
package/ROM admission, native CPU/firmware/FPGA ownership hooks, FAT32/SD PIO,
USB keyboard/hub and Ethernet prototypes, matched boot preparation and
failure-contained launch/return models. Standalone C and RTOS experiments
remain for comparison. The normal Linux build retains its current programming
profiles and video-status contract; the native experiment uses separate
`de25_embedded` and `fes.fabric.embedded-computer` definitions.

## Evidence boundaries

The dated earlier records retain their exact source and artifact identities.
They include bounded physical ZX81/ZX80 launch/return, keyboard interaction,
visible launcher return, SD installation/reboot discovery and Ethernet-to-SD
readback. They are evidence for those artifacts, not physical acceptance of
this main-merged tree or general Linux feature parity.

The latest source-retirement diagnostic used chain
`b8f89ef622f07cce3b5091f22d516f3f8e9a1c3de71b0ff7851e10373b74d647`
and FIT SHA-256
`2ba50ae7fd42f25c07990ab5b56943f8610e343522069ef6bae590e212faf5d0`.
On TRWU8CBL it completed 1,708,032 bytes in two posts/two acknowledgements,
with the complete SHA-384 source binding. Retirement then failed in phase 4
(command invalidation), first error -3, after three control writes and before
queue publication. The sampled phase lasted 109,542 us, beyond the existing
100 ms command limit; that limit is preserved. Source memory stayed retained
and activation/reuse were not authorized.

After restoring the exact original FIT, warm baseline stop command 47 failed
because the fabric drain acknowledgement was absent. Firmware retained
resources and required a reboot. A separate physical reset and baseline-only
restoration completed a fresh 51-command ZX81/ZX80 launch/return/Park cycle,
generation 5. This does not qualify warm recovery from the diagnostic.

Retained failure archive SHA-256:
`3909d7e25b4b523ee2f0e558c79e8a63aff4a8c627def1720ae7bccd314e4434`.
Separate baseline restoration archive SHA-256:
`89a2625a40dc045e4ba80349dd282ae107db5e9201e397d9b5e395dd1c35cbfe`.
These archives remain in the retained evidence store; generated images are
not shipped as source fixtures.

## Remaining work

Resolve source retirement and the post-diagnostic fabric drain failure before
attempting ordinary package activation through this experimental path. Then
rebuild a complete matched chain from the published source and qualify its
launch/return and recovery on TRWU8CBL. Broader core/device support, controller
and Bluetooth support, durable updates, multicore startup, signed secure boot
and appliance-level failure recovery remain incomplete. Neither passing host
tests nor source-bound receipts establish those properties.

The publication does not program or reboot a board. The original native
baseline remains parked on TRWU8CBL, with its test card installed. Factory
media, QSPI and the bottom kit are outside this change.
