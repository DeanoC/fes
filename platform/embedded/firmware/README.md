# Contained firmware and FPGA control prototype

This is hardware-independent source and integration work for the Linux-free
base. It supplies a pinned TF-A source transformation, a freestanding C++14
control adapter, and verification against actual FPGA RTL. It is not a Nano
boot image, a TamaGo programming binding or hardware acceptance.

The shared [contract](../../../sources/mister-packages/docs/embedded-control.md)
owns the register offsets and private firmware query. Its generated C++ and
Verilog consumers come from `make generate`; do not edit them by hand.

## Firmware source

`prepare_tfa.py` accepts only two reviewed versions of
`plat/intel/soc/common/socfpga_sip_svc.c`: upstream TF-A v2.12 blob
`3c223c1498e3a412e075e4f921cd78acd3d0fcd6`, and the Terasic DE25 Nano v2.12
blob `288baffb283787dbd49729983800ce764243adda` from commit
`580dcb5c4e10aa7daff2bf69d493c076a6d3e665`. The board's boot log identifies a
Terasic v2.12 build, but the source tag alone does not prove the exact firmware
bytes currently running. The transformer writes a fresh private directory under
this worktree's `out/`, preserving the source's BSD copyright header. It does
not patch a shared checkout, fetch a board image or deploy firmware.

The contained variant:

- Rejects partial/authentication flags; full configuration uses flag zero.
- Disables bridges and checks the result before CANCEL/RECONFIG.
- Checks CANCEL failure and RECONFIG response shape/zero credits or block size.
- Makes ISDONE leave every data bridge closed, including repeated status polls.
- Handles START's null remapper argument while preserving WRITE's CPU-to-SDM
  relative-address conversion.
- Exposes the local experimental capability query documented in the contract.

The Terasic source additionally sends an SDM isolation request during START.
The contained variant rejects unsupported flags and disables bridges before
that request, and treats an isolation failure as an error. The vendor's BUSY
response carries SDM progress in an extra word; the private profile normalizes
that word only for BUSY, while preserving error details. Its query is handled
before the vendor's versioned SiP dispatch. Both sources are tested against
the same native lifecycle and RTL model; the Terasic checks additionally cover
isolation ordering/failure, progress status and actual SiP dispatch.

