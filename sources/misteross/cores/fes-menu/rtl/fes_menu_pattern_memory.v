// SPDX-License-Identifier: GPL-2.0-or-later
// Synthetic Avalon read responder: exercises the real reader/FIFO without DDR.
module fes_menu_pattern_memory (
    input wire clk,
    input wire [27:0] address,
    input wire [7:0] burstcount,
    input wire read,
    output wire waitrequest,
    output wire [127:0] readdata,
    output wire readdatavalid
);
    reg [7:0] left = 8'd0;
    reg [10:0] x = 11'd0;
    reg [9:0] y = 10'd0;
    reg slot = 1'b0;
    assign waitrequest = left != 8'd0;
    assign readdatavalid = left != 8'd0;
    function [31:0] pixel;
        input [10:0] px;
        input [9:0] py;
        input bank;
        begin
            if (px == 11'd0 || px == 11'd1279 || py == 10'd0 || py == 10'd719)
                pixel = 32'h00ffffff;
            else if (px < 11'd64 && py < 10'd64)
                pixel = bank ? 32'h00ff00ff : 32'h0000ff00;
            else if (py >= 10'd560)
                pixel = (px[4] ^ py[4]) ? 32'h00ffffff : 32'd0;
            else if (px < 11'd160) pixel = 32'h00ffffff;
            else if (px < 11'd320) pixel = 32'h00ffff00;
            else if (px < 11'd480) pixel = 32'h0000ffff;
            else if (px < 11'd640) pixel = 32'h0000ff00;
            else if (px < 11'd800) pixel = 32'h00ff00ff;
            else if (px < 11'd960) pixel = 32'h00ff0000;
            else if (px < 11'd1120) pixel = 32'h000000ff;
            else pixel = 32'd0;
        end
    endfunction
    assign readdata = {pixel(x + 11'd3, y, slot), pixel(x + 11'd2, y, slot),
                      pixel(x + 11'd1, y, slot), pixel(x, y, slot)};
    always @(posedge clk) begin
        if (read && !waitrequest) begin
            left <= burstcount;
            // Private synthetic address space: slot bases 0x1000 and 0x401000.
            if (address == 28'h0000100 || address == 28'h0040100) begin
                x <= 11'd0;
                y <= 10'd0;
                slot <= address == 28'h0040100;
            end
        end else if (readdatavalid) begin
            left <= left - 8'd1;
            if (x == 11'd1276) begin
                x <= 11'd0;
                y <= y + 10'd1;
            end else x <= x + 11'd4;
        end
    end
endmodule
