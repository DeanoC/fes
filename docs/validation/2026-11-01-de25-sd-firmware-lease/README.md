# DE25 firmware-owned SD lease and native PIO session

This change connects the actual C ownership service and Go PIO lifecycle through
an experimental SD lease. It provides host integration and ARM64 component-build
evidence. It performs no board operation and supplies no physical SD, FPGA
programming or core-load acceptance. The last accepted physical native Go boot
is recorded [separately](../2026-11-01-de25-native-go-boot/README.md).

The base is `4f559cc68d387dfb807bb7ab351ac6fe331bf89b`. The
[shared contract](../../../sources/mister-packages/docs/embedded-sd.md) is generated
into C and Go from the same YAML/oracle. Secure initialization supplies qualified
boot scope, reset, clock/PHY, pinmux and independent PIO-stop operations. The
current owned and cold serial producers install the channel but leave it
NOT_READY; a non-secure caller cannot initialize it.

The service shares the bridge owner and generation. Resume grants controller
access before the first native MMIO. Prepare runs after host reset/internal-clock
stability and before card clocks or commands. Quiesce fences native reads before
firmware independently drains and contains SD. Only after stop/readback and the
old cold ownership checks succeed does Stop publish containment and the new
generation. While SD is leased, bridge snapshots/mutations and configuration
return Busy without consulting the old all-master cold callbacks. Data bridges
remain reset throughout this initial PIO mode.

The Go Session wraps the real `sdhci.Device`. Resume requires the same CID and
capacity, allowing a retained FAT volume to be reused under unchanged exclusive
media ownership. The last known receipt survives failures, marked invalid; an
uncertain grant, firmware reply, stop, initialization or card change retires the
session without automatic stop/retry or adoption of an active lease.

Validation completed before the repository-wide affected checks:

- C engine/owned dispatcher tests under undefined-behavior sanitization cover
  lease generations, stale/foreign/repeated requests, active/inactive scope loss,
  partial callbacks, restored cold scope and permanent failure retention.
- The real C dispatcher and state engine communicate with the Go Session and
  actual PIO driver in race-enabled tests. Controller/card registers and secure
  platform operations are modeled. Read/stop/resume bytes, card changes, failed
  preparation, driver/firmware stop errors, lost grant responses and unready
  service denial pass. The retained FAT32 lifecycle tests pass too.
- Nano parent fixtures pass, including early-capture/PSCI protections and the
  explicit check that the serial profile cannot enable SD.
- The existing owned SiP/vendor bridge/FPGA RTL integration and tenfoot
  launch/return/identity/bridge-failure checks pass with injected hardware.
- A complete owned-unready Terasic Agilex5 BL31 links the new service. All its
  producer source hashes match the final implementation. This is an unready
  component, not a matched cold boot chain.
- The TamaGo ARM64 SD session library compiles, with all 555 selected source
  files matching its receipt. This is a library archive, not a bootable payload.
- Generation and planner regressions pass. Affected checks now select the
  C/Go SD PIO suite for its reviewed implementation/contract inputs.

`result.json` binds exact build/test receipts and artifact hashes. Raw focused
logs are adjacent. Full receipts, source selections, BL31, SDK archive and the
owned firmware/RTL test outputs remain in the private `out/nano-sd-lease-*`
directories and the retained evidence bundle.

Initial failures remain in `out/nano-sd-lease-20261101`: the old cold host fixture
needed the new object/global linked, the first SDK invocation ran before entering
the module directory, and `make check` correctly refused dirty shared contracts.
The fixture and invocation were corrected; committed-source checks follow the
implementation commit. None of these attempts accessed hardware.

Next integration is the real Nano scope/reset/ComboPHY/independent-stop callbacks
and an explicit controlled-SD profile, followed by a rebuilt matched native entry
and cold boot chain. First hardware storage proof should read and compare the
existing `FES/RECOVERY.JSON`, quiesce/contain, resume the same card and compare it
again. Concurrent core/SD access and coordinated adoption of device generations
by the native fabric actor are still pending. Programming features remain zero
in the current cold serial profile.
