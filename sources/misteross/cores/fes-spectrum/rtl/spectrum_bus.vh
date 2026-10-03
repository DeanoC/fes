// SPDX-License-Identifier: GPL-2.0-or-later
// FES ZX Spectrum edge-connector bus (`fes.spectrum-bus.socket/1`).
//
// Every physical socket sees the same registered request word. Controls are
// active-high. STROBE is a one-clock pulse when RD or WR falls, with the
// address and write data already stable. A vacant response word is zero, so
// DRIVE, ROMCS, NMI and WAIT are active-high as well. AUDIO is signed 16-bit
// PCM. The shell sums it from every socket, including a socket that is not
// driving the CPU bus. The motherboard samples the registered response well
// before the Z80 reads data. WAIT delays CPU completion; STROBE is a launch
// event and is never repeated while WAIT is held. Cards must consume writes
// once per STROBE. Fast-mode internal RAM/ULA writes commit at completion.
`define SP_BUS_REQ          32
`define SP_BUS_A            15:0
`define SP_BUS_D            23:16
`define SP_BUS_MREQ         24
`define SP_BUS_IORQ         25
`define SP_BUS_RD           26
`define SP_BUS_WR           27
`define SP_BUS_M1           28
`define SP_BUS_STROBE       29
`define SP_BUS_RESET        30
`define SP_BUS_RESERVED     31

`define SP_BUS_RSP          28
`define SP_BUS_RDATA        7:0
`define SP_BUS_DRIVE        8
`define SP_BUS_ROMCS        9
`define SP_BUS_NMI          10
`define SP_BUS_WAIT         11
`define SP_BUS_AUDIO        27:12
