// SPDX-License-Identifier: GPL-2.0-or-later
// FES Apple II slot bus packing (`fes.apple2-bus.slot/1`).
//
// Every slot sees the same registered request word. The per-slot selects are
// already decoded by the motherboard, as on the original backplane, but they
// are active-high here. A vacant response word is all zero, so every card to
// motherboard control is active-high as well.
//
// STROBE is a one-clock pulse at the start of each 6502 cycle. Address, data,
// direction and selects are then held for the whole cycle (about 51 system
// clocks). A card applies side effects on STROBE and presents read data from
// the held request; the motherboard samples the response late in the cycle.
// Q3 pulses twice per cycle (at STROBE and at mid-cycle), giving cards the
// original 2 MHz sequencing rate.
`define A2_BUS_REQ          32
`define A2_BUS_A            15:0
`define A2_BUS_D            23:16
`define A2_BUS_READ         24
`define A2_BUS_STROBE       25
`define A2_BUS_Q3           26
`define A2_BUS_RESET        27
`define A2_BUS_DEVSEL       28
`define A2_BUS_IOSEL        29
`define A2_BUS_IOSTROBE     30
`define A2_BUS_RESERVED     31

`define A2_BUS_RSP          28
`define A2_BUS_RD           7:0
`define A2_BUS_DRIVE        8
`define A2_BUS_IRQ          9
`define A2_BUS_NMI          10
`define A2_BUS_INH          11
`define A2_BUS_AUDIO        27:12
