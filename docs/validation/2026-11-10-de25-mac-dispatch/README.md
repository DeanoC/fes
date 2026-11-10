# Owned firmware MAC routing and boot binding

The owned TF-A source export now installs the MAC engine, generated header and
entry guard. The real SiP dispatcher routes its private generation protocol
under the existing platform lock and selected CPU/MMU checks. Invalid cookies
and nonzero reserved X5/X6 receive an empty tagged rejection before dispatch.
No non-secure operation initializes the service or supplies device addresses.

Secure startup calls an unbound-by-default MAC supplier only after accepting
boot and existing device setup. An available provider must be contained at
generation zero, without a reservation, and match both the private accepted
boot and bridge owner/epoch. Missing, foreign, declined-after-initialization,
partially active or failed setup retires boot and retains uncertain resources.
The weak supplier returns unavailable, so current profiles grant no MAC access.

The MAC gate permits a small explicit list of read-only bridge snapshot/context/
pinmux and USB/SD queries only with a fresh confirmed scope. Their original
validators still apply. Unconfirmed MAC ownership blocks even those queries;
SD/USB mutations, bridge changes and FPGA configuration require containment.
A failed MAC boundary retires shared bridge authority and prevents subsequent
callbacks. No bridge, USB or SD generation is replaced by the MAC generation.

Portable tests compile the producer's actual entry-guard literal and execute it
with the real shared dispatcher, MAC engine, cold/boot/USB/SD/bridge implementations
and explicit modeled MAC grant/drain. Seventeen setup cases compile the actual
platform-setup body with injected boot callbacks; ten dispatcher cases cover
coexistence, reserved arguments, unbound/busy/foreign/CPU and drain failures.
The native client and descriptor/IP-stack HTTP tests now call that same owned
dispatcher across three stop/restart cycles. The cold boot identity inventory now includes the MAC engine/header/contract.
The owned-source and observed-source SHA gates were updated only after
regenerating both from the unchanged pinned Terasic Git blob and reviewing the
complete diff. The old source exactly matched the prior gates. The retained
`source-transition.json` and `owned-source.patch` bind that transition; vendor
transport functions and instrumentation edits remain unchanged.
The first test harness attempts
lacked its SMC_RET1/stdlib declarations and verifier import path; those fixture
errors and their corrected results are retained. The initial full Nano run also
exposed the missing cold-fixture instance and boot identity membership; both
were added, retaining the original failing run. No device-driver fallback or
firmware guard was weakened.

Base is `1f35b4961da8efcb1d3c86929dfefa03f89da2e0`; tested source is
`fed4e4e35b372321970efab495161c3cec804d4d`. All twelve affected checks pass, including parent/Nano
regressions, SD/FAT, native runtime/producer, board control, firmware/RTL and
FPGA Python integration. Focused native race, setup/dispatcher and exact-source
binding tests pass. The committed generated-consumer check passes.

The fresh entry/BL31 pair passes with firmware RAM/MMU checks intact. All
2122 retained native SDK input hashes, 45 entry sources and
128 firmware producer inputs match the live tree. SDK ELF remains
`bcdc2e9195253904259d22969a0252f3e090bca667015c6cbb34034a70940a31`. Entry image SHA-256:
`1d5756babcd2a092c7391b6c3df0c65189c128b4544b80fcfecfb075fc7149c3`. BL31 binary SHA-256:
`09c64d6f0d45396968ac687771990e6f2c91d7ca22dd7360374f4ba8878ec4c4`; ELF SHA-256:
`c594480b97ddc428db363b89e22beacbde2d87bb4a98839971f3a95d0c90d656`. The linked weak default setup is exactly
`mov w0,#0; ret`; the MAC object is 128 bytes at OCRAM `0x5dfe8`, within
the existing BSS exclusion. It supplies no physical provider.
The linked check's initial helper attempts lacked its tool-library environment
and used an incorrect ELF path; corrected inspection uses the build's environment
and canonical nested ELF. Both original errors remain retained.

Entry and firmware hashes match each other. SPL, FIT and SOF were not rebuilt;
this pair is not a deployable or boot-qualified complete chain. Network stays
disabled and SD read-only. Evidence and `validation.json` are under
`out/nano-mac-dispatch-20261110/`.
No kit, card, JTAG/UART, remote builder or cleanup operation was used. Hardware
remains paused until the operator returns. A source-bound physical MAC/clock/
PHY/SMMU supplier, independent interconnect drain and active-core coexistence
still need implementation/qualification before Ethernet is enabled. This export
routing does not establish that physical authority. Keyboard/HDMI, writable SD,
broader packages and full DE10-equivalent appliance acceptance remain open.
