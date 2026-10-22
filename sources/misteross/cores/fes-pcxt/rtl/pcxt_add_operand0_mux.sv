// SPDX-License-Identifier: GPL-2.0-or-later
// Keep ADD operand 0 independent of the logic operand mux. The native recipe
// maps this exact masked OR after generic synthesis, without adding a stage.
(* keep_hierarchy *)
module pcxt_add_operand0_mux (
    input wire [15:0] sel,
    input wire [255:0] data,
    output reg [15:0] out
);
    integer i;
    always @* begin
        out = 0;
        for (i = 0; i < 16; i = i + 1)
            out = out | (data[i*16 +: 16] & {16{sel[i]}});
    end
endmodule
