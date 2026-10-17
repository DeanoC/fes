// SPDX-License-Identifier: GPL-2.0-or-later
// Shared MOS6581 combined-waveform lookup for the sequenced SID voices: one
// registered read of the four pinned reSID sample tables per SYS edge. The
// pulse/triangle table is indexed with the requesting voice's ring XOR.
// Table data and provenance: c64_sid_combined_waves.vh and c64_sid.COPYING.
// mem2reg keeps the sparse tables in logic, bypassing the pinned Intel legacy
// BRAM mapper that would otherwise infer asynchronous M10K reads.
module c64_sid_wave_tables(
    input wire clk,
    input wire [11:0] phase_index,
    input wire ring_xor,
    output reg [47:0] lookup_tables
);
    `include "c64_sid_combined_waves.vh"
    (* ram_style = "logic", mem2reg *) reg [11:0] triangle_saw_words[0:4095];
    (* ram_style = "logic", mem2reg *) reg [11:0] pulse_triangle_words[0:4095];
    (* ram_style = "logic", mem2reg *) reg [11:0] pulse_saw_words[0:4095];
    (* ram_style = "logic", mem2reg *) reg [11:0] pulse_triangle_saw_words[0:4095];
    integer table_index;
    initial for(table_index=0;table_index<4096;table_index=table_index+1) begin
        triangle_saw_words[table_index]=combined_triangle_saw(12'(table_index));
        pulse_triangle_words[table_index]=combined_pulse_triangle(12'(table_index));
        pulse_saw_words[table_index]=combined_pulse_saw(12'(table_index));
        pulse_triangle_saw_words[table_index]=combined_pulse_triangle_saw(12'(table_index));
    end
    always @(posedge clk)
        lookup_tables<={triangle_saw_words[phase_index],
                        pulse_triangle_words[phase_index ^ {ring_xor,11'b0}],
                        pulse_saw_words[phase_index],
                        pulse_triangle_saw_words[phase_index]};
endmodule
