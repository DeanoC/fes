// SPDX-License-Identifier: GPL-2.0-or-later
// Text plus large, redundant capture markers in the 320x240 playfield.
// The two LPDDR banks are explicitly NOT TESTED by this package.
module fes_de25_ramtest_video (
    input wire pixel_clk,
    input wire [4:0] result_flags,
    output wire [27:0] video
);
    // Independent single-bit status crossings. Fault/stop take priority over
    // pass; no binary multi-bit transition may synthesize a passing result.
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] pass_sync = 0, fail_sync = 0, stop_sync = 0, run_sync = 0;
    always @(posedge pixel_clk) begin
        pass_sync <= {pass_sync[0], result_flags[2]};
        fail_sync <= {fail_sync[0], result_flags[3]};
        stop_sync <= {stop_sync[0], result_flags[4]};
        run_sync <= {run_sync[0], result_flags[1]};
    end
    wire [2:0] status = fail_sync[1] ? 3'd3 : stop_sync[1] ? 3'd4 :
                        pass_sync[1] ? 3'd2 : run_sync[1] ? 3'd1 : 3'd0;
    wire [9:0] x, y;
    wire active, de, hs, vs;
    reg [7:0] r, g, b;
    wire [7:0] red, green, blue;
    fes_video_720p raster (.pixel_clk(pixel_clk), .game_red(r), .game_green(g), .game_blue(b),
        .playfield_x(x), .playfield_y(y), .playfield_active(active), .red(red), .green(green), .blue(blue),
        .de(de), .hsync(hs), .vsync(vs), .frame_tick());
    assign video = {1'b1, vs, hs, de, red, green, blue};

    reg [191:0] line;
    wire [4:0] column = (x - 10'd16) >> 3;
    wire [2:0] row = y[2:0];
    wire [7:0] ch = column < 24 ? line[(23-column)*8 +: 8] : 8'd0;
    wire [7:0] glyph;
    ram_font font (.ch(ch), .row(row), .pixels(glyph));
    wire text_band = (y >= 24 && y < 32) || (y >= 72 && y < 80) ||
                     (y >= 112 && y < 120) || (y >= 152 && y < 160);
    wire ink = x >= 16 && x < 208 && text_band && glyph[7-x[2:0]];
    always @* begin
        line = "                        ";
        if (y >= 24 && y < 32) line = "DE25 RAM TEST           ";
        else if (y >= 72 && y < 80) begin
            case (status)
                3'd1: line = "SDRAM 128M RUNNING      ";
                3'd2: line = "SDRAM 128M PASS         ";
                3'd3: line = "SDRAM 128M FAIL         ";
                3'd4: line = "SDRAM 128M STOPPED      ";
                default: line = "SDRAM 128M WAIT         ";
            endcase
        end else if (y >= 112 && y < 120) line = "FPGA LPDDR NOT TESTED   ";
        else if (y >= 152 && y < 160) line = "HPS LPDDR NOT TESTED    ";
        r = 0; g = 0; b = 0;
        if (ink) begin r = 8'hff; g = 8'hff; b = 8'hff; end
        // Fixed two-colour identity marker, separate from status and text.
        if (x >= 256 && x < 296 && y >= 24 && y < 32) begin r = 8'hd0; g = 8'h20; b = 8'hd0; end
        if (x >= 256 && x < 296 && y >= 32 && y < 40) begin r = 8'h20; g = 8'hd0; b = 8'hd0; end
        if (x >= 256 && x < 296 && y >= 64 && y < 88) begin
            case (status)
                3'd1: begin r=8'he0; g=8'ha0; b=8'h00; end
                3'd2: begin r=8'h00; g=8'he0; b=8'h00; end
                3'd3: begin r=8'he0; g=8'h00; b=8'h00; end
                3'd4: begin r=8'he0; g=8'he0; b=8'he0; end
                default: begin r=8'h20; g=8'h20; b=8'he0; end
            endcase
        end
        if (x >= 256 && x < 296 && ((y >= 104 && y < 128) || (y >= 144 && y < 168))) begin
            r=8'h60; g=8'h60; b=8'h60;
        end
    end
endmodule
