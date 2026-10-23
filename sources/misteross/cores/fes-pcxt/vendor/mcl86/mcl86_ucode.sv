//------------------------------------------------------------------------------
// SPDX-License-Identifier: MIT
// SPDX-FileType: SOURCE
// SPDX-FileCopyrightText: (c) 2020, Ted Fried <@MicroCoreLabs>
// SPDX-FileCopyrightText: (c) 2026, Marcus Andrade <marcus@opengateware.org>
//------------------------------------------------------------------------------
//
// MCL86 - Intel 8086/8088 Microprocessor Compatible Gateware IP Core
//
// Copyright (c) 2020, Ted Fried <@MicroCoreLabs>
// Copyright (c) 2026, Marcus Andrade <marcus@opengateware.org>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//
//------------------------------------------------------------------------------
// Execution Unit Microcode ROM
//
// Description:
//   4K x 32 microcode store for mcl86_eu_core. Single synchronous read port,
//   split into four byte-wide block ROMs. Each word is one microinstruction;
//   the field layout lives in mcl86_eu_core.sv, which decodes it.
//
//   The read is REGISTERED and advances with the EU execute enable. One
//   microword of read latency is what gives the EU
//   sequencer its two-microword lead over the word it is executing. Do not
//   "optimize" this into an asynchronous read: the microcode, the pipeline stall
//   on taken jumps, and every microcode-address marker in the EU assume it.
//
// ROM image:
//   mcl86_ucode.mem is generated and validated by the tools in tools/ - ucasm.py
//   assembles it, ucdis.py disassembles it, and uccheck.py proves the round-trip
//   is lossless and that the EU's watched addresses still line up. Edit the ROM
//   through those tools, not by hand.
//
// Limitations:
//   - $readmemh resolves both control-ROM paths against the working directory.
//     FES cpu_roms copies mcl86_ucode.mem and emits its four byte lanes
//     beside the executable/netlist. All four lane images must exist there.
//   - Synthesis relies on the initial block for ROM contents. That is supported
//     by Quartus, Vivado and yosys, but it is not a reset value - the array is
//     never re-initialized after power-up.
//------------------------------------------------------------------------------

`default_nettype none
`timescale 1ns/1ps

module mcl86_ucode #(parameter [31:0] RESET_WORD = 32'b0) (
    input wire clk,
    input wire reset,
    input wire advance, // One microword every two system cycles
    // Same next-PC value that the EU captures on this system edge.
    input wire [11:0] next_addr,
    output logic [31:0] dout,
    output logic [15:0] operand_select0, operand_select1,
    output logic [15:0] add_select0, add_select1
);
    logic [31:0] data_half;
    // Four independent address registers give each 8-bit ROM lane a local
    // driver. They capture the EU's next PC on the same execute edge as the real PC;
    // The intervening operand edge reads these addresses before the next
    // execute edge captures the word. Both paths have a full physical cycle.
    generate for (genvar lane = 0; lane < 4; lane = lane + 1) begin: bytes
        reg [11:0] read_address;
        (* keep *) always_ff @(posedge clk) begin : read_address_output
            if (reset) read_address <= 12'h020;
            else if (advance) read_address <= next_addr;
        end
        logic [7:0] rom[0:4095];
        initial begin
            if (lane == 0) $readmemh("mcl86_ucode_byte0.mem", rom);
            else if (lane == 1) $readmemh("mcl86_ucode_byte1.mem", rom);
            else if (lane == 2) $readmemh("mcl86_ucode_byte2.mem", rom);
            else $readmemh("mcl86_ucode_byte3.mem", rom);
        end
        always_ff @(posedge clk) begin : read_mem
            data_half[lane*8 +: 8] <= rom[read_address];
        end
    end endgenerate
    always_ff @(posedge clk) begin : read_data_output
        if (reset) dout <= RESET_WORD;
        else if (advance) dout <= data_half;
    end
    // Decode the same immutable microword before the system output registers.
    // Separate kept processes preserve local controls for ADD and logic uses.
    wire [31:0] select_half = {(16'h1 << data_half[23:20]),
                             (16'h1 << data_half[19:16])};
    (* keep *) always_ff @(posedge clk) begin : read_select_output
        if (reset) {operand_select0, operand_select1} <=
            {(16'h1 << RESET_WORD[23:20]), (16'h1 << RESET_WORD[19:16])};
        else if (advance) {operand_select0, operand_select1} <= select_half;
    end
    (* keep *) always_ff @(posedge clk) begin : read_add_select_output
        if (reset) {add_select0, add_select1} <=
            {(16'h1 << RESET_WORD[23:20]), (16'h1 << RESET_WORD[19:16])};
        else if (advance) {add_select0, add_select1} <= select_half;
    end
endmodule

`default_nettype wire
