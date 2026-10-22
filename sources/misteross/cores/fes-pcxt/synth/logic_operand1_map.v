// SPDX-License-Identifier: GPL-2.0-or-later
// Logic operand 1 only. Post-synthesis map: six parallel masked groups followed by one six-input OR.
// Applying this after ABC prevents area factoring from adding routing stages.
(* techmap_celltype = "pcxt_logic_operand1_mux" *)
module _pcxt_logic_operand1_mux_native #(
    parameter [15:0] _TECHMAP_CONSTMSK_sel_ = 0,
    parameter [15:0] _TECHMAP_CONSTVAL_sel_ = 0,
    parameter [255:0] _TECHMAP_CONSTMSK_data_ = 0,
    parameter [255:0] _TECHMAP_CONSTVAL_data_ = 0
) (
    input wire [15:0] sel,
    input wire [255:0] data,
    output wire [15:0] out
);
    genvar b, g, i;
    generate for (b = 0; b < 16; b = b + 1) begin: lanes
        wire [5:0] groups;
        wire [15:0] lane_sel;
        wire [15:0] lane_data;
        for (i = 0; i < 16; i = i + 1) begin: constants
            // Disconnect only provably zero product terms. Native primitive
            // timing arcs otherwise retain unused status/selector paths even
            // when their other AND input is tied low.
            localparam ZERO_SELECT = _TECHMAP_CONSTMSK_sel_[i] &&
                (_TECHMAP_CONSTVAL_sel_[i] === 1'b0);
            localparam ZERO_DATA = _TECHMAP_CONSTMSK_data_[i*16+b] &&
                (_TECHMAP_CONSTVAL_data_[i*16+b] === 1'b0);
            assign lane_data[i] = ZERO_SELECT ? 1'b0 : data[i*16+b];
            assign lane_sel[i] = ZERO_DATA ? 1'b0 : sel[i];
        end
        for (g = 0; g < 5; g = g + 1) begin: triples
            // (A & B) | (C & D) | (E & F), indexed as {F,E,D,C,B,A}.
            // Source 7 is the measured late BIU-return input. Put it on
            // E/F in its group; pairwise OR is symmetric under this swap.
            localparam integer MIDDLE = g == 2 ? 3*g+2 : 3*g+1;
            localparam integer LAST = g == 2 ? 3*g+1 : 3*g+2;
            MISTRAL_ALUT6 #(.LUT(64'hfffff888f888f888)) masked_three (
                .A(lane_data[3*g]), .B(lane_sel[3*g]),
                .C(lane_data[MIDDLE]), .D(lane_sel[MIDDLE]),
                .E(lane_data[LAST]), .F(lane_sel[LAST]), .Q(groups[g]));
        end
        MISTRAL_ALUT2 #(.LUT(4'h8)) masked_last (
            .A(lane_data[15]), .B(lane_sel[15]), .Q(groups[5]));
        MISTRAL_ALUT6 #(.LUT(64'hfffffffffffffffe)) combine (
            .A(groups[0]), .B(groups[1]), .C(groups[3]),
            .D(groups[4]), .E(groups[2]), .F(groups[5]), .Q(out[b]));
    end endgenerate
endmodule
