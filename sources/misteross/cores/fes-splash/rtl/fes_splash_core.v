// SPDX-License-Identifier: GPL-2.0-or-later
// CTA-770.3 1280x720p60 splash: FogCast/FES mark plus autonomous motion.
// Full-frame raster (not the 320x240 playfield). No mailbox, no user-io.

module fes_splash_core (
    input  wire        pixel_clk,
    output wire [23:0] hdmi_rgb,
    output wire        hdmi_de,
    output wire        hdmi_hs,
    output wire        hdmi_vs,
    output wire        frame_tick,
    output wire [7:0]  phase
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_FRONT  = 11'd110;
    localparam [10:0] H_SYNC   = 11'd40;
    localparam [10:0] H_TOTAL  = 11'd1650;
    localparam [9:0]  V_ACTIVE = 10'd720;
    localparam [9:0]  V_FRONT  = 10'd5;
    localparam [9:0]  V_SYNC   = 10'd5;
    localparam [9:0]  V_TOTAL  = 10'd750;

    localparam [10:0] CX = 11'd460;
    localparam [9:0]  CY = 10'd360;
    localparam [10:0] RING = 11'd110;
    localparam [10:0] LX = 11'd620;
    localparam [9:0]  LY = 10'd318;
    localparam [10:0] LETTER_STRIDE = 11'd72;
    localparam [10:0] LETTER_WIDTH = 11'd60;
    localparam [9:0]  LETTER_HEIGHT = 10'd84;
    localparam [10:0] LETTERS_WIDTH = 11'd204;
    localparam [10:0] ORBIT = 11'd132;

    reg [10:0] horizontal;
    reg [9:0]  vertical;
    reg [7:0]  phase_q;

    wire de = horizontal < H_ACTIVE && vertical < V_ACTIVE;
    assign hdmi_de = de;
    assign hdmi_hs = horizontal >= H_ACTIVE + H_FRONT &&
                     horizontal < H_ACTIVE + H_FRONT + H_SYNC;
    assign hdmi_vs = vertical >= V_ACTIVE + V_FRONT &&
                     vertical < V_ACTIVE + V_FRONT + V_SYNC;
    assign frame_tick = horizontal == H_TOTAL - 1'b1 &&
                        vertical == V_TOTAL - 1'b1;
    assign phase = phase_q;

    wire [10:0] ax = (horizontal >= CX) ? (horizontal - CX) : (CX - horizontal);
    wire [10:0] ay = (vertical >= CY) ? ({1'b0, vertical} - {1'b0, CY})
                                      : ({1'b0, CY} - {1'b0, vertical});
    wire [11:0] manh = {1'b0, ax} + {1'b0, ay};
    wire [11:0] pulse = {9'd0, phase_q[4:2]};
    wire [11:0] outer_r = {1'b0, RING} + pulse;
    wire [11:0] inner_r = {1'b0, RING} - 12'd14;
    wire [11:0] core_r  = {1'b0, RING} - 12'd46;
    wire ring = manh <= outer_r && manh > inner_r;
    wire disc = manh <= inner_r && manh > core_r;

    wire [10:0] mote_x =
        (phase_q[7:5] == 3'd0 || phase_q[7:5] == 3'd1 || phase_q[7:5] == 3'd7) ? (CX + ORBIT) :
        (phase_q[7:5] == 3'd3 || phase_q[7:5] == 3'd4 || phase_q[7:5] == 3'd5) ? (CX - ORBIT) :
        CX;
    wire [9:0] mote_y =
        (phase_q[7:5] == 3'd1 || phase_q[7:5] == 3'd2 || phase_q[7:5] == 3'd3) ? (CY - ORBIT[9:0]) :
        (phase_q[7:5] == 3'd5 || phase_q[7:5] == 3'd6 || phase_q[7:5] == 3'd7) ? (CY + ORBIT[9:0]) :
        CY;
    wire [10:0] mote_ax = (horizontal >= mote_x) ? (horizontal - mote_x) : (mote_x - horizontal);
    wire [10:0] mote_ay = (vertical >= mote_y) ? ({1'b0, vertical} - {1'b0, mote_y})
                                              : ({1'b0, mote_y} - {1'b0, vertical});
    wire mote = mote_ax <= 11'd3 && mote_ay <= 11'd3;

    wire in_letters = horizontal >= LX && horizontal < LX + LETTERS_WIDTH &&
                      vertical >= LY && vertical < LY + LETTER_HEIGHT;
    wire [10:0] lx = horizontal - LX;
    wire [9:0]  ly = vertical - LY;
    wire [1:0]  letter = (lx < LETTER_STRIDE) ? 2'd0 :
                         (lx < LETTER_STRIDE + LETTER_STRIDE) ? 2'd1 : 2'd2;
    wire [10:0] glyph_x = (letter == 2'd0) ? lx :
                          (letter == 2'd1) ? (lx - LETTER_STRIDE) :
                          (lx - LETTER_STRIDE - LETTER_STRIDE);
    wire in_cell = in_letters && glyph_x < LETTER_WIDTH;
    wire [2:0] col = (glyph_x < 11'd12) ? 3'd0 :
                     (glyph_x < 11'd24) ? 3'd1 :
                     (glyph_x < 11'd36) ? 3'd2 :
                     (glyph_x < 11'd48) ? 3'd3 : 3'd4;
    wire [2:0] row = (ly < 10'd12) ? 3'd0 :
                     (ly < 10'd24) ? 3'd1 :
                     (ly < 10'd36) ? 3'd2 :
                     (ly < 10'd48) ? 3'd3 :
                     (ly < 10'd60) ? 3'd4 :
                     (ly < 10'd72) ? 3'd5 : 3'd6;

    function automatic [4:0] glyph_row;
        input [1:0] which;
        input [2:0] gy;
        begin
            case ({which, gy})
                {2'd0, 3'd0}: glyph_row = 5'b11111;
                {2'd0, 3'd1}: glyph_row = 5'b10000;
                {2'd0, 3'd2}: glyph_row = 5'b11110;
                {2'd0, 3'd3}: glyph_row = 5'b10000;
                {2'd0, 3'd4}: glyph_row = 5'b10000;
                {2'd0, 3'd5}: glyph_row = 5'b10000;
                {2'd0, 3'd6}: glyph_row = 5'b10000;
                {2'd1, 3'd0}: glyph_row = 5'b11111;
                {2'd1, 3'd1}: glyph_row = 5'b10000;
                {2'd1, 3'd2}: glyph_row = 5'b11110;
                {2'd1, 3'd3}: glyph_row = 5'b10000;
                {2'd1, 3'd4}: glyph_row = 5'b10000;
                {2'd1, 3'd5}: glyph_row = 5'b10000;
                {2'd1, 3'd6}: glyph_row = 5'b11111;
                {2'd2, 3'd0}: glyph_row = 5'b01111;
                {2'd2, 3'd1}: glyph_row = 5'b10000;
                {2'd2, 3'd2}: glyph_row = 5'b10000;
                {2'd2, 3'd3}: glyph_row = 5'b01110;
                {2'd2, 3'd4}: glyph_row = 5'b00001;
                {2'd2, 3'd5}: glyph_row = 5'b00001;
                {2'd2, 3'd6}: glyph_row = 5'b11110;
                default:      glyph_row = 5'b00000;
            endcase
        end
    endfunction

    wire [4:0] glyph_bits = glyph_row(letter, row);
    wire letter_pix = in_cell && glyph_bits[4 - col];
    wire [10:0] underline_x = LX + {3'd0, phase_q};
    wire underline = vertical >= LY + LETTER_HEIGHT + 10'd6 &&
                     vertical < LY + LETTER_HEIGHT + 10'd10 &&
                     horizontal >= underline_x &&
                     horizontal < underline_x + 11'd48;

    wire [9:0] fog_y = vertical + {2'b0, phase_q};
    wire fog = fog_y[5:0] < 6'd8;

    wire [7:0] ring_g = 8'd186 + {5'd0, phase_q[4:2]};
    wire [7:0] red =
        mote ? 8'd255 :
        letter_pix ? 8'd236 :
        underline ? 8'd80 :
        ring ? 8'd64 :
        disc ? 8'd20 :
        fog ? 8'd16 : 8'd8;
    wire [7:0] green =
        mote ? 8'd255 :
        letter_pix ? 8'd244 :
        underline ? 8'd210 :
        ring ? ring_g :
        disc ? 8'd48 :
        fog ? 8'd28 : 8'd16;
    wire [7:0] blue =
        mote ? 8'd255 :
        letter_pix ? 8'd248 :
        underline ? 8'd220 :
        ring ? 8'd210 :
        disc ? 8'd72 :
        fog ? 8'd48 : 8'd32;

    assign hdmi_rgb = de ? {red, green, blue} : 24'h000000;

    initial begin
        horizontal = 11'd0;
        vertical = 10'd0;
        phase_q = 8'd0;
    end

    always @(posedge pixel_clk) begin
        if (horizontal == H_TOTAL - 1'b1) begin
            horizontal <= 11'd0;
            if (vertical == V_TOTAL - 1'b1) begin
                vertical <= 10'd0;
                phase_q <= phase_q + 8'd1;
            end else begin
                vertical <= vertical + 1'b1;
            end
        end else begin
            horizontal <= horizontal + 1'b1;
        end
    end
endmodule
