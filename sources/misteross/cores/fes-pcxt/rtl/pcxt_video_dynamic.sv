// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_video_part.vh"
// Runtime-selectable CGA (640x200@4bpp), MDA (720x350@2bpp), and Hercules
// (720x348@2bpp) capture -> 720p60 RGB888 raster fabric.
// Uses the same 3x64 KiB framebuffer banks and held-payload CDC mailbox as pcxt_video.
// source_mode: 2'd0 = CGA 640x200, 2'd1 = MDA 720x350, 2'd2 = HGC 720x348.
// mono_tint:   2'd0 = White, 2'd1 = Green (P39), 2'd2 = Amber (P3).
module pcxt_video_dynamic (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel,
    input wire hold,
    input wire pixel_ce, source_de, source_sof, source_eol,
    input wire [3:0] rgbi,
    input wire [1:0] source_mode,
    input wire [1:0] mono_tint,
    output wire [31:0] video_request
);
    localparam integer BANK_BITS = 16;
    localparam integer STRIDE = 1 << BANK_BITS;
    localparam integer ABITS = BANK_BITS + 2;

    reg [1:0] write_bank, published_bank, source_front;
    reg [1:0] cap_mode, cap_tint;
    reg [1:0] published_mode, published_tint, queued_mode, queued_tint;
    reg sent, waiting, capturing, bad, queued;
    reg received;
    (* async_reg = "true" *) reg ack_meta, ack_sync, sent_meta, sent_sync;
    reg [10:0] source_x;
    reg [9:0] source_y;
    reg [18:0] index;
    reg [5:0] packed_pixels;

    wire [1:0] safe_source_mode = (source_mode <= 2'd2) ? source_mode : 2'd0;
    wire [1:0] cur_mode = source_sof ? safe_source_mode : cap_mode;
    wire [1:0] cur_tint = source_sof ? mono_tint : cap_tint;
    wire cur_mono = cur_mode != 2'd0;
    wire [10:0] cur_width = cur_mono ? 11'd720 : 11'd640;
    wire [9:0] cur_height = (cur_mode == 2'd2) ? 10'd348 : cur_mono ? 10'd350 : 10'd200;
    wire [18:0] cur_pixels = (cur_mode == 2'd2) ? 19'd250560 : cur_mono ? 19'd252000 : 19'd128000;

    wire [10:0] sx = source_sof ? 11'b0 : source_x;
    wire [9:0] sy = source_sof ? 10'b0 : source_y;
    wire [18:0] pos = source_sof ? 19'b0 : index;
    wire expected_de = sx < cur_width && sy < cur_height;
    wire running = source_sof ? !queued : capturing;
    wire good = source_sof || !bad;
    wire accept = pixel_ce && !reset_sys && !hold && running && good &&
                  source_de == expected_de;
    wire store = accept && source_de;
    wire [BANK_BITS-1:0] write_offset = cur_mono ? pos[17:2] : pos[16:1];
    wire [ABITS-1:0] write_address = {write_bank, write_offset};
    wire [1:0] mono = rgbi == 4'd0 ? 2'b00 : rgbi[3] ? 2'b11 : 2'b01;
    wire [7:0] packed_data = cur_mono ? {mono, packed_pixels} : {rgbi, packed_pixels[3:0]};
    wire packed_last = cur_mono ? (pos[1:0] == 2'd3) : pos[0];

    always @(posedge clk_sys) begin
        if (reset_sys) begin
            write_bank <= 2'd1; published_bank <= 2'd0; source_front <= 2'd0;
            cap_mode <= 2'd0; cap_tint <= 2'd0;
            published_mode <= 2'd0; published_tint <= 2'd0;
            queued_mode <= 2'd0; queued_tint <= 2'd0;
            queued <= 1'b0; sent <= 1'b0; waiting <= 1'b0; capturing <= 1'b0; bad <= 1'b0;
            ack_meta <= 1'b0; ack_sync <= 1'b0;
            source_x <= 11'd0; source_y <= 10'd0;
            index <= 19'd0; packed_pixels <= 6'd0;
        end else begin
            ack_meta <= received; ack_sync <= ack_meta;
            if (waiting && ack_sync == sent) begin
                waiting <= 1'b0;
                source_front <= published_bank;
            end
            if (!waiting && queued) begin
                published_bank <= write_bank;
                published_mode <= queued_mode;
                published_tint <= queued_tint;
                sent <= !sent;
                waiting <= 1'b1;
                queued <= 1'b0;
                write_bank <= 2'd3 - source_front - write_bank;
            end
            if (hold) capturing <= 1'b0;
            if (pixel_ce) begin
                if (source_sof) begin
                    capturing <= !queued && !hold;
                    bad <= 1'b0;
                    index <= 19'd0;
                    if (!queued && !hold) begin
                        cap_mode <= safe_source_mode;
                        cap_tint <= mono_tint;
                    end
                end
                if (source_eol) begin
                    source_x <= 11'd0;
                    source_y <= sy == 10'd1023 ? sy : sy + 10'd1;
                end else begin
                    source_x <= sx == 11'd2047 ? sx : sx + 11'd1;
                    source_y <= sy;
                end
                if (running && !hold) begin
                    if (source_de != expected_de || (source_eol && sx < cur_width - 11'd1)) begin
                        bad <= 1'b1;
                        capturing <= 1'b0;
                    end
                    if (store) begin
                        if (cur_mono) begin
                            case (pos[1:0])
                                2'd0: packed_pixels[1:0] <= mono;
                                2'd1: packed_pixels[3:2] <= mono;
                                2'd2: packed_pixels[5:4] <= mono;
                                default: begin end
                            endcase
                        end else if (!pos[0]) begin
                            packed_pixels[3:0] <= rgbi;
                        end
                        index <= pos + 19'd1;
                    end
                    if (accept && source_eol && sy == cur_height - 10'd1 && pos == cur_pixels) begin
                        capturing <= 1'b0;
                        if (waiting) begin
                            queued <= 1'b1;
                            queued_mode <= cur_mode;
                            queued_tint <= cur_tint;
                        end else begin
                            published_bank <= write_bank;
                            published_mode <= cur_mode;
                            published_tint <= cur_tint;
                            sent <= !sent;
                            waiting <= 1'b1;
                            write_bank <= 2'd3 - source_front - write_bank;
                        end
                    end
                end
            end
        end
    end

    reg [10:0] horizontal;
    reg [9:0] vertical;
    reg [1:0] front, front_mode, front_tint;
    reg front_valid;
    (* async_reg = "true" *) reg hold_meta, hold_sync;
    wire output_sof = horizontal == 11'd0 && vertical == 10'd0;
    wire output_eol = horizontal == 11'(`FES_VIDEO_PART720P_HORIZONTAL_TOTAL - 1);
    wire visible = horizontal < 11'd1280 && vertical < 10'd720;
    wire image = horizontal >= 11'd160 && horizontal < 11'd1120 && vertical < 10'd720;

    wire adopt = output_sof && sent_sync != received;
    wire [1:0] read_bank = adopt ? published_bank : front;
    wire active_mono = front_mode != 2'd0;
    wire [10:0] active_width = active_mono ? 11'd720 : 11'd640;
    wire [9:0] active_height = (front_mode == 2'd2) ? 10'd348 : active_mono ? 10'd350 : 10'd200;
    wire valid_bank = front_valid || adopt;

    reg [10:0] x_error, y_error;
    reg [9:0] image_x;
    reg [18:0] row_base;
    wire [11:0] next_x_error = {1'b0, x_error} + {1'b0, active_width};
    wire [11:0] next_y_error = {1'b0, y_error} + {2'b0, active_height};
    wire [18:0] image_index = row_base + {9'b0, image_x};
    wire [BANK_BITS-1:0] read_offset = image ? (active_mono ? image_index[17:2] : image_index[16:1]) : {BANK_BITS{1'b0}};
    wire [ABITS-1:0] read_address = {read_bank, read_offset};
    reg [ABITS-1:0] read_address_q;

    reg [ABITS-1:0] write_address_q;
    reg [7:0] packed_data_q;
    reg write_enable_q;
    always @(posedge clk_sys) begin
        if (reset_sys) begin
            write_address_q <= {ABITS{1'b0}};
            packed_data_q <= 8'd0;
            write_enable_q <= 1'b0;
        end else begin
            write_address_q <= write_address;
            packed_data_q <= packed_data;
            write_enable_q <= store && packed_last;
        end
    end

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
            if (reset_pixel) bank_pair_r[bank] <= 8'd0;
            else bank_pair_r[bank] <= bank_pair[bank];
        end
    end endgenerate

    reg image_q, mono_q, de_q, hs_q, vs_q, sof_q, eol_q, hold_q;
    reg [1:0] subpixel_q, tint_q;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            horizontal <= 11'd0; vertical <= 10'd0;
            front <= 2'd0; front_mode <= 2'd0; front_tint <= 2'd0;
            front_valid <= 1'b0; received <= 1'b0;
            sent_meta <= 1'b0; sent_sync <= 1'b0; hold_meta <= 1'b0; hold_sync <= 1'b0;
            x_error <= 11'd0; y_error <= 11'd0; image_x <= 10'd0; row_base <= 19'd0;
            read_address_q <= {ABITS{1'b0}};
            image_q <= 1'b0; mono_q <= 1'b0; tint_q <= 2'd0; subpixel_q <= 2'd0;
            de_q <= 1'b0; hs_q <= 1'b0; vs_q <= 1'b0; sof_q <= 1'b0; eol_q <= 1'b0; hold_q <= 1'b0;
        end else begin
            sent_meta <= sent; sent_sync <= sent_meta;
            hold_meta <= hold; hold_sync <= hold_meta;
            if (adopt) begin
                front <= published_bank;
                front_mode <= published_mode;
                front_tint <= published_tint;
                front_valid <= 1'b1;
                received <= sent_sync;
            end
            if (output_eol) begin
                horizontal <= 11'd0;
                vertical <= vertical == 10'd749 ? 10'd0 : vertical + 10'd1;
                image_x <= 10'd0; x_error <= 11'd0;
                if (vertical == 10'd749) begin
                    row_base <= 19'd0; y_error <= 11'd0;
                end else if (vertical < 10'd720) begin
                    if (next_y_error >= 12'd720) begin
                        y_error <= 11'(next_y_error - 12'd720);
                        row_base <= row_base + {8'b0, active_width};
                    end else begin
                        y_error <= next_y_error[10:0];
                    end
                end
            end else begin
                horizontal <= horizontal + 11'd1;
                if (image) begin
                    if (next_x_error >= 12'd960) begin
                        x_error <= 11'(next_x_error - 12'd960);
                        image_x <= image_x + 10'd1;
                    end else begin
                        x_error <= next_x_error[10:0];
                    end
                end
            end
            read_address_q <= read_address;
            image_q <= image && valid_bank;
            mono_q <= active_mono;
            tint_q <= front_tint;
            subpixel_q <= image_index[1:0];
            de_q <= visible;
            hs_q <= horizontal >= 11'd1390 && horizontal < 11'd1430;
            vs_q <= vertical >= 10'd725 && vertical < 10'd730;
            sof_q <= output_sof;
            eol_q <= output_eol;
            hold_q <= hold_sync;
        end
    end

    reg [1:0] subpixel_read, bank_read, tint_read;
    reg image_read, mono_read, de_read, hs_read, vs_read, sof_read, eol_read, hold_read;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            subpixel_read <= 2'd0; bank_read <= 2'd0; tint_read <= 2'd0;
            image_read <= 1'b0; mono_read <= 1'b0; de_read <= 1'b0;
            hs_read <= 1'b0; vs_read <= 1'b0; sof_read <= 1'b0; eol_read <= 1'b0; hold_read <= 1'b0;
        end else begin
            subpixel_read <= subpixel_q;
            bank_read <= read_address_q[ABITS-1:BANK_BITS];
            tint_read <= tint_q;
            image_read <= image_q;
            mono_read <= mono_q;
            de_read <= de_q; hs_read <= hs_q; vs_read <= vs_q;
            sof_read <= sof_q; eol_read <= eol_q; hold_read <= hold_q;
        end
    end

    reg [1:0] bank_r, subpixel_r, tint_r;
    wire [7:0] pair_r = bank_r == 2'd0 ? bank_pair_r[0] :
                        bank_r == 2'd1 ? bank_pair_r[1] :
                        bank_r == 2'd2 ? bank_pair_r[2] : 8'b0;
    reg image_r, mono_r, de_r, hs_r, vs_r, sof_r, eol_r, hold_r;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            bank_r <= 2'd0; subpixel_r <= 2'd0; tint_r <= 2'd0;
            image_r <= 1'b0; mono_r <= 1'b0; de_r <= 1'b0;
            hs_r <= 1'b0; vs_r <= 1'b0; sof_r <= 1'b0; eol_r <= 1'b0; hold_r <= 1'b0;
        end else begin
            bank_r <= bank_read; subpixel_r <= subpixel_read; tint_r <= tint_read;
            image_r <= image_read; mono_r <= mono_read;
            de_r <= de_read; hs_r <= hs_read; vs_r <= vs_read;
            sof_r <= sof_read; eol_r <= eol_read; hold_r <= hold_read;
        end
    end

    wire [1:0] mono_pixel = 2'(pair_r >> {subpixel_r, 1'b0});
    wire [3:0] cga_colour = subpixel_r[0] ? pair_r[7:4] : pair_r[3:0];

    function automatic [23:0] palette(input [3:0] c);
        reg [7:0] r, g, b;
        begin
            r = (c[2] ? 8'haa : 8'b0) + (c[3] ? 8'h55 : 8'b0);
            g = (c[1] ? 8'haa : 8'b0) + (c[3] ? 8'h55 : 8'b0);
            b = (c[0] ? 8'haa : 8'b0) + (c[3] ? 8'h55 : 8'b0);
            if (c == 4'd6) g = 8'h55; // IBM RGBI brown correction.
            palette = {r, g, b};
        end
    endfunction

    function automatic [23:0] mono_palette(input [1:0] level, input [1:0] tint);
        begin
            if (level == 2'b00) mono_palette = 24'h000000;
            else if (tint == 2'd1) mono_palette = level[1] ? 24'h55ff55 : 24'h00aa00; // Green P39
            else if (tint == 2'd2) mono_palette = level[1] ? 24'hffbb33 : 24'hcc7700; // Amber P3
            else mono_palette = level[1] ? 24'hffffff : 24'haaaaaa;                   // Paper White
        end
    endfunction

    wire [23:0] pixel_rgb = mono_r ? mono_palette(mono_pixel, tint_r) : palette(cga_colour);
    wire [23:0] rgb = image_r && !hold_r ? pixel_rgb : 24'b0;
    assign video_request = {1'b0, hold_r, eol_r, sof_r, 1'b1, vs_r, hs_r, de_r, rgb};
    wire unused_index = image_index[18];
endmodule

/* verilator lint_off DECLFILENAME */
// Inline development output using the production shared Direct/Scanlines RTL.
module pcxt_video_dynamic_output #(parameter bit SCANLINES = 0) (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire pixel_ce, source_de, source_sof, source_eol,
    input wire [3:0] rgbi,
    input wire [1:0] source_mode,
    input wire [1:0] mono_tint,
    output wire [31:0] video_request,
    output reg [27:0] video_response
);
    pcxt_video_dynamic adapter (.*);
    reg [31:0] request_q;
    wire [27:0] response;
    generate if (SCANLINES) begin: scanlines
        fes_video_part_scanlines part (.clock(clk_pixel), .video_request(request_q), .video_response(response));
    end else begin: direct
        fes_video_part_direct part (.video_request(request_q), .video_response(response));
    end endgenerate
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin request_q <= 32'd0; video_response <= 28'd0; end
        else begin request_q <= video_request; video_response <= response; end
    end
endmodule
/* verilator lint_on DECLFILENAME */
