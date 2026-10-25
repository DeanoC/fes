// SPDX-License-Identifier: GPL-2.0-or-later
// Build-time ZX81-family model. The producer defines one of ZX81_MODEL_ZX80,
// ZX81_MODEL_TS1000 or ZX81_MODEL_TS1500; none is the Sinclair ZX81 (PAL,
// 8 KiB ROM, 1 KiB internal RAM, NMI). The define, not a parameter, selects
// the linked ROM lanes so their routed cell names stay flat.
`ifndef ZX81_MODEL_VH
`define ZX81_MODEL_VH
`ifdef ZX81_MODEL_ZX80
`define ZX81_ROM_4K
`endif
`endif
