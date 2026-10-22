// SPDX-License-Identifier: GPL-2.0-or-later
// Map the masked OR for arbitrary controls, including zero/multiple selects.
// Compact only products proved zero from synthesis-time pin constants. Each
// remaining triple uses one LUT6, followed by one OR LUT for at most six groups.
(* techmap_celltype = "pcxt_add_operand0_mux" *)
module _pcxt_add_operand0_mux_native #(
    parameter [15:0] _TECHMAP_CONSTMSK_sel_ = 0,
    parameter [15:0] _TECHMAP_CONSTVAL_sel_ = 0,
    parameter [255:0] _TECHMAP_CONSTMSK_data_ = 0,
    parameter [255:0] _TECHMAP_CONSTVAL_data_ = 0
) (
    input wire [15:0] sel,
    input wire [255:0] data,
    output wire [15:0] out
);
    function [15:0] active_mask(input integer lane);
        integer i;
        begin
            for (i = 0; i < 16; i = i + 1)
                active_mask[i] = !((_TECHMAP_CONSTMSK_sel_[i] &&
                    (_TECHMAP_CONSTVAL_sel_[i] === 1'b0)) ||
                    (_TECHMAP_CONSTMSK_data_[i*16+lane] &&
                    (_TECHMAP_CONSTVAL_data_[i*16+lane] === 1'b0)));
        end
    endfunction

    function integer count_active(input [15:0] active);
        integer i;
        begin
            count_active = 0;
            for (i = 0; i < 16; i = i + 1)
                count_active = count_active + active[i];
        end
    endfunction

    function integer source_index(input [15:0] active, input integer rank);
        integer i, count;
        begin
            source_index = 0;
            count = 0;
            for (i = 0; i < 16; i = i + 1)
                if (active[i]) begin
                    if (count == rank) source_index = i;
                    count = count + 1;
                end
        end
    endfunction

    genvar b, g;
    generate for (b = 0; b < 16; b = b + 1) begin: lanes
        localparam [15:0] ACTIVE = active_mask(b);
        localparam integer TERMS = count_active(ACTIVE);
        localparam integer GROUPS = (TERMS + 2) / 3;
        wire [5:0] groups;
        for (g = 0; g < 6; g = g + 1) begin: products
            if (3*g >= TERMS) begin
                assign groups[g] = 1'b0;
            end else begin
                localparam integer A = source_index(ACTIVE, 3*g);
                if (3*g + 1 == TERMS) begin
                    MISTRAL_ALUT2 #(.LUT(4'h8)) masked_one (
                        .A(data[A*16+b]), .B(sel[A]), .Q(groups[g]));
                end else begin
                    localparam integer B = source_index(ACTIVE, 3*g+1);
                    if (3*g + 2 == TERMS) begin
                        MISTRAL_ALUT4 #(.LUT(16'hf888)) masked_two (
                            .A(data[A*16+b]), .B(sel[A]),
                            .C(data[B*16+b]), .D(sel[B]), .Q(groups[g]));
                    end else begin
                        localparam integer C = source_index(ACTIVE, 3*g+2);
                        MISTRAL_ALUT6 #(.LUT(64'hfffff888f888f888)) masked_three (
                            .A(data[A*16+b]), .B(sel[A]),
                            .C(data[B*16+b]), .D(sel[B]),
                            .E(data[C*16+b]), .F(sel[C]), .Q(groups[g]));
                    end
                end
            end
        end
        if (GROUPS == 0) assign out[b] = 1'b0;
        else if (GROUPS == 1) assign out[b] = groups[0];
        else if (GROUPS == 2)
            MISTRAL_ALUT2 #(.LUT(4'he)) combine (
                .A(groups[0]), .B(groups[1]), .Q(out[b]));
        else if (GROUPS == 3)
            MISTRAL_ALUT3 #(.LUT(8'hfe)) combine (
                .A(groups[0]), .B(groups[1]), .C(groups[2]), .Q(out[b]));
        else if (GROUPS == 4)
            MISTRAL_ALUT4 #(.LUT(16'hfffe)) combine (
                .A(groups[0]), .B(groups[1]), .C(groups[2]), .D(groups[3]), .Q(out[b]));
        else if (GROUPS == 5)
            MISTRAL_ALUT5 #(.LUT(32'hfffffffe)) combine (
                .A(groups[0]), .B(groups[1]), .C(groups[2]), .D(groups[3]),
                .E(groups[4]), .Q(out[b]));
        else
            MISTRAL_ALUT6 #(.LUT(64'hfffffffffffffffe)) combine (
                .A(groups[0]), .B(groups[1]), .C(groups[2]), .D(groups[3]),
                .E(groups[4]), .F(groups[5]), .Q(out[b]));
    end endgenerate
endmodule
