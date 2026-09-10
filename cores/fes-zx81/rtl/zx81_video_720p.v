// SPDX-License-Identifier: GPL-2.0-or-later
// Capture the ZX81 6.5 MHz raster and present it integer-scaled in 1280x720p60.

module zx81_video_720p (
    input  wire        clk_sys,
    input  wire        ce_6m5,
    input  wire        zx_pixel,
    input  wire        hblank,
    input  wire        vblank,
    input  wire        pixel_clk,
    output wire [7:0]  red,
    output wire [7:0]  green,
    output wire [7:0]  blue,
    output wire        de,
    output wire        hsync,
    output wire        vsync,
    output wire        frame_tick,
    output reg  [9:0]  src_x_max,
    output reg  [8:0]  src_y_max
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_FRONT = 11'd110;
    localparam [10:0] H_SYNC = 11'd40;
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_ACTIVE = 10'd720;
    localparam [9:0] V_FRONT = 10'd5;
    localparam [9:0] V_SYNC = 10'd5;
    localparam [9:0] V_TOTAL = 10'd750;
    localparam [9:0] SRC_W = 10'd328;
    localparam [8:0] SRC_H = 9'd270;
    localparam [10:0] LEFT = 11'd312;
    localparam [9:0] TOP = 10'd90;

    reg [9:0] cap_x;
    reg [8:0] cap_y;
    reg old_hblank;
    (* ramstyle = "M10K" *) reg fb [0:269][0:511];

    always @(posedge clk_sys) begin
        if (ce_6m5) begin
            old_hblank <= hblank;
            if (vblank) begin
                cap_x <= 0;
                cap_y <= 0;
            end else if (~old_hblank & hblank) begin
                if (cap_x > src_x_max) src_x_max <= cap_x;
                if (cap_y > src_y_max) src_y_max <= cap_y;
                cap_x <= 0;
                if (cap_y != SRC_H - 1'b1)
                    cap_y <= cap_y + 1'b1;
            end else if (!hblank && !vblank && cap_x < SRC_W) begin
                fb[cap_y][cap_x] <= zx_pixel;
                cap_x <= cap_x + 1'b1;
            end
        end
    end

    reg [10:0] horizontal;
    reg [9:0] vertical;

    assign de = horizontal < H_ACTIVE && vertical < V_ACTIVE;
    assign hsync = horizontal >= H_ACTIVE + H_FRONT &&
                   horizontal < H_ACTIVE + H_FRONT + H_SYNC;
    assign vsync = vertical >= V_ACTIVE + V_FRONT &&
                   vertical < V_ACTIVE + V_FRONT + V_SYNC;
    assign frame_tick = horizontal == H_TOTAL - 1'b1 &&
                        vertical == V_TOTAL - 1'b1;

    wire [10:0] src_x = (horizontal - LEFT) / 11'd2;
    wire [9:0] src_y = (vertical - TOP) / 10'd2;
    wire in_image = de &&
                    horizontal >= LEFT &&
                    horizontal < LEFT + (SRC_W << 1) &&
                    vertical >= TOP &&
                    vertical < TOP + (SRC_H << 1);
    wire pixel = in_image && src_x < {1'b0, SRC_W} && src_y < {1'b0, SRC_H} &&
                 fb[src_y[8:0]][src_x[8:0]];
    assign red = pixel ? 8'hff : 8'h00;
    assign green = pixel ? 8'hff : 8'h00;
    assign blue = pixel ? 8'hff : 8'h00;

    initial begin
        horizontal = 0;
        vertical = 0;
        src_x_max = 0;
        src_y_max = 0;
    end

    always @(posedge pixel_clk) begin
        if (horizontal == H_TOTAL - 1'b1) begin
            horizontal <= 0;
            if (vertical == V_TOTAL - 1'b1)
                vertical <= 0;
            else
                vertical <= vertical + 1'b1;
        end else
            horizontal <= horizontal + 1'b1;
    end
endmodule
