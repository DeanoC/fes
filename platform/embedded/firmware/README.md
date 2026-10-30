# Contained firmware and FPGA control prototype

This is hardware-independent source and integration work for the Linux-free
base. It supplies a pinned TF-A source transformation, a freestanding C++14
control adapter, and verification against actual FPGA RTL. It is not a Nano
boot image, a TamaGo programming binding or hardware acceptance.

The shared [contract](../../../sources/mister-packages/docs/embedded-control.md)
owns the register offsets and private firmware query. Its generated C++ and
Verilog consumers come from `make generate`; do not edit them by hand.

## Firmware source

`prepare_tfa.py` accepts exactly TF-A v2.12's
`plat/intel/soc/common/socfpga_sip_svc.c`, Git blob
`3c223c1498e3a412e075e4f921cd78acd3d0fcd6`. It writes a fresh private directory
under this worktree's `out/`, preserving the upstream BSD copyright header.
It does not patch a shared checkout, fetch a board image or deploy firmware.

The contained variant:

- Rejects partial/authentication flags; full configuration uses flag zero.
- Disables bridges and checks the result before CANCEL/RECONFIG.
- Checks CANCEL failure and RECONFIG response shape/zero credits or block size.
- Makes ISDONE leave every data bridge closed, including repeated status polls.
- Handles START's null remapper argument while preserving WRITE's CPU-to-SDM
  relative-address conversion.
- Exposes the local experimental capability query documented in the contract.

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
