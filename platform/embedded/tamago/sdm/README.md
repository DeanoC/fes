# Read-only Agilex SDM discovery

`Client.Probe` sends CONFIG_STATUS (0x04) and GET_IDCODE (0x10) through TF-A
`INTEL_SIP_SMC_MBOX_SEND_CMD` (0xc200003c). It validates the service status,
mailbox status and exact returned word counts (6 and 1). It clears the response
before each call, serializes complete probes and returns no partial observation
after an error. Any error retires the client without replay. Configuration state,
nSTATUS, CONF_DONE, INIT_DONE and SEU status determine the reported `Configured`
flag. This flag does not admit a package. IDCODE is silicon identity, not a live
core/build identity or a verified mapping to the Nano's part marking.

The Nano adapter executes SMC #0 with ordered memory access, using a private
reserved DDR arena mapped Normal noncacheable. It never supplies a Go heap or
stack address to firmware, accesses mailbox registers, writes an RBF, or opens
bridges. The arena remains reserved until reboot, including on error. No other
CPU, firmware client, OS or peripheral may use this arena/mailbox concurrently.
TF-A is a trusted dependency: it must honor response capacity and return with
all response accesses finished. An EL1 Go deadline cannot interrupt an SMC that
does not return. These firmware properties need source review and board tests;
profile declarations alone cannot prove them.

## Explicit firmware profile

Schema 5 adds `fpga_probe`, disabled as `null` by default. Enabling it requires:

```json
{
  "base": 2483027968,
  "size": 2097152,
  "service": "intel-sip-mbox-v1-read-only",
  "exclusive": true,
  "synchronous_bounded_return": true
}
```

The address above is **synthetic** (`0x94000000`), not a delivered-board recipe.
Use an actual reserved window at/above the firmware's BL31 limit (this client
requires at least `0x82000000`), aligned to 2 MiB, in HPS DDR and outside Go RAM,
firmware reservations, network DMA and display. The larger mapping granularity
keeps the service words separate from other memory owners. The client uses its
first 8 words as response capacity and a separate reserved word as the valid
zero-length argument address. Older profile schemas are rejected.

```sh
python3 platform/embedded/tamago/run.py fpga --board de25-nano \
  --profile out/actual-firmware-handoff.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-sdm-probe
```

This cross-builds only. QEMU has no SDM service; the runner rejects `fpga` on
that board. Host tests inject a protocol endpoint rather than executing SMC.

## Why programming remains unavailable

The reviewed TF-A **v2.12** implementation of
[`intel_mailbox_fpga_config_isdone`](https://github.com/ARM-software/arm-trusted-firmware/blob/v2.12/plat/intel/soc/common/socfpga_sip_svc.c)
automatically enables all bridges when a full configuration finishes and
`bridge_disable` is set. Even a status poll through FPGA_CONFIG_ISDONE can
therefore mutate containment. Our probe uses generic CONFIG_STATUS instead.
The dedicated configuration START also initializes the HPS remapper on Agilex 5;
existing XGMAC identity DMA cannot be assumed to survive that operation.

The same implementation's WRITE can return BUSY **after accepting** a buffer;
REJECTED indicates no free buffer or invalid memory. COMPLETED_WRITE reports
up to three returned addresses. A future transfer adapter must track exact
accepted buffers and bytes, reject duplicate/foreign completions, and retain
ambiguous buffers until reboot rather than blindly replay BUSY. This client
exposes none of those mutating operations.

Physical activation still requires a reviewed firmware path that preserves
containment through post-program live identity verification, documented DMA
mapping behaviour, and an FPGA-owned disable/drain acknowledgement bound to
the active scanout generation. The existing boot scanout window remains pinned
even after tenfoot exits; a successful CPU copy cannot release it. No new GP
register layout, live firmware identity, drain implementation, Nano bitstream,
or compatible boot firmware is supplied here.

## Reference identity

Protocol values and ABI were checked against these primary sources. No vendor
driver code is linked or copied into the image.

- [TF-A v2.12 SiP header](https://github.com/ARM-software/arm-trusted-firmware/blob/v2.12/plat/intel/soc/common/include/socfpga_sip_svc.h),
  Git blob `31474c4699bfd4122051e24d454a606ad81b2b90`.
- [TF-A v2.12 SiP implementation](https://github.com/ARM-software/arm-trusted-firmware/blob/v2.12/plat/intel/soc/common/socfpga_sip_svc.c),
  Git blob `3c223c1498e3a412e075e4f921cd78acd3d0fcd6`.
- [Altera bare-metal drivers mailbox API](https://github.com/altera-fpga/baremetal-drivers/blob/66bdcc347564c5698a8997169bce83a31647d618/api/mailbox/README.md),
  source checkout `66bdcc347564c5698a8997169bce83a31647d618`.
  Its examples specify one IDCODE word and six configuration-status words,
  DDR placement above BL31, and no direct-register fallback.

The validation record records the checked source hashes.
