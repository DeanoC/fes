// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_video_part.vh"
// PC display capture -> existing RGB888 raster fabric. The coordinated resets
// must assert in both domains; neither domain may reset independently in flight.
// A pending bank is immutable until scanout adopts it at an output SOF.
module pcxt_video #(parameter bit MDA = 0) (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel,
    input wire hold,
    input wire pixel_ce, source_de, source_sof, source_eol,
    input wire [3:0] rgbi,
    output wire [31:0] video_request
);
    localparam integer WIDTH = MDA ? 720 : 640;
    localparam integer HEIGHT = MDA ? 350 : 200;
    localparam integer SHIFT = MDA ? 2 : 1;
    localparam integer WORDS = WIDTH * HEIGHT >> SHIFT;
    localparam integer BANK_BITS = $clog2(WORDS);
    localparam integer STRIDE = 1 << BANK_BITS;
    localparam integer ABITS = BANK_BITS + 2;
    reg [1:0] write_bank, published_bank, source_front;
    reg sent, waiting, capturing, bad, queued;
    reg received;
    (* async_reg = "true" *) reg ack_meta, ack_sync, sent_meta, sent_sync;
    reg [10:0] source_x;
    reg [9:0] source_y;
    reg [18:0] index;
    reg [5:0] packed_pixels;
    wire [10:0] sx = source_sof ? 11'b0 : source_x;
    wire [9:0] sy = source_sof ? 10'b0 : source_y;
    wire [18:0] pos = source_sof ? 19'b0 : index;
    wire expected_de = sx < 11'(WIDTH) && sy < 10'(HEIGHT);
    wire running = source_sof ? !queued : capturing;
    wire good = source_sof || !bad;
    wire accept = pixel_ce && !reset_sys && !hold && running && good &&
                  source_de == expected_de;
    wire store = accept && source_de;
    wire [ABITS-1:0] write_address = {write_bank, {BANK_BITS{1'b0}}} + ABITS'(pos >> SHIFT);
    wire [1:0] mono = rgbi == 0 ? 2'b00 : rgbi[3] ? 2'b11 : 2'b01;
    wire [7:0] packed_data = MDA ? {mono,packed_pixels} : {rgbi,packed_pixels[3:0]};
    wire packed_last = MDA ? pos[1:0] == 3 : pos[0];
    // One held toggle is the ownership mailbox. No live pixel data crosses here.
    always @(posedge clk_sys) begin
        if (reset_sys) begin
            write_bank <= 1; published_bank <= 0; source_front <= 0; queued <= 0;
            sent <= 0; waiting <= 0; capturing <= 0; bad <= 0;
            ack_meta <= 0; ack_sync <= 0; source_x <= 0; source_y <= 0;
            index <= 0; packed_pixels <= 0;
        end else begin
            ack_meta <= received; ack_sync <= ack_meta;
            if (waiting && ack_sync == sent) begin waiting <= 0; source_front <= published_bank; end
            // Only one mailbox is outstanding. A third bank may finish while
            // it waits; publish that queued bank once the old mailbox retires.
            if (!waiting && queued) begin
                published_bank <= write_bank; sent <= !sent; waiting <= 1; queued <= 0;
                write_bank <= 2'd3 - source_front - write_bank;
            end
            if (hold) capturing <= 0;
            if (pixel_ce) begin
                if (source_sof) begin
                    capturing <= !queued && !hold;
                    bad <= 0; index <= 0;
                end
                if (source_eol) begin source_x <= 0; source_y <= sy == 1023 ? sy : sy + 1'b1; end
                else begin source_x <= sx == 2047 ? sx : sx + 1'b1; source_y <= sy; end
                if (running && !hold) begin
                    if (source_de != expected_de || (source_eol && sx < 11'(WIDTH-1))) begin
                        bad <= 1; capturing <= 0;
                    end
                    if (store) begin
                        if (MDA) begin
                            case (pos[1:0])
                                0: packed_pixels[1:0] <= mono;
                                1: packed_pixels[3:2] <= mono;
                                2: packed_pixels[5:4] <= mono;
                                default: begin end
                            endcase
                        end else if (!pos[0]) packed_pixels[3:0] <= rgbi;
                        index <= pos + 1'b1;
                    end
                    // The last active line's blanking closes a complete active
                    // surface. Remaining guest blanking gives scanout time to
                    // adopt the bank before the next guest frame starts.
                    if (accept && source_eol && sy == 10'(HEIGHT-1) && pos == 19'(WIDTH*HEIGHT)) begin
                        capturing <= 0;
                        if (waiting) queued <= 1;
                        else begin
                            published_bank <= write_bank; sent <= !sent; waiting <= 1;
                            write_bank <= 2'd3 - source_front - write_bank;
                        end
                    end
                end
            end
        end
    end

    reg [10:0] horizontal;
    reg [9:0] vertical;
    reg [1:0] front;
    reg front_valid;
    (* async_reg = "true" *) reg hold_meta, hold_sync;
    wire output_sof = horizontal == 0 && vertical == 0;
    wire output_eol = horizontal == 11'(`FES_VIDEO_PART720P_HORIZONTAL_TOTAL-1);
    wire visible = horizontal < 1280 && vertical < 720;
    wire image = horizontal >= 160 && horizontal < 1120 && vertical < 720;
    // Rational nearest-neighbour scaling to a 960x720 (4:3) viewport. DDA
    // counters avoid variable division or a multiplier on the RAM address path.
    reg [10:0] x_error, y_error;
    reg [9:0] image_x;
    reg [18:0] row_base;
    wire [11:0] next_x_error = {1'b0,x_error} + 12'(WIDTH);
    wire [11:0] next_y_error = {1'b0,y_error} + 12'(HEIGHT);
    wire adopt = output_sof && sent_sync != received;
    wire [1:0] read_bank = adopt ? published_bank : front;
    wire valid_bank = front_valid || adopt;
    wire [18:0] image_index = row_base + {9'b0,image_x};
    wire [BANK_BITS-1:0] read_offset = image ? BANK_BITS'(image_index >> SHIFT) : {BANK_BITS{1'b0}};
    wire [ABITS-1:0] read_address = {read_bank, read_offset};
    reg [ABITS-1:0] read_address_q;
    // Keep capture geometry/decode out of the M10K write setup path. Retain
    // address, data and enable together. The final byte commits no later than
    // publication; the synchronized mailbox cannot expose an unwritten byte.
    reg [ABITS-1:0] write_address_q;
    reg [7:0] packed_data_q;
    reg write_enable_q;
    always @(posedge clk_sys) begin
        if (reset_sys) begin write_address_q <= 0; packed_data_q <= 0; write_enable_q <= 0; end
        else begin
            write_address_q <= write_address;
            packed_data_q <= packed_data;
            write_enable_q <= store && packed_last;
        end
    end
    // Keep each bank's RAM read mux local and register all three results
    // before selecting the displayed bank. One 192 KiB mux otherwise sits
    // between the M10K reads and the palette-stage register.
    wire [7:0] bank_pair [0:2];
    reg [7:0] bank_pair_r [0:2];
    genvar bank;
    generate for (bank = 0; bank < 3; bank = bank + 1) begin: framebuffer_banks
        /* verilator lint_off PINCONNECTEMPTY */
        coleco_video_dpram #(.DATAWIDTH(8), .ADDRWIDTH(BANK_BITS), .NUMWORDS(STRIDE)) framebuffer (
            .clock_a(clk_sys), .address_a(write_address_q[BANK_BITS-1:0]), .data_a(packed_data_q),
            .wren_a(write_enable_q && write_address_q[ABITS-1:BANK_BITS] == 2'(bank)), .q_a(),
            .clock_b(clk_pixel), .address_b(read_address_q[BANK_BITS-1:0]),
            .data_b(8'b0), .wren_b(1'b0), .q_b(bank_pair[bank])
        );
        /* verilator lint_on PINCONNECTEMPTY */
        always @(posedge clk_pixel) begin
            if (reset_pixel) bank_pair_r[bank] <= 0;
            else bank_pair_r[bank] <= bank_pair[bank];
        end
    end endgenerate
    reg image_q, de_q, hs_q, vs_q, sof_q, eol_q, hold_q;
    reg [1:0] subpixel_q;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            horizontal <= 0; vertical <= 0; front <= 0; front_valid <= 0; received <= 0;
            sent_meta <= 0; sent_sync <= 0; hold_meta <= 0; hold_sync <= 0;
            x_error <= 0; y_error <= 0; image_x <= 0; row_base <= 0;
            read_address_q <= 0;
            image_q <= 0; subpixel_q <= 0; de_q <= 0; hs_q <= 0; vs_q <= 0;
            sof_q <= 0; eol_q <= 0; hold_q <= 0;
        end else begin
            sent_meta <= sent; sent_sync <= sent_meta;
            hold_meta <= hold; hold_sync <= hold_meta;
            if (adopt) begin front <= published_bank; front_valid <= 1; received <= sent_sync; end
            if (output_eol) begin
                horizontal <= 0;
                vertical <= vertical == 749 ? 10'b0 : vertical + 1'b1;
                image_x <= 0; x_error <= 0;
                if (vertical == 749) begin row_base <= 0; y_error <= 0; end
                else if (vertical < 720) begin
                    if (next_y_error >= 720) begin
                        y_error <= 11'(next_y_error - 720); row_base <= row_base + 19'(WIDTH);
                    end else y_error <= next_y_error[10:0];
                end
            end else begin
                horizontal <= horizontal + 1'b1;
                if (image) begin
                    if (next_x_error >= 960) begin x_error <= 11'(next_x_error-960); image_x <= image_x+1'b1; end
                    else x_error <= next_x_error[10:0];
                end
            end
            read_address_q <= read_address;
            image_q <= image && valid_bank;
            subpixel_q <= image_index[1:0];
            de_q <= visible; hs_q <= horizontal >= 1390 && horizontal < 1430;
            vs_q <= vertical >= 725 && vertical < 730;
            sof_q <= output_sof; eol_q <= output_eol; hold_q <= hold_sync;
        end
    end
    // Register the RAM address before its distributed M10K input fanout. This
    // stage carries the matching raster/subpixel fields through the RAM read.
    reg [1:0] subpixel_read, bank_read;
    reg image_read, de_read, hs_read, vs_read, sof_read, eol_read, hold_read;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            subpixel_read <= 0; bank_read <= 0; image_read <= 0; de_read <= 0; hs_read <= 0;
            vs_read <= 0; sof_read <= 0; eol_read <= 0; hold_read <= 0;
        end else begin
            subpixel_read <= subpixel_q; bank_read <= read_address_q[ABITS-1:BANK_BITS]; image_read <= image_q;
            de_read <= de_q; hs_read <= hs_q; vs_read <= vs_q;
            sof_read <= sof_q; eol_read <= eol_q; hold_read <= hold_q;
        end
    end
    // Align bank selection and raster fields with the three registered bank
    // reads, then select the packed pixel/palette before the shared effect.
    reg [1:0] bank_r;
    wire [7:0] pair_r = bank_r == 0 ? bank_pair_r[0] :
                        bank_r == 1 ? bank_pair_r[1] :
                        bank_r == 2 ? bank_pair_r[2] : 8'b0;
    reg [1:0] subpixel_r;
    reg image_r, de_r, hs_r, vs_r, sof_r, eol_r, hold_r;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            bank_r <= 0; subpixel_r <= 0; image_r <= 0; de_r <= 0;
            hs_r <= 0; vs_r <= 0; sof_r <= 0; eol_r <= 0; hold_r <= 0;
        end else begin
            bank_r <= bank_read; subpixel_r <= subpixel_read; image_r <= image_read;
            de_r <= de_read; hs_r <= hs_read; vs_r <= vs_read; sof_r <= sof_read; eol_r <= eol_read; hold_r <= hold_read;
        end
    end
    wire [1:0] mono_q = 2'(pair_r >> {subpixel_r,1'b0});
    wire [3:0] colour = MDA ? (mono_q == 0 ? 4'd0 : mono_q[1] ? 4'd15 : 4'd7) :
                             subpixel_r[0] ? pair_r[7:4] : pair_r[3:0];
    function automatic [23:0] palette(input [3:0] c);
        reg [7:0] r,g,b;
        begin
            r = (c[2] ? 8'haa : 8'b0) + (c[3] ? 8'h55 : 8'b0);
            g = (c[1] ? 8'haa : 8'b0) + (c[3] ? 8'h55 : 8'b0);
            b = (c[0] ? 8'haa : 8'b0) + (c[3] ? 8'h55 : 8'b0);
            if (c == 6) g = 8'h55; // IBM RGBI brown correction.
            palette = {r,g,b};
        end
    endfunction
    wire [23:0] rgb = image_r && !hold_r ? palette(colour) : 24'b0;
    assign video_request = {1'b0,hold_r,eol_r,sof_r,1'b1,vs_r,hs_r,de_r,rgb};
endmodule