The pinned Terasic
[bridge reset implementation](https://github.com/terasic/arm-trusted-firmware/blob/580dcb5c4e10aa7daff2bf69d493c076a6d3e665/plat/intel/soc/common/soc/socfpga_reset_manager.c#L391)
reuses return variables across enable and disable handshakes. A later successful
poll can overwrite an earlier timeout. The Terasic variant also transforms that
exact reset-manager source to preserve the first reported error, and tests the
actual vendor and modified functions with simulated handshake results. Those
tests do not establish that physical bridges or their traffic have drained,
or that a partial enable can be safely reversed. The bounded
[Nano bridge trial](../../../docs/validation/2026-10-31-de25-bridge-trial/README.md)
returned firmware error `4` with the required reset bits already set. Native
handoff and physical drain remain unqualified.
The [subsequent firmware-stage trace](../../../docs/validation/2026-10-31-de25-firmware-first-failure/README.md)
identifies stage 3, `f2s_fpga_ack_set`, with `-ETIMEDOUT` (`-60`): the initial
FPGA acknowledgment was absent while the request bit was set. The board trace
alone did not establish the reason for the missing acknowledgment.
The [reference-source investigation](../../../docs/validation/2026-10-31-de25-ack-wiring/README.md)
finds that the vendor source disables the optional fabric handshake while the
Agilex5 routine requests it even for an already-reset bridge. Its actual-C
model reproduces the failure; this does not qualify skipping hardware drain.

The contained Terasic probe now records the first failed disable poll and
immediately samples `HDSKACK`, `HDSKREQ` and `BRGMODRST`, before subsequent
writes/polls. Later errors cannot replace the first snapshot. The existing
`HPS_SET_BRIDGES` service clears this record before each call. The six
Agilex5 stages use the pinned reset-manager definitions:

| Stage | Name | Required `HDSKACK` mask/value |
| --- | --- | --- |
| 1 | `s2f_ack_clear` | `0x400 / 0` |
| 2 | `lw_ack_clear` | `0x200 / 0` |
| 3 | `f2s_fpga_ack_set` | `0x4 / 0x4` |
| 4 | `f2s_flush_ack_set` | `0x1000 / 0x1000` |
| 5 | `sdram_flush_ack_clear` | `0x800 / 0` |
| 6 | `sdram_fpga_ack_clear` | `0x4 / 0` |

Only the dedicated local probe requests extra return registers by passing
`FES_NANO_BRIDGE_DIAG_REQUEST` in the normally unused `x3`. The original `x0`
status is unchanged. Ordinary callers retain the original `SMC_RET1` path.
This is an opt-in diagnostic extension of the contained Terasic source, not
a change to the shared programming/capability contract or upstream TF-A.
The version tag and packing are defined in `nano_bridge_diag.h`:

- Returned `x1`: tag `0x46424431` in the upper 32 bits, first stage below it.
- Returned `x2`: signed 32-bit error above the sampled `HDSKACK` value.
- Returned `x3`: sampled `HDSKREQ` above sampled `BRGMODRST`.

Stage zero means no failure snapshot; zero snapshot words do not mean those
registers were read as zero. IDs 7/8 describe the vendor's non-Agilex5 NOC
branches and are not executed by this candidate. The snapshot consists of
separate reads after the failed poll, not an atomic capture or a substitute
for the original error. Poll counts/timeouts, reset writes and first-error
propagation are preserved. The dedicated loader rejects unavailable or
inconsistent diagnostic identity/state even if the ordinary checks pass.
The profile assumes the existing exclusive trusted-runtime sequencing.

It preserves accepted-BUSY and completion-address semantics. The existing
`HPS_SET_BRIDGES` service still exists; this is an exclusive trusted-runtime
sequencing contract, not a security boundary against another SMC caller. No
production programming registry is changed. Other source versions reject.

```sh
python3 platform/embedded/firmware/prepare_tfa.py \
  --reference out/reference/socfpga_sip_svc.c --output out/contained-tfa
```

The validation record includes a successful `PLAT=agilex5 bl31` AArch64 build
from the actual TF-A v2.12 tree with this prepared source. BL31 alone does not
configure Nano DDR, select its UART/PHYs or provide a complete boot chain.

## Controlled Nano bridge service

`nano_bridges.c` implements the owner/generation checks behind the native
platform's bridge contract. Its operations are injected and must be serialized,
bounded-return and qualified by trusted boot/platform code. There are no default
MMIO addresses, ownership callbacks or never-enabled boot proof. Unknown closed
and partial initial reset states reject. Lost ownership, incomplete producer
containment, driver errors and changed reset/master readback retire the service.
Control-only and data opening validate their exact masks and order.

Configuration uses `fes_nano_bridges_configuration_guard` to check an already
owned closed state without issuing another shutdown. In control-only recovery,
data paths stay reset: the service closes only the control path and explicitly
returns retained data containment, with zero fresh fabric/data ACK claims.
An active data shutdown still requires the full hardware-drain sequence.

The opt-in `nano_bridge_driver_source` transformation preserves the pinned
vendor driver and first-error checks while gating only optional fabric warning
writes/polls on an injected, qualified capability. It covers enable as well as
disable. Hardware flush/fence polls stay mandatory. The normal source
transformations and default `build_terasic.py` mode do not select this driver
or service.

```sh
python3 platform/embedded/firmware/verify_nano_bridges.py \
  --reference out/terasic-git-restore-tfa/plat/intel/soc/common/soc/socfpga_reset_manager.c \
  --output out/fresh-nano-bridge-service
python3 platform/embedded/tamago/nativefabric/verify.py \
  --reference out/terasic-git-restore-tfa/plat/intel/soc/common/socfpga_sip_svc.c \
  --reset-reference out/terasic-git-restore-tfa/plat/intel/soc/common/soc/socfpga_reset_manager.c \
  --nano-bridge-service --output out/fresh-nano-bridge-integration
```

The first command executes the actual driver C with injected registers. The
second exercises the controlled SiP dispatcher, C service and driver through
the production Go wire adapter, existing controller and tenfoot actor, with
actual FPGA RTL. Register, ownership, memory and SDM responses are models.
See the [service validation](../../../docs/validation/2026-10-31-de25-nano-bridge-service/README.md)
and [SiP binding validation](../../../docs/validation/2026-10-31-de25-owned-sip/README.md).

`prepare_nano_owned.py` installs the opt-in controlled channel in a fresh export
of the exact Terasic pin. The [shared wire contract](../../../sources/mister-packages/docs/embedded-bridges.md)
defines a private call distinct from the older mutable bridge call. Querying
the service cannot disable bridges on the inspected older firmware. The
controlled dispatcher admits only that channel, the existing capability query
and the four contained configuration operations. It blocks alternate legacy,
versioned and arbitrary-mailbox routes. Bridge and configuration operations
share a lock and the same ownership guard; an outstanding configuration blocks
bridge mutations, and pending buffers block ISDONE. Public raw bridge wrappers
cannot bypass that guard. PSCI power/reset transitions invalidate ownership;
the cold serial profile instead denies CPU_ON without side effects, as below.

```sh
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/terasic-git-restore-tfa \
  --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --nano-owned-bridges --output out/fresh-owned-bl31
```

This builds a controlled BL31 component, not a bootable candidate. Its default
secure provider supplies no evidence, so Snapshot returns the recognized
`NOT_READY` reply. Reviewed secure boot code can provide a record, independent
qualified platform policy and complete producer callbacks through
`fes_nano_platform_boot_record`; `fes_nano_bridge_platform_handoff` consumes
that evidence once. Non-secure callers cannot initialize it. The pinned HPS-first path
does not establish the required never-enabled history or whole-board master
containment. Closed reset bits alone cannot supply that proof. The default
host tests inject trusted initialization. The optional cold serial build below
installs a separate actual SPL producer and strong EL3 consumer.

`nano_boot.c` validates the record's chain identity, owner, epoch, exact version
and complete producer inventory against independent secure policy. The record
cannot grant its own qualification. Unknown/partial initial state, missing
qualification and failed/replayed handoffs reject. Consumed inputs are copied;
later caller mutation cannot change the authority. This is a secure C interface,
not a signed boot attestation or a new SiP initialization command.

Its master-stop callbacks first stop new requests and require an independent
drained-producer bitmap for all HPS masters, other CPUs and fabric producers.
Only then do they assert functional resets followed by OCP resets and check
fresh readback. The installed TF-A adapter supplies the real PER0 register
operations, barriers and bounded delays. Board-specific ownership, quiesce and
drain observation remain unqualified; no default fabric/master proof exists.
Control-only access may arm a new reader while data bridges remain held; an
active close instead needs a fresh producer-stop receipt. Ownership loss,
timeouts, restarted producers and readback failures retire the provider.
HPS peripheral resets stay held: this provider targets the serial-only native
probe. Qualified per-device resume is still required for native USB/SD/network
operation after programming. The separate recovery loader does not select it.
See the [handoff and recovery validation](../../../docs/validation/2026-10-31-de25-trusted-handoff/README.md).

Build receipts distinguish `strict`, `owned-unready` and `owned-cold-serial`.
Recovery packaging requires `strict`; the native loader now requires its exact
matched `owned-cold-serial` profile. Both reject the default unready variant.
Earlier producer receipts remain historical evidence.

### Closed-bridge SD lease

The [SD wire channel](../../../sources/mister-packages/docs/embedded-sd.md)
shares the bridge owner and generation. `nano_sd.c` requires secure-only
initialization with scope, controller resume, clock/PHY preparation and
independent drain/reset callbacks. A leased SD controller blocks bridge
snapshots, mutations and FPGA configuration before all-master cold callbacks.
Successful Stop restores the old cold scope before publishing containment;
any uncertain transition retires both services and retains the lease.

The owned dispatcher recognizes the SD channel under the same platform lock.
Default owned and cold serial builds leave it NOT_READY; they install no
SD authority. The [native session](../tamago/sdcontrol/README.md) binds the
actual PIO driver to this service through injectable transport/registers.
C/Go tests inject scope and MMIO and exercise read/quiesce/resume, changed cards
and retained failures. `nano_sd_platform.c` supplies the actual Nano callbacks;
`nano/sd-profile.json` selects them in the matched cold chain. Secure setup
verifies the original owner, closed bridges, retained CPU/master scope, direct
SD pin routing and live 200 MHz clock derivation before touching a contained
controller. Resume releases only the SD controller and its ComboPHY reset,
checks reset readback and applies the pinned Nano PHY timing. Prepare repeats
that timing after the Go host reset, before enabling the card clock. Stop
independently checks DMA/interrupts/errors, drains PIO, stops the clock and
asserts/readbacks only the controller reset; unused ECC remains held.

The timing comes from the exact Terasic Nano DTS at `d0f8813`, rather than
the different generic bare-metal defaults. Clock and pin routes are checked,
never reprogrammed. Failures retain their first observations and retire access;
the diagnostic printer uses saved values only. Register models exercise these
actual callbacks, including two full leases and failed reset/PHY/clock/drain
paths. Physical reads remain a separate gate. The matched cold diagnostic
still advertises zero FPGA programming features and prevents secondary CPU release.

### Actual cold serial producer and callbacks

```sh
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/terasic-git-restore-tfa \
  --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --nano-owned-bridges --nano-cold-serial-entry out/nano-entry \
  --output out/fresh-cold-bl31
```

This opt-in source-bound diagnostic links `nano_cold_platform.c` as a strong
provider. It consumes a bounded, aligned OCRAM record passed directly by SPL
to early EL3 setup, before non-secure execution. There is no later SiP or
non-secure initialization route. A checksum detects record corruption; it
does not authenticate an attacker-controlled boot chain.

The matched SPL uses `nano_cold.c` and the shared real register adapter to
sample SDM boot kind, reset cause, parked secondary CPUs, bridges and unused
masters before initialization. Only positively identified POR/cold/nCONFIG/JTAG
entry paths qualify. SD is the sole active device, with all DMA disabled.
After loading the sealed FIT and native entry, SPL independently observes
SDHCI idle, masks interrupts, disables the card clock, then asserts functional
and OCP resets with barriers/readback. It supplies a copied one-use record
bound to the exact entry, producer sources, owner and epoch. Full U-Boot never
reopens SD after BL31 receives that record.

Its private version-2 record also captures CPU power status, MPU power
request/status and boot configuration at each ownership boundary. The selected
primary must agree with actual EL3 MPIDR, both CPU-channel masks must name only
it, and every spare controller must retain its zero reset default with no
pending release or cleared spare reset-history bit. The active primary's
reset bit is excluded from the live-state test, following the
[factory CPU comparison](../../../docs/validation/2026-11-01-de25-cpu-inventory-reference/README.md)
and [initial EL3 power observations](../../../docs/validation/2026-11-01-de25-el3-power-inventory/README.md).
This is a trusted fresh-entry parking policy, not a generic power-off decoder
or hotplug/drain implementation. It does not accept the old six-word tuple.

The cold profile's actual PSCI CPU_ON frontend returns `PSCI_E_DENIED` before
framework bookkeeping or any hardware write and preserves the owner. The lower
platform callback returns `PSCI_E_INTERN_FAIL`, as required by TF-A's internal
callback contract, and cannot release a CPU. A strong release-lock function in
the frontend's translation unit is mandatory for the early provider; the matched
U-Boot builder requires its installation receipt. Other power/reset transitions
continue to invalidate ownership. Version-1 records/cookies reject. The shared
bridge wire ABI is unchanged.

The actual `owns`, `quiesce` and `quiescent` callbacks verify continued cold
containment through live registers; they do not access an SD controller held
in reset. A changed boot/reset scope, restarted master, active bridge or
replay retires the proof. This is not an active-device drain provider. The
cold dispatcher admits read-only Snapshot only, denies all bridge mutations
and configuration operations, and advertises zero configuration features.
The general controlled service and strict recovery behavior are unchanged.

The [Nano guide](../nano/README.md) describes matched builds, a preserved
recovery card and repeatable network uploads of `FES/UPLOAD.BIN`. The
[current validation](../../../docs/validation/2026-10-31-de25-cold-upload/README.md)
records real cross-builds and injected host tests. The later
[physical upload/cold trial](../../../docs/validation/2026-10-31-de25-upload-cold-trial/README.md)
passes the upload core/transport but rejects the first SPL CPU/reset proof.
The documented sticky POR flag is now accepted alongside SDM cold reset;
independent boot-kind proof and every CPU/ownership gate remain required.
Fresh diagnostic samples cannot replace failed early proof. The
[later native Go test](../../../docs/validation/2026-11-01-de25-native-go-boot/README.md)
now passes the matched cold serial handoff on TRWU8CBL, including both owned
snapshots, EL2/EL1 entry and the actual Go probe. That is contained serial
diagnostic acceptance. Native SD/USB/network resume,
active FPGA drain, signed secure boot and physical launch/return remain open.
The [version-2 initial-parking trace](../../../docs/validation/2026-11-01-de25-initial-parking-v2/README.md)
now accepts the observed core-0 startup with the independent power/selection
checks, then unconditionally parks before DDR/storage/BL31. That result does
not exercise the matched secure firmware, SD drain or native handoff.

## Board-specific off-board candidate

The two build commands below require clean checkouts of the exact Terasic
commits and an AArch64 GNU cross compiler. `build_terasic.py` exports the pinned
TF-A Git tree into a fresh private directory, applies the source transformation,
builds BL31 and checks its ELF load segments against the vendor's BL31 bounds.
`build_terasic_uboot.py` builds the matching DE25 U-Boot source
out of tree, using that BL31 receipt. It extracts the ATF image from the FIT
and requires its bytes to match the selected BL31. It checks that U-Boot's
initial FIT load does not overlap BL31's actual loaded memory, including BSS.
The dedicated Nano loader identity also binds the exact BL31 digest, so a
firmware-only rebuild changes both SPL and full U-Boot's expected identity.
Packaging checks that binding. This does not establish the subsequent
relocation or native memory handoff.
U-Boot's host build also
needs Python setuptools/pyelftools, SWIG, dtc, bison, flex and OpenSSL headers.

```sh
git clone --depth 1 --branch de25_nano_revA_v1.0 \
  https://github.com/terasic/arm-trusted-firmware.git out/vendor-tfa
git clone --depth 1 --branch de25_nano_revA_v1.0 \
  https://github.com/terasic/u-boot-socfpga.git out/vendor-uboot
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/vendor-tfa --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --output out/vendor-bl31
python3 platform/embedded/firmware/build_terasic_uboot.py \
  --reference-repo out/vendor-uboot --bl31-build out/vendor-bl31 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/vendor-uboot-build
```

These commands create `bl31.bin`, `u-boot.itb` and an SPL hex file only; they
do not write a boot image or access a device. The generated SPL is not paired
with the separately sealed FPGA phase-one image. The FIT and receipts are
content-hashed diagnostics, not signed boot images or hardware acceptance.

The pinned [vendor BL31 source](https://github.com/terasic/arm-trusted-firmware/blob/580dcb5c4e10aa7daff2bf69d493c076a6d3e665/plat/intel/soc/agilex5/bl31_plat_setup.c)
selects EL2 for BL33 when available. In the pinned
[U-Boot ELF loader](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/lib/elf.c),
`bootelf` enters the loaded image directly; the board defconfig does not select
the EL1 switch. It therefore does not satisfy the native image's EL1 and clean
MMU/cache contract by itself. The separate [entry prototype](../entry/README.md)
uses U-Boot's `booti` cleanup and a guarded EL2-to-EL1 transition, tested with
the vendor source built for QEMU. A Nano-specific resource handoff, matched
boot chain and recovery procedure remain required before attempting a board boot.

## Adapter and endpoint

`Control` uses synchronous, ordered, bounded-return read/write hooks for the
relative CSR aperture. It compares two snapshots, verifies all 128 build-ID
bits, and binds disable/drain to the old generation and a nonzero cookie.
The host-only `ContainedPath` fixture exercises the existing lifecycle order:
query firmware, disable/drain, contain, configure, open control-only access,
verify fresh target identity, then wait for a separate explicit data release.
The product adapter exposes only per-phase control operations, with no second
lifecycle, save or recovery policy. It allocates no memory and uses no OS or
C++ exception/RTTI support; that adapter cross-compiles for AArch64.

The runtime owns its admitted bytes and all reservations throughout this
transaction. Program hooks must retain their buffers through asynchronous
completion or unresolved failure. The test path verifies that failure cannot poll, retry or release;
there is no destructor which frees hardware resources. A failed bridge/MMIO/SMC
operation can have an uncertain outcome; callers must retain ownership and
recover physically, rather than assuming that a failed release stayed closed.
A deadline cannot preempt a firmware call or bus access that never returns.

Platform hooks must prove containment of **all** other masters, program only
retained admitted bytes, and perform masked bridge readback. Control-only access
must reach only the HPS-to-FPGA control aperture while FPGA-to-HPS/DDR masters
stay contained. The reference mask is the inspected TF-A lightweight-bridge
mask; its actual Nano routing and reset behaviour need board qualification.
Only after identity verification may a separately authorized release open the
data paths required by the admitted target. The test's all-bridge release is
a simulated platform operation, not a board default.

The RTL is a single-clock, single-AXI-ID read guard/control endpoint, not a whole
scanout engine or an address firewall. The upstream reader must supply legal
aligned INCR requests and a safe DDR window. Board glue must supply clock/reset,
CSR interconnect, CDC where needed, the real DDR port and the scanout/HDMI path.
Reset clears identity of the active generation and cannot acknowledge an old
drain. Reinitialization needs a known quiescent/reset memory interface.

## Verification

```sh
python3 platform/embedded/firmware/verify.py \
  --reference out/reference/socfpga_sip_svc.c \
  --cross-cxx out/de25-os-tools/zephyr-sdk-0.17.4/aarch64-zephyr-elf/bin/aarch64-zephyr-elf-g++ \
  --output out/embedded-control-check
```

Without `--reference`, the verifier downloads that exact public Git blob and
checks its identity before use. `--output` defaults to a fresh private directory.
Without `--cross-cxx`, it runs host RTL/firmware integration and explicitly marks
AArch64 adapter compilation not run. The affected-test planner schedules this
standalone consumer; no shipped core currently instantiates the new endpoint.

The verifier compiles the actual prepared START/WRITE/completion/status/remap
function bodies with simulated platform calls and undefined-behaviour checks,
then links them with `Control`, the host-only sequence fixture and Verilator's actual RTL model.
It exercises an identical-image reload, wrong identity, queued bursts, held AR,
early/wrong RLAST, RID/RRESP faults, reset, stale generation/cookie, CSR and reader
backpressure, simultaneous AR/response, unknown firmware, transport/start
failures, lost asynchronous responses and deadlines. This executes the patched
C functions, not an EL3 SMC instruction or actual SDM. The Nano backend remains
read-only. Source/command/ELF hashes and that evidence boundary are recorded.
