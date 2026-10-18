// SPDX-License-Identifier: GPL-2.0-or-later
// Post-synthesis map: six parallel masked groups followed by one six-input OR.
// Applying this after ABC prevents area factoring from adding routing stages.
(* techmap_celltype = "pcxt_select16" *)
module _pcxt_select16_native (
    input wire [15:0] sel,
    input wire [255:0] data,
    output wire [15:0] out
);
    genvar b, g;
    generate for (b = 0; b < 16; b = b + 1) begin: lanes
        wire [5:0] groups;
        for (g = 0; g < 5; g = g + 1) begin: triples
            // (A & B) | (C & D) | (E & F), indexed as {F,E,D,C,B,A}.
            MISTRAL_ALUT6 #(.LUT(64'hfffff888f888f888)) masked_three (
                .A(data[(3*g)*16+b]), .B(sel[3*g]),
                .C(data[(3*g+1)*16+b]), .D(sel[3*g+1]),
                .E(data[(3*g+2)*16+b]), .F(sel[3*g+2]), .Q(groups[g]));
        end
        MISTRAL_ALUT2 #(.LUT(4'h8)) masked_last (
            .A(data[240+b]), .B(sel[15]), .Q(groups[5]));
        MISTRAL_ALUT6 #(.LUT(64'hfffffffffffffffe)) combine (
            .A(groups[0]), .B(groups[1]), .C(groups[2]),
            .D(groups[3]), .E(groups[4]), .F(groups[5]), .Q(out[b]));
    end endgenerate
endmodule
