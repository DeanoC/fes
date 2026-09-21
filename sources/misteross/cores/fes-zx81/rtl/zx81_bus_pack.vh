// SPDX-License-Identifier: GPL-2.0-or-later
// Freeze-scaffold packing for the ZX81 expansion edge. Vacant rdata FFs
// hold 0, so cart-to-CPU controls are active-high (ROMCS/WAIT/DSEL/PRESENT).
`define ZX81_BUS_REQ 44
`define ZX81_BUS_RSP 20
`define ZX81_BUS_A 15:0
`define ZX81_BUS_DWR 23:16
`define ZX81_BUS_MREQ_N 24
`define ZX81_BUS_IORQ_N 25
`define ZX81_BUS_RD_N 26
`define ZX81_BUS_WR_N 27
`define ZX81_BUS_M1_N 28
`define ZX81_BUS_RFSH_N 29
`define ZX81_BUS_PEEK_A 43:30
`define ZX81_BUS_DRD 7:0
`define ZX81_BUS_PEEK_D 15:8
`define ZX81_BUS_DSEL 16
`define ZX81_BUS_ROMCS 17
`define ZX81_BUS_WAIT 18
`define ZX81_BUS_RAM_PRESENT 19
