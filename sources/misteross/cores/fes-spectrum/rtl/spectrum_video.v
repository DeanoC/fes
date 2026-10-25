// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum picture: complete 50 Hz frames captured from the ULA and shown
// 3x3 in 1280x720p60.
//
// The ULA writes one 8-pixel chunk per clock, as {bitmap, ink, paper} with
// 4-bit colours, into one of three frame banks (320x240, 40 words per line).
// HDMI shows the newest complete bank from its next frame start, so 50 Hz
// frames repeat whole and never tear.
// The 960x720 picture is centred at x=160; the side bars are black for the
// display plane's surround.
module spectrum_video (
    input  wire        clk_sys,
    input  wire        pixel_we,
    input  wire [7:0]  pixel_row,
    input  wire [5:0]  pixel_chunk,
    input  wire [15:0] pixel_data,
    input  wire        frame_done,
    input  wire        pixel_clk,
    output reg  [7:0]  red = 8'd0,
    output reg  [7:0]  green = 8'd0,
    output reg  [7:0]  blue = 8'd0,
    output reg         de = 1'b0,
    output reg         hsync = 1'b0,
    output reg         vsync = 1'b0
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_FRONT = 11'd110;
    localparam [10:0] H_SYNC = 11'd40;
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_ACTIVE = 10'd720;
    localparam [9:0] V_FRONT = 10'd5;
    localparam [9:0] V_SYNC = 10'd5;
    localparam [9:0] V_TOTAL = 10'd750;
    localparam [10:0] LEFT = 11'd160;

    (* ramstyle = "M10K" *) reg [15:0] frames [0:28799];

    function [14:0] word_address(input [1:0] bank, input [7:0] row, input [5:0] chunk);
        reg [14:0] base;
        begin
            base = bank == 2'd0 ? 15'd0 : bank == 2'd1 ? 15'd9600 : 15'd19200;
            word_address = base + {2'd0, row, 5'd0} + {4'd0, row, 3'd0} + {9'd0, chunk};
        end
    endfunction
    function [1:0] other_bank(input [1:0] a, input [1:0] b);
        other_bank = (a != 2'd0 && b != 2'd0) ? 2'd0 : (a != 2'd1 && b != 2'd1) ? 2'd1 : 2'd2;
    endfunction

    // ------------------------------------------------------- writer (system)
    // A frame's first word picks its bank: neither the newest complete bank
    // nor the one HDMI shows. The ULA's 72 blank lines between the last word
    // of one frame and the first of the next let any HDMI frame start that
    // took the previous newest bank cross back long before that choice.
    reg [1:0] write_bank = 2'd0;
    reg [1:0] ready_bank = 2'd1;
    reg ready_toggle = 1'b0;
    reg [1:0] shown_bank = 2'd2;
    (* async_reg = "true" *) reg [1:0] shown_meta = 2'd2;
    (* async_reg = "true" *) reg [1:0] shown_sync = 2'd2;
    wire frame_start = pixel_row == 8'd0 && pixel_chunk == 6'd0;
    wire [1:0] target_bank = frame_start ? other_bank(ready_bank, shown_sync) : write_bank;
    wire [14:0] write_address = word_address(target_bank, pixel_row, pixel_chunk);
    always @(posedge clk_sys) begin
        shown_meta <= shown_bank;
        shown_sync <= shown_meta;
        if (pixel_we) begin
            frames[write_address] <= pixel_data;
            write_bank <= target_bank;
        end
        if (frame_done) begin
            ready_bank <= target_bank;
            ready_toggle <= ~ready_toggle;
        end
    end

    // ---------------------------------------------------------- reader (HDMI)
    (* async_reg = "true" *) reg [2:0] ready_meta = 3'b001;
    (* async_reg = "true" *) reg [2:0] ready_sync = 3'b001;
    reg ready_seen = 1'b0;
    reg [10:0] h = 11'd0;
    reg [9:0] v = 10'd0;
    reg [1:0] sub_x = 2'd0, sub_y = 2'd0;
    reg [2:0] pixel = 3'd0;
    reg [7:0] sy = 8'd0;
    reg [7:0] bits = 8'd0;
    reg [3:0] ink = 4'd0, paper = 4'd0;
    reg [15:0] next_word = 16'd0;
    reg [14:0] read_address = 15'd0;
    reg [15:0] read_data = 16'd0;
    reg [15:0] read_q = 16'd0;
    reg fetching = 1'b0;
    reg [4:0] fetch_phase = 5'd0;
    reg [5:0] fetch_word = 6'd0;
    // The frame store spans many M10K blocks; read_q registers the word after
    // their output mux, so the mux does not share a clock with next_word.
    always @(posedge pixel_clk) begin
        read_data <= frames[read_address];
        read_q <= read_data;
    end

    function [23:0] palette(input [3:0] colour);
        reg [7:0] level;
        begin
            level = colour[3] ? 8'hff : 8'hcd;
            palette = {colour[1] ? level : 8'h00, colour[2] ? level : 8'h00,
                       colour[0] ? level : 8'h00};
        end
    endfunction

    wire active_line = v < V_ACTIVE;
    wire in_picture = active_line && h >= LEFT && h < LEFT + 11'd960;
    reg [23:0] rgb;
    always @(posedge pixel_clk) begin
        ready_meta <= {ready_toggle, ready_bank};
        ready_sync <= ready_meta;
        if (h == H_TOTAL - 1'b1) begin
            h <= 11'd0;
            if (v == V_TOTAL - 1'b1) begin
                v <= 10'd0;
                sy <= 8'd0;
                sub_y <= 2'd0;
                if (ready_sync[2] != ready_seen) begin
                    ready_seen <= ready_sync[2];
                    shown_bank <= ready_sync[1:0];
                end
            end else begin
                v <= v + 10'd1;
                if (active_line) begin
                    if (sub_y == 2'd2) begin
                        sub_y <= 2'd0;
                        sy <= sy + 8'd1;
                    end else sub_y <= sub_y + 2'd1;
                end
            end
        end else begin
            h <= h + 11'd1;
        end

        // Word w is shown from h = LEFT + 24w. Its address is issued 8 clocks
        // earlier and the data captured 5 clocks earlier.
        if (h == LEFT - 11'd9) begin
            fetching <= active_line;
            fetch_phase <= 5'd0;
            fetch_word <= 6'd0;
        end else if (fetching) begin
            if (fetch_phase == 5'd0)
                read_address <= word_address(shown_bank, sy, fetch_word);
            if (fetch_phase == 5'd3)
                next_word <= read_q;
            if (fetch_phase == 5'd23) begin
                fetch_phase <= 5'd0;
                if (fetch_word == 6'd39) fetching <= 1'b0;
                else fetch_word <= fetch_word + 6'd1;
            end else begin
                fetch_phase <= fetch_phase + 5'd1;
            end
        end

        // Three output pixels per source pixel, eight pixels per chunk.
        if (h == LEFT - 11'd1) begin
            sub_x <= 2'd0;
            pixel <= 3'd0;
            {bits, ink, paper} <= next_word;
        end else if (in_picture) begin
            if (sub_x == 2'd2) begin
                sub_x <= 2'd0;
                pixel <= pixel + 3'd1;
                if (pixel == 3'd7) {bits, ink, paper} <= next_word;
                else bits <= {bits[6:0], 1'b0};
            end else sub_x <= sub_x + 2'd1;
        end

        rgb = in_picture ? palette(bits[7] ? ink : paper) : 24'h000000;
        red <= rgb[23:16];
        green <= rgb[15:8];
        blue <= rgb[7:0];
        de <= h < H_ACTIVE && active_line;
        hsync <= h >= H_ACTIVE + H_FRONT && h < H_ACTIVE + H_FRONT + H_SYNC;
        vsync <= v >= V_ACTIVE + V_FRONT && v < V_ACTIVE + V_FRONT + V_SYNC;
    end
endmodule
