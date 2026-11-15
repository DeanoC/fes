# Nano secure handoff, master stopping and network recovery

Follow-up: the [bounded TRWU8CBL Ethernet/SD trial](../2026-10-31-de25-ethernet-sd/README.md)
corrects this preparation's missing FAT boot FIT, passes Ethernet download and
SD scratch write/readback, and verifies factory restoration. The off-board
handoff/ownership qualification limits below still apply.

Base: `8dc3de51236e55a2c2b1ba775aec24ea4bcbe6e9`. This continues the
[owned SiP binding](../2026-10-31-de25-owned-sip/README.md). All work in this
record is off-board: source transforms, injected register/producer tests,
cross-builds and regular-file recovery media. No kit, card or flash is changed.

## Handoff and master callbacks

The C handoff receiver consumes a versioned record once per secure boot. An
independent secure policy must qualify its source identity, history, complete
master inventory and fabric-handshake capability. Owner and epoch must match
the live platform scope. The incoming record cannot grant qualification. A
failed attempt cannot retry; replay of a successful handoff cannot replace its
owner. No non-secure/SiP initialization operation is added.
Callback contexts must remain valid until reset. Calls run under the firmware
channel lock and must return within their bounds without reentering SiP/PSCI.

The master callback provider composes the existing bridge service. It stops
new requests for every named HPS producer, the other CPUs and all fabric
producers, then polls a separate quiescent/drained bitmap. Reset readback alone
does not count. Functional resets precede OCP interface resets, with ordered
readback and scope checks throughout. The real TF-A adapter binds the pinned
PER0 register, barriers and bounded delay. The existing bridge driver still
performs its mandatory hardware polls and propagates its first failure.

A completed stop can contain traffic before active bridges close. That receipt
is private to this provider and is cleared before opening control access.
Once data bridges are held, control-only access can arm a reader without
claiming fresh drain. Producer restart, changed epoch, incomplete reset or an
uncertain callback result retires the provider and prevents replay. PSCI
invalidation now retires the boot provider as well as bridge state.
HPS peripheral resets stay held after this sequence; there is no implicit
device resume on data-bridge release. The first native profile remains
serial-only. Native USB, SD and network need qualified per-device resume before
they can use this provider for full launch/return cycles.

The 26 standalone cases cover cold/active cycles, control-only recovery,
record/policy mutation, qualification gaps, unknown initial reset states,
quiesce errors, independent drain timeouts, scope loss, reset failures, late
producer restart and invalidation. They use undefined-behavior checks and
strict compiler warnings. The actual vendor-driver/SiP/RTL integration now
initializes through this handoff receiver; its 13 lifecycle cases, extra SiP
gate test and five tenfoot child cases pass with the race detector.
The affected run (`--base 8dc3de512 --jobs 4`) passes all 10 selected checks.
`make check` passes with 31 generated consumers and 37 fixture copies. Focused
recovery/profile/artifact rejection tests and documentation checks pass too.

Both controlled and strict Agilex 5 BL31 components cross-build. Their source
receipts are separate. The controlled component still has no qualified secure
board provider: its weak provider returns unavailable, and the service remains
`NOT_READY`. The SPL/Boot ROM history gap and physical producer-drain semantics
are not established by these host tests. No SPL measurement/attestation
producer is installed. The compiled interface and register adapters do not
authorize native hardware entry or a new physical programming profile.

The first standalone test found that active producer stopping was incorrectly
requiring the bridges to be closed already. The provider now records completed
quiescence before bridge closure and checks it again before publishing success;
the full launch/return integration passes with that correction. Failed logs
are retained.

## Updating the test card over a wire

Ethernet through a recovery U-Boot is the practical path. Altera documents
[TFTP-to-RAM followed by MMC writes](https://altera-fpga.github.io/rel-24.2/embedded-designs/agilex-5/e-series/premium/gsrd/ug-gsrd-agx5e-premium/#write-emmc-flash)
on its Agilex 5 development kit. This demonstrates the mechanism, not Nano
PHY/card acceptance. U-Boot also documents its [MMC block commands](https://docs.u-boot.org/en/latest/usage/cmd/mmc.html).
JTAG can start/recover the matching loader; no direct JTAG-to-SD writer is
supplied here.

The new `--network-recovery` build keeps vendor Ethernet/TFTP, SHA-256, FAT
writing and SD/MMC writing. It stops at a manual prompt, has no automatic boot
command or writes, uses no persistent environment, disables random MACs and
keeps SD in PIO mode. Supply a fixed board MAC and explicit host/IP settings
at the prompt. Native handoff and recovery are mutually exclusive profiles;
recovery requires the strict BL31, so it remains independent of the unready
owned service and its restricted SiP routes.

The recovery packager combines the retained Nano reference with this SPL and
creates a FAT32 test-card image as a regular file. It checks the Nano device,
configuration order, raw FIT, FAT geometry, backup boot sector and manifest
readback. The primary raw FIT is at sector 512. Space from sector 16384 to
32768 is reserved for a future experimental FIT; the FAT32 partition starts at
32768. No updater enforces dual-slot recovery yet, and no experimental SPL is
configured to consume that reserved slot in this change.

The next bounded hardware test can boot this recovery pair, establish a fixed
MAC/IP, receive a small known file, compare its hash, write an SD scratch file
and verify readback. This can prove the Ethernet and FAT-writing path while
leaving the raw recovery FIT intact. It needs one initial preparation of the
spare card. Later file updates can stay on the board. Updating the complete
SPL/FIT/firmware chain safely needs a checked experimental slot and matching
SPL; overwriting the only recovery loader is not an implemented update strategy.
The original factory card remains the rollback medium.

```sh
python3 platform/embedded/firmware/build_terasic_uboot.py \
  --reference-repo out/terasic-git-restore-uboot \
  --bl31-build out/nano-boot-strict-bl31-v1 \
  --cross-prefix "$PWD/out/bootchain-tools/uboot-bin/aarch64-linux-gnu-" \
  --network-recovery --jobs 4 --output out/fresh-network-recovery

python3 platform/embedded/nano/recovery_package.py \
  --uboot-build out/fresh-network-recovery \
  --vendor-sof /absolute/path/to/verified-nano-reference.sof \
  --quartus-pfg /absolute/path/to/quartus_pfg \
  --output out/fresh-network-recovery-kit
```

The retained toolchain environment is in the build receipt. The initial
recovery build selected the smaller TF-A cross-tool directory and failed its
`readelf` preflight; the recorded retry uses the existing U-Boot tool wrapper
directory and builds successfully. No checks were relaxed.
The initial image packaging attempt resolved mtools' executable symlinks to
the multicall binary, losing its command name. The corrected attempt preserves
the command names and passes actual format/copy/readback and image checks.

[evidence.json](evidence.json) records exact source and artifact identities,
validation and remaining board checks. Outputs are retained under
`/home/deano/fes-retained/de25-nano-trusted-handoff-20261031`.
