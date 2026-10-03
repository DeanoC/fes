// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_native_video.vh"
// Native indexed4/TMS9918 Direct and Scanlines output. All RAM and scanout
// operate on the socket's single pixel clock. Two pixels per RAM word keep
// two complete 256x192 banks at 48 M10Ks in diagnostic locked synthesis.
module fes_native_video #(
    parameter SCANLINES = 0,
    parameter WIDTH = `FES_NATIVE_VIDEO_INITIAL_WIDTH,
    parameter HEIGHT = `FES_NATIVE_VIDEO_INITIAL_HEIGHT,
    parameter ENCODING = `FES_NATIVE_VIDEO_ENCODING_INDEX4_TMS9918
) (
    input wire clock,
    input wire [31:0] request,
    output wire [27:0] response
);
    // The first profile binds geometry and palette at elaboration. Encoding
    // is never guessed from the producer's name or changed by a pixel token.
    initial begin
        if (WIDTH != 256 || HEIGHT != 192 || ENCODING != `FES_NATIVE_VIDEO_ENCODING_INDEX4_TMS9918)
            $error("Unsupported native geometry/encoding");
    end
    localparam PIXELS = WIDTH * HEIGHT;
    localparam WORDS = PIXELS / 2;
    localparam X_ORIGIN = (1280 - WIDTH * 2) / 2;
    localparam Y_ORIGIN = (720 - HEIGHT * 2) / 2;
    reg [10:0] h_count = 0;
    reg [9:0] v_count = 0;
    reg front = 0, front_valid = 0, pending = 0;
    reg capturing = 0, held = 1;
    reg [15:0] capture_index = 0;
    reg [3:0] first_pixel = 0;
    wire valid = request[`FES_NATIVE_VIDEO_REQUEST_VALID_BIT];
    wire sof = request[`FES_NATIVE_VIDEO_REQUEST_SOF_BIT];
    wire eol = request[`FES_NATIVE_VIDEO_REQUEST_EOL_BIT];
    wire eof = request[`FES_NATIVE_VIDEO_REQUEST_EOF_BIT];
    wire control = request[`FES_NATIVE_VIDEO_REQUEST_CONTROL_BIT];
    wire reserved = |request[31:30];
    wire control_ok = valid && control && !reserved && request[27:0] == 28'h1000000;
    wire start = sof && !pending && !held;
    wire [15:0] index_now = sof ? 16'b0 : capture_index;
    wire markers_ok = !reserved && request[23:4] == 0 && !request[28] &&
                      sof == (index_now == 0) &&
                      eol == (32'(index_now) % WIDTH == WIDTH - 1) &&
                      eof == (32'(index_now) == PIXELS - 1);
    wire accept = valid && !control && !held && !pending &&
                  (capturing || start) && markers_ok;
    wire write_enable = accept && index_now[0];
    wire [15:0] write_address = (!front ? 16'(WORDS) : 16'b0) + (index_now >> 1);

    wire image = 32'(h_count) >= X_ORIGIN && 32'(h_count) < X_ORIGIN + WIDTH * 2 &&
                 32'(v_count) >= Y_ORIGIN && 32'(v_count) < Y_ORIGIN + HEIGHT * 2;
    wire [15:0] image_index = 16'(((32'(v_count) - Y_ORIGIN) >> 1) * WIDTH +
                                 ((32'(h_count) - X_ORIGIN) >> 1));
    wire [15:0] read_address = (front ? 16'(WORDS) : 16'b0) +
                              (image ? image_index >> 1 : 16'b0);
    wire [7:0] read_pair;
    /* verilator lint_off PINCONNECTEMPTY */
    coleco_video_dpram #(.DATAWIDTH(8), .ADDRWIDTH(16), .NUMWORDS(WORDS * 2)) framebuffer (
        .clock_a(clock), .address_a(write_address),
        .data_a({request[3:0], first_pixel}), .wren_a(write_enable), .q_a(),
        .clock_b(clock), .address_b(read_address),
        .data_b(8'b0), .wren_b(1'b0), .q_b(read_pair)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    reg image_q = 0, nibble_q = 0, dim_q = 0;
    reg de_q = 0, hs_q = 0, vs_q = 0;
    always @(posedge clock) begin
        if (h_count == 1649) begin
            h_count <= 0;
            v_count <= v_count == 749 ? 10'd0 : v_count + 1'b1;
        end else h_count <= h_count + 1'b1;
        de_q <= h_count < 1280 && v_count < 720;
        hs_q <= h_count >= 1390 && h_count < 1430;
        vs_q <= v_count >= 725 && v_count < 730;
        image_q <= image && front_valid;
        nibble_q <= image_index[0];
        dim_q <= SCANLINES != 0 && v_count[0];
        // A completed bank is displayed only at an output frame boundary.
        // If source SOF coincides with this swap, discard that whole source
        // frame: it arrived before the bank was free at the sampling edge.
        if (h_count == 0 && v_count == 0 && pending && !(control_ok && request[28])) begin
            front <= !front;
            front_valid <= 1;
            pending <= 0;
        end
        if (valid) begin
            if (control) begin
                if (control_ok) begin
                    held <= request[28];
                    capturing <= 0;
                    if (request[28]) pending <= 0;
                end else capturing <= 0;
            end else if (!accept) begin
                capturing <= 0;
            end else begin
                if (!index_now[0]) first_pixel <= request[3:0];
                capture_index <= index_now + 1'b1;
                capturing <= !eof;
                if (eof) pending <= 1;
            end
        end
    end
    function [23:0] palette;
        input [3:0] color;
        begin
            case (color)
                2: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR2);
                3: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR3);
                4: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR4);
                5: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR5);
                6: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR6);
                7: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR7);
                8: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR8);
                9: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR9);
                10: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR10);
                11: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR11);
                12: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR12);
                13: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR13);
                14: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR14);
                15: palette = 24'(`FES_NATIVE_VIDEO_TMS9918_COLOR15);
                default: palette = 0;
            endcase
        end
    endfunction
    wire [23:0] rgb = image_q && !held ? palette(nibble_q ? read_pair[7:4] : read_pair[3:0]) : 24'b0;
    wire [23:0] filtered = dim_q ? {1'b0, rgb[23:17], 1'b0, rgb[15:9], 1'b0, rgb[7:1]} : rgb;
    assign response = {1'b1, vs_q, hs_q, de_q, filtered};
endmodule
