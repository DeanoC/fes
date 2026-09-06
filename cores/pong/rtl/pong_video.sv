// SPDX-License-Identifier: GPL-2.0-or-later
// Continuous 320x240 raster: 20 MHz / (3 * 424 * 262), approximately 60.01 Hz.
// Game reset deliberately does not stop video synchronization.
module pong_video (
    input logic clk,
    output logic ce_pixel,
    output logic frame_tick,
    output logic [9:0] x,
    output logic [9:0] y,
    output logic active,
    output logic hsync,
    output logic vsync
);
    logic [1:0] divider;
    initial begin
        x = 0;
        y = 0;
        divider = 0;
    end
    assign ce_pixel = (divider == 2);
    assign frame_tick = ce_pixel && x == 423 && y == 261;
    assign active = x < 320 && y < 240;
    assign hsync = x >= 336 && x < 368;
    assign vsync = y >= 244 && y < 247;

    always @(posedge clk) begin
        if (ce_pixel) begin
            divider <= 0;
            if (x == 423) begin
                x <= 0;
                y <= (y == 261) ? 10'd0 : y + 10'd1;
            end else x <= x + 10'd1;
        end else divider <= divider + 2'd1;
    end
endmodule
