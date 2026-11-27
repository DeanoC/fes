# DesignWare APB I2C driver (`i2c/`)

`i2c` implements a bare-metal Synopsys DesignWare APB I2C (`DW_apb_i2c`,
`IC_COMP_TYPE == 0x44570140`) master driver matching Agilex 5 HPS controllers
(`Agilex5I2C0Base = 0x10c02800` .. `Agilex5I2C4Base = 0x10c02c00`).

## Features

- `Open(regs, cfg)` verifies the DesignWare component ID, computes SCL high/low
  counts for `SpeedStandard` (100 kHz) and `SpeedFast` (400 kHz), decodes
  hardware TX/RX FIFO depths from `IC_COMP_PARAM_1`, and leaves interrupts masked.
- `Bus` (`WriteReg`, `ReadReg`, `WriteRegByte`, `ReadRegByte`, `Probe`) serializes bus
  transfers under a mutex, pipelines multi-byte repeated-START reads (such as
  128-byte EDID blocks) within the RX FIFO depth, and maps `IC_TX_ABRT_SOURCE`
  to `ErrNACK`, `ErrArbitrationLost`, or `ErrAbort`.
- `OpenMMIO(base, cfg)` (on `tamago && arm64`) wraps mapped MMIO access with
  `DSB $15` ordering barriers.
