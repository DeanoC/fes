// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"
// Shared implementation behind separately instantiated CGA/MDA ISA cards.
// Parameterized board fabric. The font is a separate combinational ROM supplied by the shell;
// address = character*16 + row, MSB is the leftmost dot. No IBM ROM is bundled.
module pcxt_display_card #(
    parameter bit MDA = 0,
    parameter integer CLOCK_HZ = 100000000
) (
    input wire clk, reset,
    input wire [47:0] request,
    output wire [31:0] response,
    output wire [11:0] font_address,
    input wire [7:0] font_data,
    input wire light_pen_trigger, light_pen_switch,
    // Independent guest raster stream, sampled only when pixel_ce is high.
    output reg pixel_ce, de, hs, vs, sof, eol,
    output reg [3:0] rgbi,
    output wire unsupported_interlace
);
    localparam integer WORD_BITS = MDA ? 11 : 13;
    localparam [9:0] IO_BASE = MDA ? 10'h3b0 : 10'h3d0;
    wire clear = reset || request[`FES_PCXT_ISA_RESET_BIT];
    wire active, commit;
    wire [47:0] access_request;
    wire [19:0] address = access_request[19:0];
    wire [1:0] kind = access_request[29:28];
    wire writing = access_request[30];
    wire [7:0] wdata = access_request[27:20];
    wire io_select = active && kind == 1 && !access_request[35] && address[9:4] == IO_BASE[9:4];
    wire mem_select = active && kind == 0 && address[19:15] == (MDA ? 5'b10110 : 5'b10111);
    wire [3:0] port = address[3:0];
    reg [5:0] mode, colour;
    reg pen_latched, pen_old;
    wire pen_capture = !MDA && !pen_latched && ((light_pen_trigger && !pen_old) ||
                       (commit && io_select && writing && port == 12));
    wire crtc_write = commit && io_select && writing && !port[3];
    wire [7:0] crtc_data;
    wire [13:0] ma;
    wire [4:0] ra;
    wire crtc_de, crtc_hs, crtc_vs, cursor, line_end, frame_end;
    reg [7:0] io_data;
    reg [15:0] vram [0:(1<<WORD_BITS)-1];
    reg [15:0] cpu_word, video_word;
    wire [WORD_BITS-1:0] cpu_address = address[WORD_BITS:1];
    wire [12:0] display_address = !MDA && mode[1] ? {ra[0], ma[11:0]} : ma[12:0];
    // Prepare byte-write eligibility while the endpoint decodes/prepares the
    // held request. Commit cannot occur until that preparation has completed.
    reg mem_write_eligible, mem_write_high;
    always @(posedge clk) begin : prepare_vram_write
        mem_write_eligible <= active && kind == 0 &&
            address[19:15] == (MDA ? 5'b10110 : 5'b10111) && writing;
        mem_write_high <= address[0];
    end
    always @(posedge clk) begin
        cpu_word <= vram[cpu_address];
        video_word <= vram[display_address[WORD_BITS-1:0]];
        if (commit && mem_write_eligible) begin
            if (mem_write_high) vram[cpu_address][15:8] <= wdata;
            else vram[cpu_address][7:0] <= wdata;
        end
    end
    wire [7:0] mem_data = address[0] ? cpu_word[15:8] : cpu_word[7:0];
    pcxt_isa_endpoint endpoint (
        .clk(clk), .reset(reset), .request(request), .response(response),
        .device_valid(active), .device_request(access_request),
        .device_claim(io_select || mem_select), .device_ready(1'b1),
        .device_rdata(mem_select ? mem_data : io_data), .device_commit(commit),
        .irq(8'b0), .drq(3'b0)
    );
    always @* begin
        io_data = 8'hff;
        if (!port[3] && port[0]) io_data = crtc_data;
        else if (port == 10) begin
            if (MDA) io_data = {4'hf, status_video, 2'b00, crtc_hs};
            else io_data = {4'b0, crtc_vs, !light_pen_switch, pen_latched, !crtc_de};
        end
    end
    always @(posedge clk) begin
        if (clear) begin mode <= 0; colour <= 0; pen_latched <= 0; pen_old <= 0; end
        else begin
            pen_old <= light_pen_trigger;
            if (pen_capture) pen_latched <= 1;
            if (commit && io_select && writing) begin
                if (port == 8) mode <= wdata[5:0];
                if (!MDA && port == 9) colour <= wdata[5:0];
                if (!MDA && port == 11) pen_latched <= 0;
            end
        end
    end
    // Fractional master-dot enables: CGA 14.318180 MHz, MDA 16.257 MHz.
    reg [26:0] phase;
    localparam integer DOT_RATE = MDA ? 16257000 : 14318180;
    localparam integer DOT_WRAP = CLOCK_HZ - DOT_RATE;
    initial begin
        if (CLOCK_HZ < 2*DOT_RATE || CLOCK_HZ > 134217728)
            $fatal(1, "Unsupported board clock for the dot-enable accumulator");
    end
    reg dot_ce;
    // Predict the wrap flag for the next phase, keeping the comparison out
    // of downstream CRTC enables without shifting a single master-dot pulse.
    always @(posedge clk) begin : predict_dot_enable
        if (clear) dot_ce <= 0;
        else dot_ce <= !dot_ce && phase >= 27'(DOT_WRAP - DOT_RATE);
    end
    reg [3:0] dot;
    wire [3:0] last_dot = MDA ? 4'd8 : mode[0] ? 4'd7 : 4'd15;
    wire char_ce = dot_ce && dot == last_dot;
    pcxt_6845 crtc (
        .clk(clk), .reset(clear), .char_ce(char_ce),
        .select_write(crtc_write && !port[0]), .data_write(crtc_write && port[0]),
        .wdata(wdata), .rdata(crtc_data), .light_pen(pen_capture),
        .ma(ma), .ra(ra), .display_enable(crtc_de), .hsync(crtc_hs), .vsync(crtc_vs),
        .cursor(cursor), .line_end(line_end), .frame_end(frame_end),
        .unsupported_interlace(unsupported_interlace)
    );
    reg first_pixel;
    reg [4:0] blink_frames;
    assign font_address = {video_word[7:0], MDA ? ra[3:0] : {1'b0, ra[2:0]}};
    // One physical-cycle renderer pipeline. Capture all rendering inputs on
    // every clock so dot enable does not become another high-fanout enable.
    // The valid bit and raster markers travel with the same pixel snapshot.
    reg [46:0] render_inputs;
    reg [4:0] render_markers;
    reg render_valid;
    wire [15:0] render_word;
    wire [7:0] render_font;
    wire [3:0] render_dot;
    wire [4:0] render_ra;
    wire [5:0] render_mode, render_colour;
    wire render_cursor, render_blink;
    wire render_de, render_hs, render_vs, render_sof, render_eol;
    assign {render_word, render_font, render_dot, render_ra, render_mode,
            render_colour, render_cursor, render_blink} = render_inputs;
    assign {render_de, render_hs, render_vs, render_sof, render_eol} = render_markers;
    always @(posedge clk) begin : display_input_stage
        render_inputs <= {video_word, font_data, dot, ra, mode, colour, cursor, blink_frames[4]};
        render_markers <= {crtc_de, crtc_hs, crtc_vs, first_pixel, dot == last_dot && line_end};
        render_valid <= !clear && dot_ce;
    end

    // Preserve the original MDA status-read cycle despite the stream latency.
    // Only the nonzero predicate is needed; cursor overrides attributes.
    wire status_glyph = dot < 8 ? font_data[3'd7-dot[2:0]] :
                        video_word[7:5] == 3'b110 && font_data[0];
    wire status_ink = (status_glyph || (video_word[10:8] == 1 &&
                      video_word[14:12] == 0 && ra == 13)) &&
                      !(mode[5] && video_word[15] && blink_frames[4]);
    wire status_nonzero = mode[3] && crtc_de && (cursor ||
        ((video_word[14:12] == 7 && video_word[10:8] == 0) ? !status_ink :
         ((video_word[14:12] != 0 || video_word[10:8] != 0) && status_ink)));
    reg status_video;
    always @(posedge clk) begin : mda_status_stage
        if (clear) status_video <= 0;
        else if (dot_ce) status_video <= status_nonzero;
    end

    wire [3:0] glyph_dot = MDA || render_mode[0] ? render_dot : {1'b0, render_dot[3:1]};
    wire glyph = glyph_dot < 8 ? render_font[3'd7-glyph_dot[2:0]] :
                 MDA && render_word[7:5] == 3'b110 && render_font[0];
    wire [7:0] attribute = render_word[15:8];
    wire blink_hidden = render_mode[5] && attribute[7] && render_blink;
    wire [15:0] graphics_word = {render_word[7:0], render_word[15:8]};
    wire [1:0] pair_colour = 2'(graphics_word >> (4'd14-{render_dot[3:1],1'b0}));
    reg [3:0] pixel;
    reg ink;
    always @* begin
        pixel = !MDA && !render_mode[0] && !render_mode[4] ? render_colour[3:0] : 4'b0;
        ink = 0;
        if (render_de) begin
            if (!MDA && render_mode[1]) begin
                if (render_mode[4]) pixel = graphics_word[4'd15-render_dot] ? render_colour[3:0] : 4'b0;
                else if (pair_colour == 0) pixel = render_colour[3:0];
                else if (render_mode[2]) begin
                    case (pair_colour)
                        1: pixel = {render_colour[4],3'd3};
                        2: pixel = {render_colour[4],3'd4};
                        default: pixel = {render_colour[4],3'd7};
                    endcase
                end else pixel = {render_colour[4], pair_colour[1], pair_colour[0], render_colour[5]};
            end else if (MDA) begin
                ink = (glyph || (attribute[2:0] == 1 && attribute[6:4] == 0 && render_ra == 13)) && !blink_hidden;
                // 00/08/80/88 invisible; 70/78/F0/F8 reverse, remaining attributes normal.
                if (attribute[6:4] == 7 && attribute[2:0] == 0)
                    pixel = ink ? 4'b0 : 4'd7;
                else if (attribute[6:4] == 0 && attribute[2:0] == 0) pixel = 0;
                else pixel = ink ? (attribute[3] ? 4'd15 : 4'd7) : 4'b0;
                if (render_cursor) pixel = attribute[3] ? 4'd15 : 4'd7;
            end else begin
                pixel = (glyph && !blink_hidden) || render_cursor ? attribute[3:0] :
                         {attribute[7] && !render_mode[5], attribute[6:4]};
            end
        end
        if (!render_mode[3]) pixel = 0;
    end
    always @(posedge clk) begin : display_output_stage
        if (clear) begin
            pixel_ce <= 0; de <= 0; hs <= 0; vs <= 0;
            sof <= 0; eol <= 0; rgbi <= 0;
        end else begin
            pixel_ce <= render_valid;
            sof <= 0; eol <= 0;
            if (render_valid) begin
                rgbi <= pixel; de <= render_de; hs <= render_hs; vs <= render_vs;
                sof <= render_sof; eol <= render_eol;
            end
        end
    end
    always @(posedge clk) begin
        if (clear) begin
            phase <= 0; dot <= 0; first_pixel <= 1; blink_frames <= 0;
        end else begin
            phase <= dot_ce ? phase - 27'(DOT_WRAP) : phase + 27'(DOT_RATE);
            if (dot_ce) begin
                dot <= dot >= last_dot ? 4'b0 : dot + 1'b1;
                first_pixel <= 0;
                if (char_ce && frame_end) begin
                    first_pixel <= 1; blink_frames <= blink_frames + 1'b1;
                end
            end
        end
    end
    wire unused_request = &{1'b0, access_request[47:36], access_request[34:31], address[14:10], ma[13], display_address};
endmodule
