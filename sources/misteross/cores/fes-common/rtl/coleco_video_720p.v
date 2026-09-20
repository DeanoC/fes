// SPDX-License-Identifier: GPL-2.0-or-later
// Capture a 256x192 Coleco logical raster and present it as centered 2x
// graphics in a deterministic 1280x720p60 HDMI timing shell.

module coleco_video_720p (
    input  wire       clk_sys,
    input  wire       pixel_clk,
    input  wire       raster_ce,
    input  wire [7:0] logical_x,
    input  wire [7:0] logical_y,
    input  wire [3:0] logical_pixel,
    input  wire       logical_blank,
    output reg  [7:0] red,
    output reg  [7:0] green,
    output reg  [7:0] blue,
    output wire       de,
    output wire       hsync,
    output wire       vsync,
    output wire       frame_tick
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_FRONT = 11'd110;
    localparam [10:0] H_SYNC = 11'd40;
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_ACTIVE = 10'd720;
    localparam [9:0] V_FRONT = 10'd5;
    localparam [9:0] V_SYNC = 10'd5;
    localparam [9:0] V_TOTAL = 10'd750;
    localparam [10:0] IMAGE_LEFT = 11'd384;
    localparam [9:0] IMAGE_TOP = 10'd168;
    localparam [10:0] IMAGE_WIDTH = 11'd512;
    localparam [9:0] IMAGE_HEIGHT = 10'd384;

    wire logical_write = raster_ce && !logical_blank;
    wire [15:0] logical_address = {logical_y, logical_x};

    reg [10:0] horizontal;
    reg [9:0] vertical;
    wire image_active = de &&
                        horizontal >= IMAGE_LEFT &&
                        horizontal < IMAGE_LEFT + IMAGE_WIDTH &&
                        vertical >= IMAGE_TOP &&
                        vertical < IMAGE_TOP + IMAGE_HEIGHT;
    wire [10:0] image_x = (horizontal - IMAGE_LEFT) >> 1;
    wire [9:0] image_y = (vertical - IMAGE_TOP) >> 1;
    wire [15:0] read_address = image_active ?
                                {image_y[7:0], image_x[7:0]} : 16'd0;
    wire [3:0] framebuffer_q_a;
    wire [3:0] framebuffer_q;

    coleco_video_dpram #(
        .DATAWIDTH(4),
        .ADDRWIDTH(16),
        .NUMWORDS(49152)
    ) framebuffer (
        .clock_a(clk_sys),
        .address_a(logical_address),
        .data_a(logical_pixel),
        .wren_a(logical_write),
        .q_a(framebuffer_q_a),
        .clock_b(pixel_clk),
        .address_b(read_address),
        .data_b(4'd0),
        .wren_b(1'b0),
        .q_b(framebuffer_q)
    );

    assign de = horizontal < H_ACTIVE && vertical < V_ACTIVE;
    assign hsync = horizontal >= H_ACTIVE + H_FRONT &&
                   horizontal < H_ACTIVE + H_FRONT + H_SYNC;
    assign vsync = vertical >= V_ACTIVE + V_FRONT &&
                   vertical < V_ACTIVE + V_FRONT + V_SYNC;
    assign frame_tick = horizontal == H_TOTAL - 1'b1 &&
                        vertical == V_TOTAL - 1'b1;

    always @* begin
        red = 8'h00;
        green = 8'h00;
        blue = 8'h00;
        if (image_active) begin
            // Fixed RGB approximation of the sixteen TMS9918 color codes.
            case (framebuffer_q)
                4'd2: {red,green,blue} = 24'h21c842;
                4'd3: {red,green,blue} = 24'h5edc78;
                4'd4: {red,green,blue} = 24'h5455ed;
                4'd5: {red,green,blue} = 24'h7d76fc;
                4'd6: {red,green,blue} = 24'hd4524d;
                4'd7: {red,green,blue} = 24'h42ebf5;
                4'd8: {red,green,blue} = 24'hfc5554;
                4'd9: {red,green,blue} = 24'hff7978;
                4'd10: {red,green,blue} = 24'hd4c154;
                4'd11: {red,green,blue} = 24'he6ce80;
                4'd12: {red,green,blue} = 24'h21b03b;
                4'd13: {red,green,blue} = 24'hc95bba;
                4'd14: {red,green,blue} = 24'hcccccc;
                4'd15: {red,green,blue} = 24'hffffff;
                default: {red,green,blue} = 24'h000000;
            endcase
        end
    end

    initial begin
        horizontal = 11'd0;
        vertical = 10'd0;
    end

    always @(posedge pixel_clk) begin
        if (horizontal == H_TOTAL - 1'b1) begin
            horizontal <= 11'd0;
            if (vertical == V_TOTAL - 1'b1)
                vertical <= 10'd0;
            else
                vertical <= vertical + 1'b1;
        end else begin
            horizontal <= horizontal + 1'b1;
        end
    end
endmodule
