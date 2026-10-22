// SPDX-License-Identifier: GPL-2.0-or-later
// Preserve this combinational boundary through generic synthesis. The native
// recipe replaces it with the proved two-level LUT map before device packing.
(* keep_hierarchy *)
module pcxt_logic_operand1_mux (
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
