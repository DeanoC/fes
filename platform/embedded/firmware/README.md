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
