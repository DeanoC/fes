// SPDX-License-Identifier: GPL-2.0-or-later
// Capture a 256x192 Coleco logical raster and present it as centered 2x
// graphics in a deterministic 1280x720p60 HDMI timing shell.

module coleco_video_720p (
    input  wire       clk_sys,
    input  wire       pixel_clk,
    input  wire       raster_ce,
    input  wire [7:0] logical_x,
    input  wire [7:0] logical_y,
    input  wire [1:0] logical_pixel,
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
    wire [1:0] framebuffer_q;

    coleco_video_dpram #(
        .DATAWIDTH(2),
        .ADDRWIDTH(16),
        .NUMWORDS(49152)
    ) framebuffer (
        .clock_a(clk_sys),
        .address_a(logical_address),
        .data_a(logical_pixel),
        .wren_a(logical_write),
        .clock_b(pixel_clk),
        .address_b(read_address),
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
            case (framebuffer_q)
                2'd1: begin
                    red = 8'hff;
                    green = 8'h40;
                end
                2'd2: begin
                    green = 8'hff;
                    blue = 8'h40;
                end
                2'd3: begin
                    red = 8'hff;
                    green = 8'hff;
                    blue = 8'hff;
                end
                default: begin
                    red = 8'h00;
                    green = 8'h00;
                    blue = 8'h00;
                end
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
