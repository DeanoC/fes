// SPDX-License-Identifier: GPL-2.0-or-later
// Freeze-scaffold packing for the ZX81 expansion edge. Vacant rdata FFs
// hold 0, so cart-to-CPU controls are active-high (ROMCS/WAIT/DSEL/PRESENT).
// All bits in one response word (DRD, PEEK_D, DSEL, ROMCS, WAIT) must
// describe the same request. A cart may add internal pipeline stages as
// long as it delays all of them together. End-to-end request-to-response
// latency at the machine must stay at most 6 clk_sys.
`define ZX81_BUS_REQ 46
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

// Bus 2.0 adds physical edge clock and reset without repacking v1 signals.
`define ZX81_BUS_CPU_CLK 44
`define ZX81_BUS_RESET_N 45
