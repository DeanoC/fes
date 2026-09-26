// SPDX-License-Identifier: GPL-2.0-or-later
// Apple II video scanned directly from RAM in the 74.25 MHz HDMI domain.
//
// There is no frame buffer: the renderer reads text, lo-res and hi-res pages
// through RAM port B while it draws a fixed CTA-770.3 1280x720p60 raster.
// The 280x192 Apple picture is scaled 4x horizontally and 3x vertically to
// 1120x576 and centred (80 pixels left/right, 72 lines top/bottom), so one
// 14 MHz Apple "half dot" is two HDMI pixels.
//
// Text uses the open character generator (apple2_font.hex) and is always
// white on black. Lo-res draws the sixteen colours directly. Hi-res follows
// the NTSC artifact rule: each half dot updates one bit of a four-bit window
// indexed by its phase, and the window is the lo-res colour index, so the
// hi-res violet/green/blue/orange/white fall out of the same palette. Soft
// switches are synchronised into this domain and sampled once per line, so a
// mid-line mode change takes effect on the next line (no raster effects).
module apple2_video (
    input  wire        pixel_clk,
    input  wire        text_mode,
    input  wire        mixed_mode,
    input  wire        page2,
    input  wire        hires_mode,
    output reg  [15:0] ram_addr,
    input  wire [7:0]  ram_data,
    output reg  [7:0]  red,
    output reg  [7:0]  green,
    output reg  [7:0]  blue,
    output reg         de,
    output reg         hsync,
    output reg         vsync,
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
    localparam [9:0] IMAGE_TOP = 10'd72;
    localparam [9:0] IMAGE_BOTTOM = 10'd648;
    localparam [10:0] FETCH_START = 11'd52;   // one 28-pixel column early
    localparam [10:0] IMAGE_LEFT = 11'd80;

    reg [10:0] h = 11'd0;
    reg [9:0] v = 10'd0;
    always @(posedge pixel_clk) begin
        if (h == H_TOTAL - 1'b1) begin
            h <= 11'd0;
            v <= (v == V_TOTAL - 1'b1) ? 10'd0 : v + 1'b1;
        end else begin
            h <= h + 1'b1;
        end
    end
    assign frame_tick = h == H_TOTAL - 1'b1 && v == V_TOTAL - 1'b1;

    (* async_reg = "true" *) reg [3:0] mode_meta = 4'b0001;
    (* async_reg = "true" *) reg [3:0] mode_sync = 4'b0001;
    always @(posedge pixel_clk) begin
        mode_meta <= {hires_mode, page2, mixed_mode, text_mode};
        mode_sync <= mode_meta;
    end

    // Apple line bookkeeping: ay is 0..191 inside the image, sub is 0..2.
    reg [7:0] ay = 8'd0;
    reg [1:0] sub = 2'd0;
    wire line_in_image = v >= IMAGE_TOP && v < IMAGE_BOTTOM;
    always @(posedge pixel_clk)
        if (h == H_TOTAL - 1'b1) begin
            if (v == IMAGE_TOP - 1'b1) begin
                ay <= 8'd0;
                sub <= 2'd0;
            end else if (line_in_image) begin
                if (sub == 2'd2) begin
                    sub <= 2'd0;
                    ay <= ay + 1'b1;
                end else begin
                    sub <= sub + 1'b1;
                end
            end
        end

    // Per-line mode and base address, captured at the start of each line.
    reg line_text = 1'b1;
    reg line_hires = 1'b0;
    reg [15:0] line_base = 16'h0400;
    reg [4:0] flash_count = 5'd0;
    always @(posedge pixel_clk) begin
        if (frame_tick)
            flash_count <= flash_count + 1'b1;
        if (h == 11'd0) begin
            line_text <= mode_sync[0] || (mode_sync[1] && ay >= 8'd160);
            line_hires <= mode_sync[3];
            if (mode_sync[3] && !(mode_sync[0] || (mode_sync[1] && ay >= 8'd160)))
                line_base <= (mode_sync[2] ? 16'h4000 : 16'h2000) +
                             {3'b000, ay[2:0], 10'b0} +
                             {6'b0, ay[5:3], 7'b0} +
                             (ay[7:6] == 2'd0 ? 16'd0 : ay[7:6] == 2'd1 ? 16'd40 : 16'd80);
            else
                line_base <= (mode_sync[2] ? 16'h0800 : 16'h0400) +
                             {6'b0, ay[5:3], 7'b0} +
                             (ay[7:6] == 2'd0 ? 16'd0 : ay[7:6] == 2'd1 ? 16'd40 : 16'd80);
        end
    end
    wire flash_on = flash_count[4];

    // Column sequencer: 41 columns of 28 pixels from FETCH_START. Column k
    // fetches Apple column k (k <= 39) and displays Apple column k-1.
    reg [5:0] col = 6'd63;
    reg [4:0] csub = 5'd0;
    wire run = col != 6'd63;
    always @(posedge pixel_clk) begin
        if (h == FETCH_START - 1'b1 && line_in_image) begin
            col <= 6'd0;
            csub <= 5'd0;
        end else if (run) begin
            if (csub == 5'd27) begin
                csub <= 5'd0;
                col <= (col == 6'd40) ? 6'd63 : col + 1'b1;
            end else begin
                csub <= csub + 1'b1;
            end
        end
    end

    // Character generator: 64 glyphs x 8 rows, bit 0 = leftmost dot.
    reg [7:0] font [0:511];
    initial $readmemh("cores/fes-apple2/rtl/apple2_font.hex", font);
    reg [8:0] font_addr = 9'd0;
    reg [7:0] font_q = 8'd0;
    always @(posedge pixel_clk)
        font_q <= font[font_addr];

    // Fetch pipeline for the next column.
    reg [7:0] next_byte = 8'd0;
    reg [7:0] next_glyph = 8'd0;
    always @(posedge pixel_clk) begin
        if (run && col <= 6'd39) begin
            if (csub == 5'd0)
                ram_addr <= line_base + {10'd0, col};
            if (csub == 5'd4) begin
                next_byte <= ram_data;
                font_addr <= {ram_data[5:0], ay[2:0]};
            end
            if (csub == 5'd8)
                next_glyph <= font_q;
        end
    end

    // Current display column.
    reg [7:0] cur_byte = 8'd0;
    reg [7:0] cur_glyph = 8'd0;
    reg prev_dot6 = 1'b0;
    reg [3:0] window = 4'd0;
    wire showing = run && col != 6'd0;
    wire [5:0] dcol = col - 1'b1;
    wire [3:0] hd = csub[4:1];          // half dot 0..13

    wire [2:0] dot7 = csub[4:2];
    wire [1:0] phase = {hd[1] ^ dcol[0], hd[0]};

    always @(posedge pixel_clk) begin
        if (h == FETCH_START - 1'b1) begin
            window <= 4'd0;
            prev_dot6 <= 1'b0;
        end
        if (run && csub == 5'd27 && col <= 6'd39) begin
            if (col != 6'd0)
                prev_dot6 <= cur_byte[6];
            cur_byte <= next_byte;
            cur_glyph <= next_glyph;
        end
        if (showing && !csub[0])
            window[phase] <= hires_dot;
    end

    // Hi-res dot for this half dot: a set bit 7 delays the byte by one half
    // dot, and its first half dot repeats the previous byte's last dot.
    reg hires_dot;
    always @* begin
        if (!cur_byte[7])
            hires_dot = cur_byte[hd[3:1]];
        else if (hd == 4'd0)
            hires_dot = prev_dot6;
        else
            hires_dot = cur_byte[(hd - 1'b1) >> 1];
    end

    reg [3:0] window_now;
    always @* begin
        window_now = window;
        window_now[phase] = hires_dot;
    end

    wire inverse = cur_byte[7:6] == 2'b00 || (cur_byte[7:6] == 2'b01 && flash_on);
    wire text_dot = cur_glyph[dot7] ^ inverse;
    wire [3:0] lores_color = ay[2] ? cur_byte[7:4] : cur_byte[3:0];

    function [23:0] palette;
        input [3:0] index;
        begin
            case (index)
                4'd0: palette = 24'h000000;   // black
                4'd1: palette = 24'he31e60;   // magenta
                4'd2: palette = 24'h604ebd;   // dark blue
                4'd3: palette = 24'hff44fd;   // purple (hi-res violet)
                4'd4: palette = 24'h00a360;   // dark green
                4'd5: palette = 24'h9c9c9c;   // grey 1
                4'd6: palette = 24'h14cffd;   // medium blue (hi-res blue)
                4'd7: palette = 24'hd0c3ff;   // light blue
                4'd8: palette = 24'h607203;   // brown
                4'd9: palette = 24'hff6a3c;   // orange (hi-res orange)
                4'd10: palette = 24'h9c9c9c;  // grey 2
                4'd11: palette = 24'hffa0d0;  // pink
                4'd12: palette = 24'h14f53c;  // light green (hi-res green)
                4'd13: palette = 24'hd0dd8d;  // yellow
                4'd14: palette = 24'h72ffd0;  // aquamarine
                default: palette = 24'hffffff; // white
            endcase
        end
    endfunction

    reg [23:0] pixel;
    always @* begin
        pixel = 24'h000000;
        if (showing) begin
            if (line_text)
                pixel = text_dot ? 24'hffffff : 24'h000000;
            else if (line_hires)
                pixel = palette(window_now);
            else
                pixel = palette(lores_color);
        end
    end

    always @(posedge pixel_clk) begin
        {red, green, blue} <= (h >= IMAGE_LEFT && h < IMAGE_LEFT + 11'd1120) ? pixel : 24'h000000;
        de <= h < H_ACTIVE && v < V_ACTIVE;
        hsync <= h >= H_ACTIVE + H_FRONT && h < H_ACTIVE + H_FRONT + H_SYNC;
        vsync <= v >= V_ACTIVE + V_FRONT && v < V_ACTIVE + V_FRONT + V_SYNC;
    end

    initial begin
        ram_addr = 16'h0400;
        red = 8'h00;
        green = 8'h00;
        blue = 8'h00;
        de = 1'b0;
        hsync = 1'b0;
        vsync = 1'b0;
    end
endmodule
