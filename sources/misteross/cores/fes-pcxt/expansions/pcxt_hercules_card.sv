// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"
// Combined MDA / Hercules Graphics Card (HGC) ISA display adapter.
// Parameterized board fabric. Font ROM is external (char*16 + row, rows 0..13).
// When hercules_enable=0: strict IBM MDA (4 KiB VRAM at B0000-B7FFF, 3B0-3B7/3B8/3BA, 3BFh ignored).
// When hercules_enable=1: full Hercules HGC (64 KiB VRAM, 3BFh config, 720x348 graphics, 3BAh !vsync).
module pcxt_hercules_card #(
    parameter integer CLOCK_HZ = 100000000
) (
    input wire clk, reset, enable, hercules_enable,
    input wire [47:0] request,
    output wire [31:0] response,
    output wire [11:0] font_address,
    input wire [7:0] font_data,
    // Independent guest raster stream, sampled only when pixel_ce is high.
    output reg pixel_ce, de, hs, vs, sof, eol,
    output reg [3:0] rgbi,
    output wire graphics_mode, unsupported_interlace
);
    wire clear = reset || request[`FES_PCXT_ISA_RESET_BIT];
    wire active, commit;
    wire [47:0] access_request;
    wire [19:0] address = access_request[19:0];
    wire [1:0] kind = access_request[29:28];
    wire writing = access_request[30];
    wire [7:0] wdata = access_request[27:20];

    reg [7:0] mode;
    reg [1:0] config_reg;
    wire gfx_mode = hercules_enable && config_reg[0] && mode[1];
    wire gfx_page = hercules_enable && config_reg[1] && mode[7];
    assign graphics_mode = gfx_mode;

    // XT ignores A15:10 on I/O ports; 3B0-3BF is 6'h3b.
    wire io_select = enable && active && kind == 1 && !access_request[35] && address[9:4] == 6'h3b;
    wire page0_select = enable && active && kind == 0 && address[19:15] == 5'b10110;
    wire page1_select = enable && active && kind == 0 && address[19:15] == 5'b10111 &&
                        hercules_enable && config_reg[1];
    wire mem_select = page0_select || page1_select;
    wire [3:0] port = address[3:0];

    wire crtc_write = commit && io_select && writing && !port[3];
    wire [7:0] crtc_data;
    wire [13:0] ma;
    wire [4:0] ra;
    wire crtc_de, crtc_hs, crtc_vs, cursor, line_end, frame_end;

    reg [15:0] vram [0:32767];
    reg [15:0] cpu_word, video_word;
    wire [14:0] cpu_address = hercules_enable ? address[15:1] : {4'b0, address[11:1]};
    wire [14:0] display_address = gfx_mode ? {gfx_page, ra[1:0], ma[11:0]} :
                                  (hercules_enable ? {1'b0, ma[13:0]} : {4'b0, ma[10:0]});

    // Prepare byte-write eligibility while the endpoint decodes the held request.
    reg mem_write_eligible, mem_write_high;
    always @(posedge clk) begin : prepare_vram_write
        mem_write_eligible <= mem_select && writing;
        mem_write_high <= address[0];
    end
    always @(posedge clk) begin
        cpu_word <= vram[cpu_address];
        video_word <= vram[display_address];
        if (commit && mem_write_eligible) begin
            if (mem_write_high) vram[cpu_address][15:8] <= wdata;
            else vram[cpu_address][7:0] <= wdata;
        end
    end
    wire [7:0] mem_data = address[0] ? cpu_word[15:8] : cpu_word[7:0];

    reg [7:0] io_data;
    wire [31:0] endpoint_response;
    pcxt_isa_endpoint endpoint (
        .clk(clk), .reset(reset), .request(enable ? request : 48'd0), .response(endpoint_response),
        .device_valid(active), .device_request(access_request),
        .device_claim(io_select || mem_select), .device_ready(1'b1),
        .device_rdata(mem_select ? mem_data : io_data), .device_commit(commit),
        .irq(8'b0), .drq(3'b0)
    );
    assign response = enable ? endpoint_response : 32'd0;

    always @(posedge clk) begin
        if (clear) begin
            mode <= 8'h00;
            config_reg <= 2'b00;
        end else if (commit && io_select && writing) begin
            if (port == 4'd8) mode <= wdata;
            if (hercules_enable && port == 4'd15) config_reg <= wdata[1:0];
        end
    end

    // Fractional 16.257 MHz master-dot enable (shared by MDA and Hercules).
    reg [26:0] phase;
    localparam integer DOT_RATE = 16257000;
    localparam integer DOT_WRAP = CLOCK_HZ - DOT_RATE;
    initial begin
        if (CLOCK_HZ < 2 * DOT_RATE || CLOCK_HZ > 134217728)
            $fatal(1, "Unsupported board clock for the MDA/Hercules dot-enable accumulator");
    end
    reg dot_ce;
    always @(posedge clk) begin : predict_dot_enable
        if (clear) dot_ce <= 1'b0;
        else dot_ce <= !dot_ce && phase >= 27'(DOT_WRAP - DOT_RATE);
    end

    reg [3:0] dot;
    wire [3:0] last_dot = gfx_mode ? 4'd15 : 4'd8;
    wire char_ce = dot_ce && dot >= last_dot;
    pcxt_6845 crtc (
        .clk(clk), .reset(clear), .char_ce(char_ce),
        .select_write(crtc_write && !port[0]), .data_write(crtc_write && port[0]),
        .wdata(wdata), .rdata(crtc_data), .light_pen(1'b0),
        .ma(ma), .ra(ra), .display_enable(crtc_de), .hsync(crtc_hs), .vsync(crtc_vs),
        .cursor(cursor), .line_end(line_end), .frame_end(frame_end),
        .unsupported_interlace(unsupported_interlace)
    );

    reg first_pixel;
    reg [4:0] blink_frames;
    assign font_address = {video_word[7:0], ra[3:0]};

    // Preserve the status-read video-dot bit on port 3BAh bit 3.
    wire [15:0] live_gfx_word = {video_word[7:0], video_word[15:8]};
    wire status_glyph = dot < 4'd8 ? font_data[3'd7 - dot[2:0]] :
                        video_word[7:5] == 3'b110 && font_data[0];
    wire status_ink = (status_glyph || (video_word[10:8] == 3'd1 &&
                      video_word[14:12] == 3'd0 && ra == 5'd13)) &&
                      !(mode[5] && video_word[15] && blink_frames[4]);
    wire status_text_nonzero = cursor ||
        ((video_word[14:12] == 3'd7 && video_word[10:8] == 3'd0) ? !status_ink :
         ((video_word[14:12] != 3'd0 || video_word[10:8] != 3'd0) && status_ink));
    wire status_nonzero = mode[3] && crtc_de &&
        (gfx_mode ? live_gfx_word[4'd15 - dot] : status_text_nonzero);
    reg status_video;
    always @(posedge clk) begin : status_stage
        if (clear) status_video <= 1'b0;
        else if (dot_ce) status_video <= status_nonzero;
    end

    always @* begin
        io_data = 8'hff;
        if (!port[3] && port[0]) io_data = crtc_data;
        else if (port == 4'd10) begin
            io_data = hercules_enable ? {!crtc_vs, 3'b000, status_video, 2'b00, crtc_hs} :
                                        {4'hf, status_video, 2'b00, crtc_hs};
        end
    end

    // One-cycle renderer pipeline matching pcxt_display_card.
    reg [29:0] render_inputs;
    reg [7:0] render_font;
    reg [4:0] render_markers;
    reg render_valid;
    wire [15:0] render_word;
    wire [3:0] render_dot;
    wire [4:0] render_ra;
    wire render_enable_blink, render_enable_video;
    wire render_gfx, render_cursor, render_blink;
    wire render_de, render_hs, render_vs, render_sof, render_eol;
    assign {render_word, render_dot, render_ra, render_enable_blink, render_enable_video,
            render_gfx, render_cursor, render_blink} = render_inputs;
    assign {render_de, render_hs, render_vs, render_sof, render_eol} = render_markers;

    always @(posedge clk) begin : display_input_stage
        render_inputs <= {video_word, dot, ra, mode[5], mode[3], gfx_mode, cursor, blink_frames[4]};
        render_font <= font_data;
        render_markers <= {crtc_de, crtc_hs, crtc_vs, first_pixel, dot >= last_dot && line_end};
        render_valid <= !clear && dot_ce;
    end

    wire glyph = render_dot < 4'd8 ? render_font[3'd7 - render_dot[2:0]] :
                 render_word[7:5] == 3'b110 && render_font[0];
    wire [7:0] attribute = render_word[15:8];
    wire blink_hidden = render_enable_blink && attribute[7] && render_blink;
    wire [15:0] graphics_word = {render_word[7:0], render_word[15:8]};
    reg [3:0] pixel;
    reg ink;
    always @* begin
        pixel = 4'b0;
        ink = 1'b0;
        if (render_de && render_enable_video) begin
            if (render_gfx) begin
                pixel = graphics_word[4'd15 - render_dot] ? 4'd7 : 4'b0;
            end else begin
                ink = (glyph || (attribute[2:0] == 3'd1 && attribute[6:4] == 3'd0 &&
                       render_ra == 5'd13)) && !blink_hidden;
                if (attribute[6:4] == 3'd7 && attribute[2:0] == 3'd0)
                    pixel = ink ? 4'b0 : 4'd7;
                else if (attribute[6:4] == 3'd0 && attribute[2:0] == 3'd0)
                    pixel = 4'b0;
                else
                    pixel = ink ? (attribute[3] ? 4'd15 : 4'd7) : 4'b0;
                if (render_cursor) pixel = attribute[3] ? 4'd15 : 4'd7;
            end
        end
    end

    always @(posedge clk) begin : display_output_stage
        if (clear) begin
            pixel_ce <= 1'b0; de <= 1'b0; hs <= 1'b0; vs <= 1'b0;
            sof <= 1'b0; eol <= 1'b0; rgbi <= 4'b0;
        end else begin
            pixel_ce <= render_valid;
            sof <= 1'b0; eol <= 1'b0;
            if (render_valid) begin
                rgbi <= pixel; de <= render_de; hs <= render_hs; vs <= render_vs;
                sof <= render_sof; eol <= render_eol;
            end
        end
    end

    always @(posedge clk) begin
        if (clear) begin
            phase <= 27'd0; dot <= 4'd0; first_pixel <= 1'b1; blink_frames <= 5'd0;
        end else begin
            phase <= dot_ce ? phase - 27'(DOT_WRAP) : phase + 27'(DOT_RATE);
            if (dot_ce) begin
                dot <= dot >= last_dot ? 4'd0 : dot + 4'd1;
                first_pixel <= 1'b0;
                if (char_ce && frame_end) begin
                    first_pixel <= 1'b1;
                    blink_frames <= blink_frames + 5'd1;
                end
            end
        end
    end

    wire unused_signals = &{1'b0, access_request[47:36], access_request[34:31], address[14:10], mode[6], mode[4], mode[2:0]};
endmodule
