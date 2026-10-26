// SPDX-License-Identifier: MIT
// pc386 VGA scanout (docs/pc386-machine-design.md, "Scanout and retrace" and
// "Capture and presentation at 720p"): the CRTC raster at the guest's own dot
// clock, line prefetch from VRAM, the pixel path through the attribute
// controller and the DAC, and the capture writer.
//
// Raster. A rational enable of the system clock gives the 25.175 MHz or
// 28.322 MHz dot clock (misc output bits 3:2), halved by the sequencer's
// dot clock /2; characters are 8 or 9 dots. Totals, display ends, retrace and
// line compare come from the CRTC registers, so input status 1 shows the
// retrace the programmed mode produces. The start address, preset row scan and
// byte panning latch at the start of vertical retrace; pixel panning is
// sampled at the start of each line. The system clock must be at least twice
// the dot clock: the pixel pipeline advances one stage per dot.
//
// Addressing. The character counter C starts each line at the line's base and
// counts character clocks (by 2 or 4 when CRTC 17h bit 3 or 14h bit 5 asks);
// a row of MSL+1 scan lines (each doubled when CRTC 9 bit 7 is set) shares a
// base, and the next row adds twice the offset register. Line compare restarts
// the next line at base 0, row scan 0. The planar address is C in byte and
// doubleword mode (chain-4 stores pixel A at offset A >> 2, so mode 13h reads
// consecutive offsets) and C << 1 with bit 0 from C[13] or C[15] in word mode;
// CRTC 17h bits 0 and 1 substitute row scan bits 0 and 1 for bits 13 and 14.
//
// Prefetch. During each line the next display line's words are read from
// port 1 into one half of a two-line buffer, one single-beat read per
// character column, at most 12 outstanding. Text mode reads, for each column
// as its character arrives, the font byte from plane 2 at the character map
// base + character * 32 + row scan. A line whose fetch has not finished when
// it starts shows black and counts as an underflow.
//
// Pixels. Text: the glyph bit, the 9th dot repeating dot 8 for C0h-DFh with
// line graphics, attribute blink (frame counter bit 4), the cursor (bit 3)
// as foreground rows, underline. Planar 16-colour, CGA interleaved and
// 256-colour shift modes. Then colour plane enable, the palette, P54S and
// colour select, the PEL mask and the DAC; 256-colour pixels skip the palette
// and pair two dots. DAC writes affect the next pixel read.
//
// Capture. Each display line's pixels, up to 800 by 600, go to one of three
// buffers at CAPTURE_BASE, 2 MiB apart, 3200 bytes per line, one word per
// pixel (B, G, R, 0 in byte order; 6-bit DAC values widened by replication).
// A frame starts in a buffer that is neither the newest complete one nor the
// one the presenter shows, and is published once its last line is written.
module pc386_vga_scanout #(
    parameter [28:0] CAPTURE_BASE = 29'h06140000,
    parameter integer DOT0_N = 1007, DOT0_D = 4000,
    parameter integer DOT1_N = 14161, DOT1_D = 50000
) (
    input wire clk, reset,
    input wire [7:0] misc,
    input wire video_enable,
    input wire [1:0] seq_reset,
    input wire [7:0] seq_clocking,
    input wire [5:0] char_map,
    input wire [1:0] shift_mode,
    input wire graphics,
    // CRTC registers by index (decimal)
    input wire [7:0] crtc0, crtc1, crtc6, crtc7, crtc8, crtc9, crtc10, crtc11, crtc12, crtc13,
    input wire [7:0] crtc14, crtc15, crtc16, crtc17, crtc18, crtc19, crtc20, crtc23, crtc24,
    input wire attr_pas,
    input wire [7:0] attr_mode,
    input wire [5:0] overscan,
    input wire [3:0] plane_enable, pel_pan, color_select,
    input wire [7:0] pel_mask,
    input wire [7:0] palette0, palette1, palette2, palette3, palette4, palette5, palette6, palette7,
    input wire [7:0] palette8, palette9, palette10, palette11, palette12, palette13, palette14, palette15,
    // DAC, written and read back by the register side
    input wire dac_write,
    input wire [7:0] dac_write_index,
    input wire [17:0] dac_write_data,
    input wire [7:0] dac_read_index,
    input wire [1:0] dac_read_cycle,
    output wire [7:0] dac_read_data,
    output wire [7:0] status,
    // Port 1 line fetch: single beats, granted one at a time, answered in order
    output wire fetch_request,
    output wire [14:0] fetch_beat,
    input wire fetch_grant,
    input wire fetch_valid,
    input wire [63:0] fetch_data,
    // Port 2 capture writes
    output reg [28:0] cap_address,
    output reg [7:0] cap_burstcount,
    output reg cap_write,
    output wire [63:0] cap_writedata,
    input wire cap_waitrequest,
    output reg [1:0] frame_buffer,
    output reg [9:0] frame_width, frame_height,
    output reg [15:0] frame_sequence,
    input wire [1:0] present_buffer,
    output reg frame_start, frame_done, start_latched,
    output reg [15:0] frame_count,
    output reg [31:0] underflows, capture_drops
);
    // ---- Timing from the CRTC -------------------------------------------------
    wire [8:0] htotal = {1'b0, crtc0} + 9'd5;
    wire [8:0] hde = {1'b0, crtc1} + 9'd1;
    wire [10:0] vtotal = {1'b0, crtc7[5], crtc7[0], crtc6} + 11'd2;
    wire [10:0] vde = {1'b0, crtc7[6], crtc7[1], crtc18} + 11'd1;
    wire [9:0] vrs = {crtc7[7], crtc7[2], crtc16};
    wire [9:0] line_compare = {crtc9[6], crtc7[4], crtc24};
    wire [4:0] msl = crtc9[4:0];
    wire double_scan = crtc9[7];
    wire nine_dot = !seq_clocking[0];
    wire [3:0] dots = nine_dot ? 4'd9 : 4'd8;
    wire mode256 = shift_mode[1];
    wire text = !graphics;

    // ---- Dot clock --------------------------------------------------------------
    wire tick0, tick1;
    pc386_rate #(.N(DOT0_N), .D(DOT0_D)) rate0 (.clk(clk), .reset(reset), .tick(tick0));
    pc386_rate #(.N(DOT1_N), .D(DOT1_D)) rate1 (.clk(clk), .reset(reset), .tick(tick1));
    wire dot_tick = misc[3:2] == 2'd1 ? tick1 : tick0;
    reg half;
    always @(posedge clk) if (reset) half <= 0; else if (dot_tick) half <= !half;
    wire dot = dot_tick && (!seq_clocking[3] || half);

    // ---- Raster -----------------------------------------------------------------------
    reg [3:0] dot_count;
    reg [8:0] hcount;
    reg [10:0] vcount;
    reg retrace;
    reg [15:0] start_address;
    reg [4:0] preset_row;
    reg [1:0] byte_pan;
    wire char_end = dot && dot_count >= dots - 4'd1;
    wire line_end = char_end && hcount >= htotal - 9'd1;
    wire [10:0] vnext = vcount + 11'd1 >= vtotal ? 11'd0 : vcount + 11'd1;
    wire display_area = hcount < hde && vcount < vde;
    assign status = {4'b0, retrace, 2'b0, !display_area};

    // ---- Fetch line state --------------------------------------------------------
    reg [15:0] line_base;
    reg [4:0] line_row;
    reg line_double, line_split;
    reg [10:0] fetched_line;
    reg fetch_slot;          // slot of the latest fetch
    reg slot_done [0:1];
    // A fetch waits here until the fetcher is free.
    reg pending_valid, pending_slot, pending_split;
    reg [15:0] pending_base;
    reg [4:0] pending_row;
    wire [10:0] fetch_target = vnext + 11'd1 >= vtotal ? 11'd0 : vnext + 11'd1;
    wire fetch_trigger = line_end && fetch_target < vde;
    wire [15:0] next_base = fetch_target == 0 ? start_address + {14'b0, byte_pan} :
                            fetched_line[9:0] == line_compare ? 16'd0 :
                            (double_scan && !line_double) || line_row != msl ? line_base :
                            line_base + {7'b0, crtc19, 1'b0};
    wire [4:0] next_row = fetch_target == 0 ? preset_row :
                          fetched_line[9:0] == line_compare ? 5'd0 :
                          double_scan && !line_double ? line_row :
                          line_row == msl ? 5'd0 : line_row + 5'd1;
    wire next_double = fetch_target != 0 && fetched_line[9:0] != line_compare && double_scan && !line_double;
    wire next_split = fetch_target != 0 && (line_split || fetched_line[9:0] == line_compare);

    // Per-slot line facts for the display.
    reg [4:0] slot_row [0:1];
    reg slot_split [0:1];
    reg [7:0] slot_cursor [0:1];   // bit 7: no cursor on this line

    // ---- Fetcher -------------------------------------------------------------------
    reg busy, f_slot, f_text;
    reg [15:0] f_base;
    reg [4:0] f_row;
    reg [7:0] f_column, f_columns;
    reg [8:0] f_received, f_expected;
    reg [7:0] f_cursor;
    wire [8:0] columns_wanted = hde + 9'd2;
    wire [7:0] columns = columns_wanted > 9'd128 ? 8'd128 : columns_wanted[7:0];
    wire [15:0] cursor_location = {crtc14, crtc15};
    wire [1:0] count_shift = crtc20[5] ? 2'd2 : crtc23[3] ? 2'd1 : 2'd0;
    wire [15:0] c_value = f_base + ({8'b0, f_column} >> count_shift);
    function automatic [15:0] planar(input [15:0] c, input [4:0] row, input [7:0] mode, input dword);
        reg [15:0] address;
        begin
            address = dword || mode[6] ? c : {c[14:0], mode[5] ? c[15] : c[13]};
            if (!mode[0]) address[13] = row[0];
            if (!mode[1]) address[14] = row[1];
            planar = address;
        end
    endfunction
    wire [15:0] column_address = planar(c_value, f_row, crtc23, crtc20[6]);
    // Font reads queue as characters arrive.
    wire font_queue_full, font_queue_empty;
    wire [4:0] font_queue_count;
    wire [7:0] font_column;
    wire [15:0] font_address;
    reg font_push;
    reg [7:0] font_push_column;
    reg [15:0] font_push_address;
    wire font_issue, column_issue;
    pc386_fifo #(.WIDTH(24), .DEPTH(16)) font_queue (
        .clk(clk), .reset(reset), .push(font_push), .din({font_push_column, font_push_address}),
        .full(font_queue_full), .pop(font_issue), .dout({font_column, font_address}),
        .empty(font_queue_empty), .count(font_queue_count)
    );
    // Outstanding reads: {font, half, column}
    wire [4:0] outstanding;
    wire tags_full, tags_empty;
    wire tag_font, tag_half;
    wire [7:0] tag_column;
    wire [4:0] chars_in_flight_bound = outstanding + font_queue_count;
    assign font_issue = fetch_grant && !font_queue_empty;
    assign column_issue = fetch_grant && font_queue_empty;
    pc386_fifo #(.WIDTH(10), .DEPTH(16)) tags (
        .clk(clk), .reset(reset), .push(fetch_grant),
        .din(font_issue ? {1'b1, font_address[0], font_column} : {1'b0, column_address[0], f_column}),
        .full(tags_full), .pop(fetch_valid), .dout({tag_font, tag_half, tag_column}), .empty(tags_empty),
        .count(outstanding)
    );
    wire columns_left = busy && f_column < f_columns;
    assign fetch_request = (!font_queue_empty || (columns_left && (!f_text || chars_in_flight_bound < 5'd15)))
                           && outstanding < 5'd12;
    assign fetch_beat = !font_queue_empty ? font_address[15:1] : column_address[15:1];

    wire [31:0] response_word = tag_half ? fetch_data[63:32] : fetch_data[31:0];
    wire map_a = response_word[11];   // attribute bit 3 selects character map A
    wire [2:0] map_index = map_a ? {char_map[5], char_map[3:2]} : {char_map[4], char_map[1:0]};
    wire [15:0] map_base = {map_index[1:0], map_index[2], 13'b0};

    // ---- Line buffer ----------------------------------------------------------------
    reg [7:0] display_column;
    reg display_slot;
    wire [31:0] buffer_word;
    wire [7:0] buffer_font;
    pc386_ram #(.WIDTH(32), .DEPTH(256)) words (
        .clk(clk), .write(fetch_valid && !tag_font), .waddress({f_slot, tag_column[6:0]}), .wdata(response_word),
        .wbyteenable(1'b1), .read(1'b1), .raddress({display_slot, display_column[6:0]}), .rdata(buffer_word)
    );
    pc386_ram #(.WIDTH(8), .DEPTH(256)) fonts (
        .clk(clk), .write(fetch_valid && tag_font), .waddress({f_slot, tag_column[6:0]}), .wdata(response_word[23:16]),
        .wbyteenable(1'b1), .read(1'b1), .raddress({display_slot, display_column[6:0]}), .rdata(buffer_font)
    );

    always @(posedge clk) begin
        font_push <= 0;
        if (reset) begin
            busy <= 0; f_slot <= 0; f_text <= 0; f_base <= 0; f_row <= 0; f_column <= 0; f_columns <= 0;
            f_received <= 0; f_expected <= 0; f_cursor <= 8'h80;
            pending_valid <= 0; pending_slot <= 0; pending_split <= 0; pending_base <= 0; pending_row <= 0;
            line_base <= 0; line_row <= 0; line_double <= 0; line_split <= 0; fetched_line <= 0;
            fetch_slot <= 0; slot_done[0] <= 0; slot_done[1] <= 0;
            slot_row[0] <= 0; slot_row[1] <= 0; slot_split[0] <= 0; slot_split[1] <= 0;
            slot_cursor[0] <= 8'h80; slot_cursor[1] <= 8'h80;
            font_push_column <= 0; font_push_address <= 0;
        end else begin
            if (fetch_trigger) begin
                line_base <= next_base; line_row <= next_row; line_double <= next_double;
                line_split <= next_split; fetched_line <= fetch_target;
                pending_valid <= 1; pending_slot <= !fetch_slot; pending_base <= next_base;
                pending_row <= next_row; pending_split <= next_split;
                fetch_slot <= !fetch_slot;
                slot_done[!fetch_slot] <= 0;
            end
            if (!busy && pending_valid && !fetch_trigger) begin
                busy <= 1; pending_valid <= 0;
                f_slot <= pending_slot; f_base <= pending_base; f_row <= pending_row;
                f_column <= 0; f_columns <= columns; f_received <= 0; f_text <= text;
                f_expected <= text ? {columns, 1'b0} : {1'b0, columns};
                f_cursor <= 8'h80;
                slot_row[pending_slot] <= pending_row;
                slot_split[pending_slot] <= pending_split;
            end
            if (column_issue) begin
                f_column <= f_column + 8'd1;
                if (c_value == cursor_location && f_cursor[7]) f_cursor <= f_column;
            end
            if (fetch_valid) begin
                f_received <= f_received + 9'd1;
                if (!tag_font && f_text) begin
                    font_push <= 1;
                    font_push_column <= tag_column;
                    font_push_address <= map_base + {3'b0, response_word[7:0], f_row};
                end
            end
            if (busy && f_column == f_columns && f_received == f_expected && !font_push) begin
                busy <= 0;
                slot_done[f_slot] <= 1;
                slot_cursor[f_slot] <= f_cursor;
            end
        end
    end

    // ---- Raster and display line ------------------------------------------------
    reg line_shown, line_blank;
    reg [4:0] display_row;
    reg display_split;
    reg [7:0] display_cursor;
    reg [3:0] sub;               // dot within the character, or pixel within the word in 256-colour
    reg pixel_phase;             // 256-colour: second dot of a pixel
    reg [9:0] pixel_x;
    reg [9:0] line_pixels, captured_lines;
    reg capturing, frame_open;
    wire [3:0] text_pan = nine_dot ? (pel_pan >= 4'd8 ? 4'd0 : pel_pan + 4'd1) : {1'b0, pel_pan[2:0]};
    always @(posedge clk) begin
        frame_start <= 0;
        start_latched <= 0;
        if (reset) begin
            dot_count <= 0; hcount <= 0; vcount <= 0; retrace <= 0;
            start_address <= 0; preset_row <= 0; byte_pan <= 0; frame_count <= 0;
            line_shown <= 0; line_blank <= 0; display_row <= 0; display_split <= 0; display_cursor <= 8'h80;
            display_slot <= 0; underflows <= 0;
        end else if (dot) begin
            dot_count <= char_end ? 4'd0 : dot_count + 4'd1;
            if (char_end) hcount <= line_end ? 9'd0 : hcount + 9'd1;
            if (line_end) begin
                vcount <= vnext;
                if (vnext[9:0] == vrs) begin
                    retrace <= 1;
                    start_address <= {crtc12, crtc13};
                    preset_row <= crtc8[4:0];
                    byte_pan <= crtc8[6:5];
                    start_latched <= 1;
                end else if (retrace && vnext[3:0] == crtc17[3:0]) retrace <= 0;
                if (vnext == 0) begin
                    frame_count <= frame_count + 16'd1;
                    frame_start <= 1;
                end
                // The next line shows the slot fetched for it.
                line_shown <= vnext < vde;
                if (vnext < vde) begin
                    display_slot <= fetch_slot;
                    line_blank <= !slot_done[fetch_slot];
                    if (!slot_done[fetch_slot]) underflows <= underflows + 32'd1;
                    display_row <= slot_row[fetch_slot];
                    display_split <= slot_split[fetch_slot];
                    display_cursor <= slot_cursor[fetch_slot];
                end
            end
        end
    end

    // ---- Pixel pipeline ---------------------------------------------------------
    // The generator holds the column and dot of the next display dot, so the
    // line buffer has read that column by the time the dot comes. Stage A
    // takes the word; stage B forms the attribute and DAC index; at the next
    // dot the DAC has read that index and capture takes the colour with
    // stage B's valid and blank flags. Each stage moves on a dot.
    // Sampled as the next line starts, from that line's own facts.
    wire panning_off = slot_split[fetch_slot] && attr_mode[5];
    wire [3:0] start_dots = panning_off ? 4'd0 : mode256 ? {1'b0, pel_pan[3:1]} : text ? text_pan : pel_pan;
    reg a_valid, b_valid;
    reg [3:0] a_sub;
    reg [1:0] a_plane;
    reg [7:0] a_column;
    reg [31:0] a_word;
    reg [7:0] a_font;
    reg b_black;
    reg [7:0] dac_index;
    wire [17:0] dac_rgb;
    reg [9:0] source;   // 256-colour: the source pixel; its plane is source mod 4
    wire [9:0] source_next = source + 10'd1;
    wire pixel_dot = !mode256 || !pixel_phase;
    wire shown_dot = display_area && line_shown;
    always @(posedge clk) begin
        if (reset) begin
            sub <= 0; pixel_phase <= 0; source <= 0; display_column <= 0;
            a_valid <= 0; a_sub <= 0; a_plane <= 0; a_column <= 0; a_word <= 0; a_font <= 0;
        end else if (dot) begin
            a_valid <= shown_dot && pixel_dot && !line_end;
            a_sub <= sub;
            a_plane <= source[1:0];
            a_column <= display_column;
            a_word <= buffer_word;
            a_font <= buffer_font;
            if (line_end) begin
                // The first column and dot of the next line, after panning.
                pixel_phase <= 0;
                source <= {6'b0, start_dots};
                if (mode256) begin display_column <= {6'b0, start_dots[3:2]}; sub <= 0; end
                else if (start_dots >= dots) begin display_column <= 8'd1; sub <= start_dots - dots; end
                else begin display_column <= 8'd0; sub <= start_dots; end
            end else if (shown_dot) begin
                if (mode256) begin
                    pixel_phase <= !pixel_phase;
                    if (pixel_phase) begin
                        source <= source_next;
                        display_column <= source_next[9:2];
                    end
                end else if (sub == dots - 4'd1) begin
                    sub <= 0;
                    display_column <= display_column + 8'd1;
                end else sub <= sub + 4'd1;
            end
        end
    end

    function automatic [7:0] pick(input [3:0] index, input [7:0] p0, input [7:0] p1, input [7:0] p2,
                                  input [7:0] p3, input [7:0] p4, input [7:0] p5, input [7:0] p6,
                                  input [7:0] p7, input [7:0] p8, input [7:0] p9, input [7:0] p10,
                                  input [7:0] p11, input [7:0] p12, input [7:0] p13, input [7:0] p14,
                                  input [7:0] p15);
        case (index)
            4'd0: pick = p0; 4'd1: pick = p1; 4'd2: pick = p2; 4'd3: pick = p3;
            4'd4: pick = p4; 4'd5: pick = p5; 4'd6: pick = p6; 4'd7: pick = p7;
            4'd8: pick = p8; 4'd9: pick = p9; 4'd10: pick = p10; 4'd11: pick = p11;
            4'd12: pick = p12; 4'd13: pick = p13; 4'd14: pick = p14; default: pick = p15;
        endcase
    endfunction
    wire char_blink = !frame_count[4];
    wire cursor_blink = !frame_count[3];
    wire [7:0] character = a_word[7:0];
    wire [7:0] attribute = a_word[15:8];
    wire glyph_bit = a_sub < 4'd8 ? a_font[3'd7 - a_sub[2:0]] :
                     attr_mode[2] && character[7:5] == 3'b110 && a_font[0];
    wire blink_mode = attr_mode[3];
    wire [3:0] text_background = blink_mode ? {1'b0, attribute[6:4]} : attribute[7:4];
    wire cursor_here = !display_cursor[7] && a_column == display_cursor && !crtc10[5] &&
                       display_row >= crtc10[4:0] && display_row <= crtc11[4:0] && cursor_blink;
    wire underline = display_row == crtc20[4:0] && attribute[2:0] == 3'b001 && attribute[6:4] == 3'b000;
    wire text_on = cursor_here || underline || (glyph_bit && !(blink_mode && attribute[7] && !char_blink));
    wire [2:0] bit_index = 3'd7 - a_sub[2:0];
    wire [3:0] planar_attribute = {a_word[{2'd3, bit_index}], a_word[{2'd2, bit_index}], a_word[{2'd1, bit_index}], a_word[{2'd0, bit_index}]};
    wire [1:0] cga_shift = 2'd3 - a_sub[1:0];
    wire [7:0] cga_low = a_sub[2] ? a_word[15:8] : a_word[7:0];
    wire [7:0] cga_high = a_sub[2] ? a_word[31:24] : a_word[23:16];
    wire [3:0] cga_attribute = {cga_high[2 * cga_shift +: 2], cga_low[2 * cga_shift +: 2]};
    wire [3:0] graphic_attribute = (shift_mode[0] ? cga_attribute : a_sub >= 4'd8 ? 4'd0 : planar_attribute);
    wire [3:0] attribute4 = (text ? (text_on ? attribute[3:0] : text_background) : graphic_attribute) & plane_enable;
    wire [7:0] palette_value = pick(attribute4, palette0, palette1, palette2, palette3, palette4, palette5,
                                    palette6, palette7, palette8, palette9, palette10, palette11, palette12,
                                    palette13, palette14, palette15);
    wire [7:0] attribute_index = attr_mode[7] ? {color_select, palette_value[3:0]} :
                                 {color_select[3:2], palette_value[5:0]};
    wire [7:0] pixel_byte = a_word[8 * a_plane +: 8];
    wire screen_off = !video_enable || seq_reset != 2'b11 || seq_clocking[5];
    always @(posedge clk) begin
        if (reset) begin
            b_valid <= 0; b_black <= 0; dac_index <= 0;
        end else if (dot) begin
            b_valid <= a_valid;
            b_black <= screen_off || line_blank;
            dac_index <= (!attr_pas ? {2'b0, overscan} : mode256 ? pixel_byte : attribute_index) & pel_mask;
        end
    end
    wire unused_pixel = ^{captured_lines, capturing, frame_open, tags_full, tags_empty};

    // ---- DAC ----------------------------------------------------------------------
    pc386_ram #(.WIDTH(18), .DEPTH(256)) dac_scan (
        .clk(clk), .write(dac_write), .waddress(dac_write_index), .wdata(dac_write_data), .wbyteenable(1'b1),
        .read(1'b1), .raddress(dac_index), .rdata(dac_rgb)
    );
    wire [17:0] dac_cpu;
    pc386_ram #(.WIDTH(18), .DEPTH(256)) dac_copy (
        .clk(clk), .write(dac_write), .waddress(dac_write_index), .wdata(dac_write_data), .wbyteenable(1'b1),
        .read(1'b1), .raddress(dac_read_index), .rdata(dac_cpu)
    );
    assign dac_read_data = {2'b0, dac_read_cycle == 2'd0 ? dac_cpu[17:12] :
                                  dac_read_cycle == 2'd1 ? dac_cpu[11:6] : dac_cpu[5:0]};
    function automatic [7:0] widen(input [5:0] value);
        widen = {value, value[5:4]};
    endfunction
    wire [31:0] pixel_word = b_black ? 32'd0 :
                             {8'd0, widen(dac_rgb[17:12]), widen(dac_rgb[11:6]), widen(dac_rgb[5:0])};

    // ---- Capture ---------------------------------------------------------------------
    // Pixels pair into beats; each completed line leaves its beat count in a
    // descriptor queue, so the writer never bursts across a line.
    reg [31:0] low_pixel;
    reg pair;
    reg [8:0] line_beats;
    reg beat_push, descriptor_push;
    reg [63:0] beat_data;
    wire beat_full, beat_empty;
    wire [63:0] beat_head;
    wire [7:0] beat_count;
    reg beat_pop;
    pc386_fifo #(.WIDTH(64), .DEPTH(128)) beats (
        .clk(clk), .reset(reset), .push(beat_push && !beat_full), .din(beat_data), .full(beat_full),
        .pop(beat_pop), .dout(beat_head), .empty(beat_empty), .count(beat_count)
    );
    assign cap_writedata = beat_head;
    wire [8:0] descriptor_head;
    wire descriptor_empty, descriptor_full;
    wire [3:0] descriptor_count;
    reg descriptor_pop;
    reg [8:0] descriptor_data;
    pc386_fifo #(.WIDTH(9), .DEPTH(8)) descriptors (
        .clk(clk), .reset(reset), .push(descriptor_push), .din(descriptor_data), .full(descriptor_full),
        .pop(descriptor_pop), .dout(descriptor_head), .empty(descriptor_empty), .count(descriptor_count)
    );
    reg [1:0] capture_buffer;
    reg [9:0] capture_line, width_seen;
    reg frame_end_pending;
    wire line_capturing = capture_line < 10'd600;
    // Push side: on each pixel; at each display line's end, pad and describe.
    wire line_closing = dot && line_end && line_shown;
    always @(posedge clk) begin
        beat_push <= 0;
        descriptor_push <= 0;
        frame_done <= 0;
        if (reset) begin
            low_pixel <= 0; pair <= 0; line_beats <= 0; beat_data <= 0; descriptor_data <= 0;
            capture_buffer <= 0; capture_line <= 0; width_seen <= 0; line_pixels <= 0; pixel_x <= 0;
            frame_end_pending <= 0; capture_drops <= 0;
            frame_buffer <= 0; frame_width <= 0; frame_height <= 0; frame_sequence <= 0;
            captured_lines <= 0; capturing <= 0; frame_open <= 0;
        end else begin
            if (beat_push && beat_full) capture_drops <= capture_drops + 32'd1;
            if (dot && b_valid && line_capturing && pixel_x < 10'd800) begin
                pixel_x <= pixel_x + 10'd1;
                if (pair) begin
                    beat_push <= 1; beat_data <= {pixel_word, low_pixel}; line_beats <= line_beats + 9'd1;
                end else low_pixel <= pixel_word;
                pair <= !pair;
            end
            // Pipeline latency: a line's last pixel leaves stage C two dots
            // after its last display dot, well before the line ends.
            if (line_closing) begin
                if (line_capturing) begin
                    if (pair) begin beat_push <= 1; beat_data <= {32'd0, low_pixel}; end
                    descriptor_push <= 1;
                    descriptor_data <= line_beats + {8'd0, pair};
                    if (capture_line == 0) width_seen <= pixel_x;
                    capture_line <= capture_line + 10'd1;
                end
                pair <= 0; line_beats <= 0; pixel_x <= 0;
                if (vnext >= vde) frame_end_pending <= 1;
            end
            // Publish once every line of the frame is written.
            if (frame_end_pending && descriptor_empty && beat_empty && !cap_write && !descriptor_push && !beat_push) begin
                frame_end_pending <= 0;
                frame_done <= 1;
                frame_buffer <= capture_buffer;
                frame_width <= width_seen;
                frame_height <= capture_line > 10'd600 ? 10'd600 : capture_line;
                frame_sequence <= frame_sequence + 16'd1;
            end
            if (dot && line_end && vnext == 0) begin
                capture_line <= 0;
                // A buffer that is neither the newest complete one nor the
                // presenter's.
                capture_buffer <= (frame_buffer != 2'd0 && present_buffer != 2'd0) ? 2'd0 :
                                  (frame_buffer != 2'd1 && present_buffer != 2'd1) ? 2'd1 : 2'd2;
            end
        end
    end
    // Writer: bursts of up to 16 beats within a line.
    reg [9:0] write_line;
    reg [8:0] write_beat, burst_left;
    wire [8:0] line_left = descriptor_head - write_beat;
    wire line_known = !descriptor_empty;
    wire [8:0] ready_beats = {1'b0, beat_count};
    wire [8:0] burst = line_known ? (line_left > 9'd16 ? 9'd16 : line_left) : 9'd16;
    wire can_burst = !cap_write && (line_known ? (line_left != 0 && ready_beats >= burst) : ready_beats >= 9'd16);
    wire beat_taken = cap_write && !cap_waitrequest;
    always @* begin
        beat_pop = beat_taken;
        descriptor_pop = !cap_write && line_known && line_left == 0;
    end
    always @(posedge clk) begin
        if (reset) begin
            cap_write <= 0; cap_address <= 0; cap_burstcount <= 0; write_line <= 0; write_beat <= 0; burst_left <= 0;
        end else begin
            if (dot && line_end && vnext == 0) write_line <= 0;
            if (descriptor_pop) begin write_line <= write_line + 10'd1; write_beat <= 0; end
            else if (can_burst) begin
                cap_write <= 1;
                cap_burstcount <= burst[7:0];
                burst_left <= burst;
                // Line l starts at beat 400 l: 512 l - 128 l + 16 l.
                cap_address <= CAPTURE_BASE + {9'b0, capture_buffer, 18'b0} +
                               {10'b0, write_line, 9'b0} - {12'b0, write_line, 7'b0} + {15'b0, write_line, 4'b0} +
                               {20'b0, write_beat};
            end
            if (beat_taken) begin
                write_beat <= write_beat + 9'd1;
                burst_left <= burst_left - 9'd1;
                if (burst_left == 9'd1) cap_write <= 0;
            end
        end
    end
endmodule
