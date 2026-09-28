// SPDX-License-Identifier: GPL-2.0-or-later
// FES Commodore 64 cartridge bus (`fes.c64-bus.socket/1`).
//
// Both physical sockets see one registered request word. The motherboard
// decodes ROML, ROMH, IO1 and IO2 and presents them active-high; a socket
// wrapper clears the selects that socket does not own. STROBE is a one-clock
// pulse at the start of each 6510 cycle. Address, data, direction and selects
// are then held for the rest of the cycle. A card applies side effects on
// STROBE and presents read data from the held request; the motherboard
// samples the response late in the cycle.
//
// EXROM and GAME in the response are active-high pulls: 1 means the card
// pulls that pin low. A vacant response is all zero, so a vacant socket
// releases both pins and drives no data.
`define C64_BUS_REQ          32
`define C64_BUS_A            15:0
`define C64_BUS_D            23:16
`define C64_BUS_READ         24
`define C64_BUS_STROBE       25
`define C64_BUS_RESET        26
`define C64_BUS_ROML         27
`define C64_BUS_ROMH         28
`define C64_BUS_IO1          29
`define C64_BUS_IO2          30
`define C64_BUS_BA           31

`define C64_BUS_RSP          28
`define C64_BUS_RD           7:0
`define C64_BUS_DRIVE        8
`define C64_BUS_IRQ          9
`define C64_BUS_NMI          10
`define C64_BUS_EXROM        11
`define C64_BUS_GAME         12
`define C64_BUS_AUDIO        27:13
