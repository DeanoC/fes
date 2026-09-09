// SPDX-License-Identifier: GPL-2.0-or-later

// Fixed CTA-770.3 1280x720p60 raster at a 74.25 MHz pixel clock.
// A 320x240 game image is scaled 3x and centered between 160-pixel black bars.
module video_720p (
    input  wire        pixel_clk,
    input  wire [7:0]  game_red,
    input  wire [7:0]  game_green,
    input  wire [7:0]  game_blue,
    output wire [9:0]  playfield_x,
    output wire [9:0]  playfield_y,
    output wire        playfield_active,
    output wire [7:0]  red,
    output wire [7:0]  green,
    output wire [7:0]  blue,
    output wire        de,
    output wire        hsync,
    output wire        vsync,
    output wire        frame_tick
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_FRONT = 11'd110;
    localparam [10:0] H_SYNC = 11'd40;
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_ACTIVE = 10'd720;
    localparam [9:0] V_FRONT = 10'd5;
    localparam [9:0] V_SYNC = 10'd5;
    localparam [9:0] V_TOTAL = 10'd750;
    localparam [10:0] PLAYFIELD_LEFT = 11'd160;
    localparam [10:0] PLAYFIELD_RIGHT = 11'd1120;

    reg [10:0] horizontal;
    reg [9:0] vertical;
    wire [10:0] centered_x = horizontal - PLAYFIELD_LEFT;
    /* verilator lint_off UNUSEDSIGNAL */
    wire [10:0] scaled_x = centered_x / 11'd3;
    /* verilator lint_on UNUSEDSIGNAL */
    wire [9:0] scaled_y = vertical / 10'd3;

    assign de = horizontal < H_ACTIVE && vertical < V_ACTIVE;
    assign hsync = horizontal >= H_ACTIVE + H_FRONT &&
                   horizontal < H_ACTIVE + H_FRONT + H_SYNC;
    assign vsync = vertical >= V_ACTIVE + V_FRONT &&
                   vertical < V_ACTIVE + V_FRONT + V_SYNC;
    assign frame_tick = horizontal == H_TOTAL - 1'b1 &&
                        vertical == V_TOTAL - 1'b1;
    assign playfield_active = de && horizontal >= PLAYFIELD_LEFT &&
                              horizontal < PLAYFIELD_RIGHT;
    assign playfield_x = playfield_active ? scaled_x[9:0] : 10'd0;
    assign playfield_y = playfield_active ? scaled_y : 10'd0;
    assign red = playfield_active ? game_red : 8'h00;
    assign green = playfield_active ? game_green : 8'h00;
    assign blue = playfield_active ? game_blue : 8'h00;

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
