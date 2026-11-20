// SPDX-License-Identifier: GPL-3.0-or-later
// The 192 KiB Atari ST ROM in SDRAM, filled at launch
// (fes.fabric.firmware-upload). The device has too little block RAM for the
// ROM beside the ST's video frame store. This puts the machine's ROM port and
// the firmware writes on the CPU's SDRAM port, in the ROM region the DE25
// derivation of st_memory adds at halfword c0000.
//
// The machine drives one of its ROM and RAM ports in a bus cycle, never both,
// and drops the request for at least a clock between cycles, which the memory
// needs to rearm. A ROM read therefore takes the CPU's phase slot exactly as
// a RAM read does; its reply is the memory's one-clock ready with the word.
// Firmware arrives only while the machine is held, when both machine ports
// are idle: each uploaded pair is one posted word write, even byte in the
// upper half as the 68000 reads it, and is acknowledged when the memory takes
// it. No FPGA-family primitive belongs here.
module fes_st_rom_sdram (
    input  wire        rom_req,
    input  wire [17:1] rom_addr,
    output wire [15:0] rom_rdata,
    output wire        rom_ready,
    input  wire        ram_req, ram_write,
    input  wire [18:1] ram_addr,
    input  wire [15:0] ram_wdata,
    input  wire [1:0]  ram_byte_enable,
    output wire [15:0] ram_rdata,
    output wire        ram_ready,
    input  wire [17:0] firmware_write_addr,
    input  wire [15:0] firmware_write_data,
    input  wire [1:0]  firmware_write_enable,
    output wire        firmware_write_ready,
    output wire        cpu_req, cpu_rom, cpu_write,
    output wire [18:1] cpu_addr,
    output wire [15:0] cpu_wdata,
    output wire [1:0]  cpu_byte_enable,
    input  wire        cpu_ready,
    input  wire [15:0] cpu_rdata
);
    // The front end sends whole pairs from even addresses: the image is even.
    wire firmware = firmware_write_enable[0];
    assign cpu_req = firmware || rom_req || ram_req;
    assign cpu_rom = firmware || rom_req;
    assign cpu_write = firmware || (!rom_req && ram_write);
    assign cpu_addr = firmware ? {1'b0, firmware_write_addr[17:1]} :
                      rom_req ? {1'b0, rom_addr} : ram_addr;
    assign cpu_wdata = firmware ? {firmware_write_data[7:0], firmware_write_data[15:8]} : ram_wdata;
    assign cpu_byte_enable = firmware ? {1'b1, firmware_write_enable[1]} :
                             rom_req ? 2'b11 : ram_byte_enable;
    assign firmware_write_ready = firmware && cpu_ready;
    assign rom_ready = !firmware && rom_req && cpu_ready;
    assign ram_ready = !firmware && !rom_req && cpu_ready;
    assign rom_rdata = cpu_rdata;
    assign ram_rdata = cpu_rdata;
endmodule
