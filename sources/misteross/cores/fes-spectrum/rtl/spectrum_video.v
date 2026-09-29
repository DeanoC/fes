// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum picture scanned from RAM in the 74.25 MHz HDMI domain.
//
// There is no frame buffer. The 256x192 bitmap is scaled 4x by 3x to
// 1024x576 and centred (128 pixels left/right, 72 lines top/bottom). The
// rest of the active raster is the border colour. Attribute flash follows
// the machine's 50 Hz frame, sampled into this clock. This is not a
// cycle-exact ULA raster: the CPU does not contend with the scanner.
module spectrum_video (
    input  wire        pixel_clk,
    input  wire [2:0]  border,
    input  wire        flash_on,
    output reg  [15:0] ram_addr,
    input  wire [7:0]  ram_data,
    output reg  [7:0]  red,
    output reg  [7:0]  green,
    output reg  [7:0]  blue,
    output reg         de,
    output reg         hsync,
    output reg         vsync,
    output reg  [10:0] pixel_x,
    output reg  [9:0]  pixel_y
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_FRONT = 11'd110;
    localparam [10:0] H_SYNC = 11'd40;
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_ACTIVE = 10'd720;
    localparam [9:0] V_FRONT = 10'd5;
    localparam [9:0] V_SYNC = 10'd5;
    localparam [9:0] V_TOTAL = 10'd750;
    localparam [10:0] IMAGE_LEFT = 11'd128;
    localparam [9:0] IMAGE_TOP = 10'd72;
    localparam [10:0] PREFETCH_X = 11'd124;

    reg [10:0] h = 11'd0;
    reg [9:0] v = 10'd0;
    reg [7:0] sy = 8'd0;
    reg [1:0] sub = 2'd0;
    (* async_reg = "true" *) reg [2:0] border_meta = 3'd7;
    (* async_reg = "true" *) reg [2:0] border_sync = 3'd7;
    (* async_reg = "true" *) reg flash_meta = 1'b0;
    (* async_reg = "true" *) reg flash_sync = 1'b0;
    reg [7:0] bitmap_hold = 8'h00;
    reg [7:0] attr_hold = 8'h00;
    reg [7:0] bitmap_q = 8'h00;
    reg [7:0] group_x = 8'h00;
    reg [7:0] group_y = 8'h00;
    reg [1:0] fetch = 2'd0;

    wire line_in_image = v >= IMAGE_TOP && v < IMAGE_TOP + 10'd576;
    wire [10:0] ahead = h - PREFETCH_X;
    wire prefetch = h >= PREFETCH_X && h < PREFETCH_X + 11'd1024 && ahead[4:0] == 5'd0 && line_in_image;
    wire [7:0] prefetch_x = {ahead[10:5], 3'b000};

    function [15:0] bitmap_address;
        input [7:0] x;
        input [7:0] y;
        begin
            bitmap_address = 16'h4000 + {y[7:6], y[2:0], y[5:3], x[7:3]};
        end
    endfunction

    function [15:0] attr_address;
        input [7:0] x;
        input [7:0] y;
        begin
            attr_address = 16'h5800 + {y[7:3], x[7:3]};
        end
    endfunction

    function [23:0] palette;
        input [2:0] color;
        input bright;
        begin
            case (color)
                3'd1: palette = bright ? 24'h0000ff : 24'h0000cd;
                3'd2: palette = bright ? 24'hff0000 : 24'hcd0000;
                3'd3: palette = bright ? 24'hff00ff : 24'hcd00cd;
                3'd4: palette = bright ? 24'h00ff00 : 24'h00cd00;
                3'd5: palette = bright ? 24'h00ffff : 24'h00cdcd;
                3'd6: palette = bright ? 24'hffff00 : 24'hcdcd00;
                3'd7: palette = bright ? 24'hffffff : 24'hcdcdcd;
                default: palette = 24'h000000;
            endcase
        end
    endfunction

    reg [23:0] pixel_rgb;
    wire in_picture = h >= IMAGE_LEFT && h < IMAGE_LEFT + 11'd1024 && line_in_image;
    wire [10:0] px = h - IMAGE_LEFT;
    wire [2:0] bit_index = 3'd7 - px[4:2];
    wire ink = bitmap_hold[bit_index];
    wire flash = attr_hold[7] && flash_sync;
    wire [2:0] ink_color = flash ? attr_hold[5:3] : attr_hold[2:0];
    wire [2:0] paper_color = flash ? attr_hold[2:0] : attr_hold[5:3];
    wire [23:0] picture_rgb = palette(ink ? ink_color : paper_color, attr_hold[6]);
    wire [23:0] border_rgb = palette(border_sync, 1'b0);

    always @* pixel_rgb = in_picture ? picture_rgb : border_rgb;

    always @(posedge pixel_clk) begin
        border_meta <= border;
        border_sync <= border_meta;
        flash_meta <= flash_on;
        flash_sync <= flash_meta;

        if (h == H_TOTAL - 1'b1) begin
            h <= 11'd0;
            v <= (v == V_TOTAL - 1'b1) ? 10'd0 : v + 1'b1;
            if (v == IMAGE_TOP - 1'b1) begin
                sy <= 8'd0;
                sub <= 2'd0;
            end else if (line_in_image) begin
                if (sub == 2'd2) begin
                    sub <= 2'd0;
                    sy <= sy + 8'd1;
                end else begin
                    sub <= sub + 2'd1;
                end
            end
        end else begin
            h <= h + 1'b1;
        end

        // RAM data is visible two clocks after the address is presented.
        // Issue the bitmap, then the attribute, and hold both on the clock
        // before the 32-pixel group they describe.
        if (prefetch) begin
            group_x <= prefetch_x;
            group_y <= sy;
            ram_addr <= bitmap_address(prefetch_x, sy);
            fetch <= 2'd1;
        end else if (fetch == 2'd1) begin
            ram_addr <= attr_address(group_x, group_y);
            fetch <= 2'd2;
        end else if (fetch == 2'd2) begin
            bitmap_q <= ram_data;
            fetch <= 2'd3;
        end else if (fetch == 2'd3) begin
            bitmap_hold <= bitmap_q;
            attr_hold <= ram_data;
            fetch <= 2'd0;
        end

        pixel_x <= h;
        pixel_y <= v;
        de <= h < H_ACTIVE && v < V_ACTIVE;
        hsync <= h >= H_ACTIVE + H_FRONT && h < H_ACTIVE + H_FRONT + H_SYNC;
        vsync <= v >= V_ACTIVE + V_FRONT && v < V_ACTIVE + V_FRONT + V_SYNC;
        red <= pixel_rgb[23:16];
        green <= pixel_rgb[15:8];
        blue <= pixel_rgb[7:0];
    end
endmodule
