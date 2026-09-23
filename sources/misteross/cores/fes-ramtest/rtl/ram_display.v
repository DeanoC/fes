// SPDX-License-Identifier: GPL-2.0-or-later
// Two status bars on the 320x240 playfield. The top bar is the SDRAM addon
// and the bottom bar is HPS DDR. Green is pass, red is fail, amber is running.
module ram_display (
    input wire pixel_clk,
    input wire [9:0] x,
    input wire [9:0] y,
    input wire active,
    input wire sdram_pass,
    input wire sdram_fail,
    input wire hps_pass,
    input wire hps_fail,
    output reg [7:0] red,
    output reg [7:0] green,
    output reg [7:0] blue
);
    reg [1:0] sdram_pass_sync = 2'b00;
    reg [1:0] sdram_fail_sync = 2'b00;
    reg [1:0] hps_pass_sync = 2'b00;
    reg [1:0] hps_fail_sync = 2'b00;

    wire sdram_pass_s = sdram_pass_sync[1];
    wire sdram_fail_s = sdram_fail_sync[1];
    wire hps_pass_s = hps_pass_sync[1];
    wire hps_fail_s = hps_fail_sync[1];
    wire sdram_bar = active && x >= 10'd32 && x < 10'd288 && y >= 10'd40 && y < 10'd96;
    wire hps_bar = active && x >= 10'd32 && x < 10'd288 && y >= 10'd128 && y < 10'd184;
    wire [7:0] sdram_red = sdram_fail_s ? 8'hE0 : (sdram_pass_s ? 8'h20 : 8'hE0);
    wire [7:0] sdram_green = sdram_fail_s ? 8'h30 : (sdram_pass_s ? 8'hC0 : 8'hA0);
    wire [7:0] sdram_blue = sdram_fail_s ? 8'h28 : (sdram_pass_s ? 8'h40 : 8'h20);
    wire [7:0] hps_red = hps_fail_s ? 8'hE0 : (hps_pass_s ? 8'h20 : 8'hE0);
    wire [7:0] hps_green = hps_fail_s ? 8'h30 : (hps_pass_s ? 8'hC0 : 8'hA0);
    wire [7:0] hps_blue = hps_fail_s ? 8'h28 : (hps_pass_s ? 8'h40 : 8'h20);

    always @(posedge pixel_clk) begin
        sdram_pass_sync <= {sdram_pass_sync[0], sdram_pass};
        sdram_fail_sync <= {sdram_fail_sync[0], sdram_fail};
        hps_pass_sync <= {hps_pass_sync[0], hps_pass};
        hps_fail_sync <= {hps_fail_sync[0], hps_fail};
        if (sdram_bar) begin
            red <= sdram_red;
            green <= sdram_green;
            blue <= sdram_blue;
        end else if (hps_bar) begin
            red <= hps_red;
            green <= hps_green;
            blue <= hps_blue;
        end else begin
            red <= 8'h10;
            green <= 8'h14;
            blue <= 8'h18;
        end
    end
endmodule
