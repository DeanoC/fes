// SPDX-License-Identifier: GPL-2.0-or-later
// Logic operand 1 only. Post-synthesis map: six parallel masked groups followed by one six-input OR.
// Applying this after ABC prevents area factoring from adding routing stages.
(* techmap_celltype = "pcxt_logic_operand1_mux" *)
module _pcxt_logic_operand1_mux_native (
    input wire [15:0] sel,
    input wire [255:0] data,
    output wire [15:0] out
);
    genvar b, g;
    generate for (b = 0; b < 16; b = b + 1) begin: lanes
        wire [5:0] groups;
        for (g = 0; g < 5; g = g + 1) begin: triples
            // (A & B) | (C & D) | (E & F), indexed as {F,E,D,C,B,A}.
            // Source 7 is the measured late BIU-return input. Put it on
            // E/F in its group; pairwise OR is symmetric under this swap.
            localparam integer MIDDLE = g == 2 ? 3*g+2 : 3*g+1;
            localparam integer LAST = g == 2 ? 3*g+1 : 3*g+2;
            MISTRAL_ALUT6 #(.LUT(64'hfffff888f888f888)) masked_three (
                .A(data[(3*g)*16+b]), .B(sel[3*g]),
                .C(data[MIDDLE*16+b]), .D(sel[MIDDLE]),
                .E(data[LAST*16+b]), .F(sel[LAST]), .Q(groups[g]));
        end
        MISTRAL_ALUT2 #(.LUT(4'h8)) masked_last (
            .A(data[240+b]), .B(sel[15]), .Q(groups[5]));
        MISTRAL_ALUT6 #(.LUT(64'hfffffffffffffffe)) combine (
            .A(groups[0]), .B(groups[1]), .C(groups[3]),
            .D(groups[4]), .E(groups[2]), .F(groups[5]), .Q(out[b]));
    end endgenerate
endmodule
