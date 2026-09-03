# Schema `mister-packages.v1`

Every package file starts with:

```yaml
schema: mister-packages.v1
kind: platform | board | soc | cpu | register_bank
id: unique.dot.or.slash.free.id
```

Paths in a platform file are relative to `packages/`.

Hex values are YAML scalars such as `0xff706000`. The loader accepts
`0x` hex or decimal.

## platform

Selects one board, one SoC, and one CPU. There is no instance graph and
no connection list.

```yaml
kind: platform
id: de10_nano
board: board/de10_nano.yaml
soc: soc/cyclone_v.yaml
cpu: cpu/arm_cyclone_v.yaml
```

## board

Vendor, FPGA part, clocks, and pins. Pins are descriptive; this schema
does not generate XDC or Quartus constraints.

## soc

Physical windows (base and size), AXI-style buses, and named register
banks. A bank has a base address and a `registers:` file path.

Windows exist because register banks only expose a base. Main_MiSTer and
the native runtime map whole `/dev/mem` apertures.

## cpu

Target triple, architecture, width, and core count. This is emit context,
not a compiler.

## register_bank

A list of registers with offsets, optional `address_symbols`, optional
whole-register `writes`, and fields.

```yaml
kind: register_bank
id: cyclone_v.fpga_manager
registers:
  - name: STAT
    offset: 0x0
    address_symbols: [kFpgaStatusAddress]
    fields:
      - name: MODE
        bits: "2:0"
        mask_symbol: kFpgaModeMask
        values:
          - symbol: kFpgaModeReset
            value: 0x1
      - name: MSEL
        bits: "7:3"
        mask_symbol: kFpgaMselMask
        shift_symbol: kFpgaMselShift
```

`bits` is `hi:lo` or a single bit. The emitter computes mask and shift
from that range.

Field `values` default to **field** placement (the unshifted number, as
`kFpgaModeReset = 0x1`). `placement: register` shifts the value into the
register (`kFpgaCoreReset = 0x40000000`).

`writes` are named constants for whole-register values that are not a
bitfield encoding (`kSdrFpgaPortsEnabled = 0x3fff`).

`address_symbols` may list more than one name when two runtime headers
share an address (`kFpgaGpoAddress` and `kSpiGpoAddress`).

## What is intentionally missing

- Connections, prefabs, wildcards, and bus address allocation.
- `system` packages (milestone 2).
- Actions, templates, and git clones. Provenance belongs in a later
  explicit adapter file, not a shell recipe.
