# Agilex SDM discovery and configuration model (`sdm/`)

`sdm/` contains two distinct components:
1. **`Client` (read-only DE25-Nano discovery):** queries SDM `CONFIG_STATUS`
   (`0x04`) and `GET_IDCODE` (`0x10`) through TF-A
   `INTEL_SIP_SMC_MBOX_SEND_CMD` (`0xc200003c`), returning an `Observation`
   with structured decoders (`PinStatus`, `SoftStatus`, `IDCodeInfo`,
   `HasError`, `Configured`, and `String`).
2. **`ConfigModel` (off-board software protocol model):** models the
   `START`/`WRITE`/`COMPLETED_WRITE`/`ISDONE` state machine for QEMU/host tests
   under [fabricmodel](../fabricmodel/README.md).

Real DE25-Nano FPGA reconfiguration runs through
[nativefabric](../nativefabric/README.md) (`ConfigurationTransfer`) and the
contained EL3 BL31 service ([firmware/](../../firmware/README.md)), **not**
through `sdm.Client`. See [../../ARCHITECTURE.md](../../ARCHITECTURE.md).

## Read-only discovery (`Client.Probe`)

- **Called by:** `cmd/fpga` (DE25-Nano diagnostic; QEMU rejects `fpga`).
- **Delegates to:** `board/de25nano` SMC #0 wrapper using a private 2 MiB
  Normal-noncacheable DDR arena (never Go heap/stack).
- **Contract:** Clears response memory before each call, verifies service
  status, mailbox status, and exact word counts (6 words for `CONFIG_STATUS`,
  1 word for `GET_IDCODE`), and retires without replay on any error while
  keeping the arena pinned until reboot.
- **Why stock TF-A `FPGA_CONFIG_ISDONE` is unsafe:** Stock upstream TF-A v2.12
  `intel_mailbox_fpga_config_isdone` automatically re-enables FPGA bridges when
  a full configuration completes, and `WRITE` can return `BUSY` *after*
  accepting a buffer while `COMPLETED_WRITE` returns SDM-relative addresses
  (`cpu_addr - 0x80000000`). That is why `Client.Probe` stays strictly
  read-only and real configuration uses the patched contained BL31 in
  [firmware/](../../firmware/README.md).

### Schema-8 firmware profile (`fpga_probe`)

Disabled as `null` by default in schema-8 profiles
([board/README.md](../board/README.md)). Enabling it requires a disjoint 2 MiB-
aligned HPS DDR window at or above `0x82000000`:

```json
{
  "base": 2483027968,
  "size": 2097152,
  "service": "intel-sip-mbox-v1-read-only",
  "exclusive": true,
  "synchronous_bounded_return": true
}
```

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py fpga \
  --board de25-nano --profile out/actual-firmware-handoff.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-sdm-probe
```

## Configuration protocol model

`ConfigModel` is an event-driven software model of `START`, `WRITE`,
`COMPLETED_WRITE`, and `ISDONE` with no SMC, MMIO, or hardware authority
(`ReferenceTFAV212` and unknown firmware reject before `START`; only the
hypothetical `HeldBridgeModel` endpoint is accepted).

- **Called by:** `configcheck` + `cmd/fabric` (host tests and QEMU `fabric`
  suite), connected beneath `fabricmodel.Controller`.
- **Invariants exercised:**
  - Retains up to 8 MiB of source bytes, tracks up to 4 outstanding chunk
    writes using CPU request addresses vs SDM-relative completion addresses.
  - Treats both `WRITE OK` and `WRITE BUSY` as buffer acceptance (pausing
    submissions on `BUSY` to poll completions); never replays rejected or
    uncertain writes.
  - Validates up to 3 distinct known addresses per `COMPLETED_WRITE` batch
    without holes; duplicate, foreign, missing, or timed-out completions
    permanently retire the model and retain its buffers.
  - Requires quiescent display-drain evidence from `fabricmodel.Controller`
    before containment/reconfiguration.

## Reference sources and tests

- Protocol constants were verified against TF-A v2.12 (`socfpga_sip_svc.h`
  `31474c4699`, `socfpga_sip_svc.c` `3c223c1498`, Agilex 5 `socfpga_plat_def.h`)
  and Altera bare-metal mailbox docs (`66bdcc347564`). No vendor C code is
  linked into Go.
- Run tests:
  ```sh
  GOWORK=off go -C platform/embedded/tamago test -race ./sdm ./configcheck
  ```
