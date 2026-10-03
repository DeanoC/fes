// SPDX-License-Identifier: GPL-2.0-or-later
// TMS9918 indexed4 source. The registered VDP coordinates stay for at least
// twelve system clocks. Sample once, three clocks after a coordinate/blank
// transition, after its registered pattern/color and sprite RAM reads settle.
// No framebuffer, HDMI geometry or output effect belongs to this adapter.
module coleco_native_video (
    input wire clk_sys,
    input wire hold,
    input wire [7:0] logical_x,
    input wire [7:0] logical_y,
    input wire [3:0] logical_pixel,
    input wire logical_blank,
    output reg [31:0] request = 0
);
    reg [7:0] previous_x = 0, previous_y = 0;
    reg previous_blank = 1;
    reg previous_hold = 0, control_initialized = 0;
    reg [1:0] settle = 0;
    always @(posedge clk_sys) begin
        request <= 0;
        previous_x <= logical_x;
        previous_y <= logical_y;
        previous_blank <= logical_blank;
        previous_hold <= hold;
        if (!control_initialized || hold != previous_hold) begin
            control_initialized <= 1;
            request <= {2'b0, 1'b1, hold, 3'b0, 1'b1, 24'b0};
            settle <= 0;
            previous_blank <= 1;
        end else if (hold || logical_blank) begin
            settle <= 0;
        end else if (previous_blank || logical_x != previous_x || logical_y != previous_y) begin
            settle <= 3;
        end else if (settle != 0) begin
            settle <= settle - 1'b1;
            if (settle == 1)
                request <= {2'b0, 2'b0,
                            (logical_x == 255 && logical_y == 191),
                            (logical_x == 255),
                            (logical_x == 0 && logical_y == 0),
                            1'b1, 20'b0, logical_pixel};
        end
    end
endmodule
