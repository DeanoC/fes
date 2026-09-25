// SPDX-License-Identifier: GPL-2.0-or-later
// Coleco CPU peripheral edge v2. All response controls are active high.
`ifndef COLECO_BUS_V2_PACK_VH
`define COLECO_BUS_V2_PACK_VH
`define COLECO_V2_BUS_REQ 31
`define COLECO_V2_BUS_RSP 28
`define COLECO_V2_BUS_A 15:0
`define COLECO_V2_BUS_DWR 23:16
`define COLECO_V2_BUS_MREQ_N 24
`define COLECO_V2_BUS_IORQ_N 25
`define COLECO_V2_BUS_RD_N 26
`define COLECO_V2_BUS_WR_N 27
`define COLECO_V2_BUS_M1_N 28
`define COLECO_V2_BUS_RFSH_N 29
`define COLECO_V2_BUS_RESET 30
`define COLECO_V2_BUS_DRD 7:0
`define COLECO_V2_BUS_CLAIM 8
`define COLECO_V2_BUS_WAIT 9
`define COLECO_V2_BUS_INT 10
`define COLECO_V2_BUS_RAM_CLAIM 11
`define COLECO_V2_BUS_PCM 27:12
`endif
