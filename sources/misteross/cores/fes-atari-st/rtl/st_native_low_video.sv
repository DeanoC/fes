// SPDX-License-Identifier: GPL-3.0-or-later
// 320-pixel ST raster capture, including opened bottom display lines.
// RAM rows are prefetched in the system
// domain; palette selection occurs at each native pixel, not at HDMI SOF.
// Three RGB333 banks have explicit publish/release ownership. Pixel may read
// only an immutable completed bank, and changes banks only at output SOF.
// Raw DE/MMU and Timer B timing remain separate from calibrated RGB output.
// This is a net Hatari/STF-calibrated candidate, not one literal hardware delay.
module st_native_low_video (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire native_vblank, native_display,
    input wire native_short_line, native_blank,
    input wire [8:0] native_word_loss,
    input wire [8:0] native_line, native_cycle,
    input wire native_pixel_ce, native_lookup_ce,
    input wire [7:0] sync_mode,
    input wire [23:0] screen_base,
    input wire [1:0] resolution,
    input wire [143:0] palette,
    output reg memory_req,
    output reg [18:1] memory_addr,
    input wire memory_ready,
    input wire [15:0] memory_data,
    input wire ram_write_committed,
    input wire [18:1] ram_write_addr,
    input wire [15:0] ram_write_data,
    input wire [1:0] ram_write_lanes,
    input wire output_sof,
    input wire [16:0] output_address,
    output wire [8:0] output_rgb, output_border, output_raster_rgb,
    input wire [8:0] output_border_row, output_border_x,
    input wire output_border_line_start, output_border_ce,
    output reg output_raster_border, output_pal,
    output reg output_valid,
    output reg [8:0] output_height,
    output reg [31:0] debug_frames, debug_skipped, debug_underruns
);
    // Explicit shallow chunks avoid the mapper selecting ninety 8192x1
    // blocks per 79040x9 bank instead of seventy-eight 1024x10 blocks.
    // Complete-frame ownership is independent of the playback read pipeline.
    localparam integer FRAME_CHUNKS = 78;
    wire [8:0] chunk_read0 [0:FRAME_CHUNKS-1];
    wire [8:0] chunk_read1 [0:FRAME_CHUNKS-1];
    wire [8:0] chunk_read2 [0:FRAME_CHUNKS-1];
    reg [1:0] front_bank;
    // Register local 16-chunk groups before the final OR. With the RAM read,
    // each bank's group registers its own hit and loads its own chunk index
    // only when it is the addressed group, so no one index register drives
    // all fifteen selections. A miss, a chunk past the frame and reset clear
    // a group's word; only the addressed group of the front bank is nonzero.
    // These are pixel registers; all 234 synchronous RAM ports stay unchanged.
    wire [8:0] group_read0 [0:4], group_read1 [0:4], group_read2 [0:4];
    generate for (genvar g = 0; g < 5; g = g + 1) begin : read_groups
        wire [8:0] words0 [0:15], words1 [0:15], words2 [0:15];
        for (genvar k = 0; k < 16; k = k + 1) begin : words
            if (g * 16 + k < FRAME_CHUNKS) begin : present
                assign words0[k] = chunk_read0[g * 16 + k];
                assign words1[k] = chunk_read1[g * 16 + k];
                assign words2[k] = chunk_read2[g * 16 + k];
            end else begin : padding
                assign words0[k] = 9'd0;
                assign words1[k] = 9'd0;
                assign words2[k] = 9'd0;
            end
        end
        wire group_hit = output_address[16:14] == 3'(g);
        wire hit_next0 = group_hit && front_bank == 2'd0;
        wire hit_next1 = group_hit && front_bank == 2'd1;
        wire hit_next2 = group_hit && front_bank[1];
        reg hit0, hit1, hit2;
        reg [3:0] index0, index1, index2;
        reg [8:0] selected0, selected1, selected2;
        always @(posedge clk_pixel) begin
            hit0 <= hit_next0; hit1 <= hit_next1; hit2 <= hit_next2;
            if (hit_next0) index0 <= output_address[13:10];
            if (hit_next1) index1 <= output_address[13:10];
            if (hit_next2) index2 <= output_address[13:10];
            selected0 <= reset_pixel || !hit0 ? 9'd0 : words0[index0];
            selected1 <= reset_pixel || !hit1 ? 9'd0 : words1[index1];
            selected2 <= reset_pixel || !hit2 ? 9'd0 : words2[index2];
        end
        assign group_read0[g] = selected0;
        assign group_read1[g] = selected1;
        assign group_read2[g] = selected2;
    end endgenerate
    reg [2:0] published, released, seen;
    (* async_reg = "true" *) reg [2:0] pub_meta, pub_sync, release_meta, release_sync;
    reg [31:0] sequence_number [0:2];
    // At most three unseen publications can exist, and a selection consumes
    // every pending publication. Their publication numbers differ by at most
    // two, so signed modulo-8 ordering is unambiguous even across wrap/pauses.
    reg [2:0] publication_number [0:2];
    reg [2:0] publication_counter;
    reg [8:0] border [0:2];
    reg [8:0] height [0:2];
    reg [2:0] pal_banks;
    wire [2:0] raster_banks;
    wire border_overflow;
    reg [31:0] front_sequence;
    reg [8:0] front_border;
    // One RAM edge and one grouped-selection edge. Hits tag their words with
    // the bank at the RAM edge, including the old bank for a read in flight
    // when SOF selects anew.
    assign output_rgb = group_read0[0] | group_read0[1] | group_read0[2] | group_read0[3] | group_read0[4] |
                        group_read1[0] | group_read1[1] | group_read1[2] | group_read1[3] | group_read1[4] |
                        group_read2[0] | group_read2[1] | group_read2[2] | group_read2[3] | group_read2[4];
    assign output_border = front_border;
    integer candidate;
    wire choose_valid;
    wire [1:0] choose_bank;
    function automatic newer_publication(input [2:0] latest, earlier);
        reg [2:0] distance;
        begin
            distance = latest - earlier;
            newer_publication = distance != 3'd0 && !distance[2];
        end
    endfunction
    // Compare stable bank metadata in parallel. Pending toggles select among
    // those results instead of feeding a sequence mux into another subtractor.
    // The tournament retains the original bank-zero-first tie behaviour.
    wire [2:0] pending_publications = pub_sync ^ seen;
    wire bank1_newer0 = newer_publication(publication_number[1], publication_number[0]);
    wire bank2_newer0 = newer_publication(publication_number[2], publication_number[0]);
    wire bank2_newer1 = newer_publication(publication_number[2], publication_number[1]);
    wire choose1 = pending_publications[1] && (!pending_publications[0] || bank1_newer0);
    wire choose2 = pending_publications[2] &&
        (!(|pending_publications[1:0]) || (choose1 ? bank2_newer1 : bank2_newer0));
    assign choose_valid = |pending_publications;
    assign choose_bank = choose2 ? 2'd2 : choose1 ? 2'd1 : 2'd0;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            pub_meta <= 3'd0; pub_sync <= 3'd0;
            released <= 3'd0; seen <= 3'd0;
            front_bank <= 2'd0; front_sequence <= 32'd0;
            output_raster_border <= 1'b0; output_pal <= 1'b1;
            front_border <= 9'd0; output_valid <= 1'b0; output_height <= 9'd200;
        end else begin
            pub_meta <= published; pub_sync <= pub_meta;
            if (output_sof && choose_valid) begin
                // Release the previous display and discard older pending frames.
                // The newly selected publication stays pinned until a later SOF.
                for (candidate = 0; candidate < 3; candidate = candidate + 1) begin
                    if (2'(candidate) != choose_bank) released[candidate] <= pub_sync[candidate];
                    seen[candidate] <= pub_sync[candidate];
                end
                front_bank <= choose_bank;
                front_sequence <= sequence_number[choose_bank];
                front_border <= border[choose_bank];
                output_height <= height[choose_bank];
                output_pal <= pal_banks[choose_bank];
                output_raster_border <= raster_banks[choose_bank];
                output_valid <= 1'b1;
            end
        end
    end

    wire [2:0] free_banks = ~(published ^ release_sync);
    wire bank_free = |free_banks;
    wire [1:0] free_bank = free_banks[0] ? 2'd0 : free_banks[1] ? 2'd1 : 2'd2;
    reg owned, enabled, base_valid, good_frame, previous_display;
    reg frame_pal, bottom_seen, border_enabled, border_overflow_reported;
    reg [1:0] write_bank;
    reg [8:0] frame_top, expected_row, pixel_x;
    reg [23:0] frame_base;
    reg [31:0] epoch;
    reg [16:0] pixels;
    reg line_valid;
    wire [8:0] row = native_line - frame_top;
    wire [8:0] maximum_height = frame_pal ? 9'd247 : 9'd226;
    wire active_row = native_line >= frame_top && row < maximum_height;
    wire display_rise = native_display && !previous_display;
    wire display_fall = !native_display && previous_display;
    // st_io registers this pulse with native cycle/DE. Consume the same
    // event as border capture, without an independent oscillator or phase.
    wire pixel_due = native_pixel_ce;
    wire [8:0] sample_x = display_rise ? 9'd0 : pixel_x;
    // The shortened DE interval still owns a fixed 320-pixel output row.
    // Complete its final four positions as border pixels, not stale RAM.
    wire short_padding = native_short_line && pixel_x >= 9'd316 && pixel_x < 9'd320;
    wire prepare_pixel = owned && enabled && !hold && active_row && (native_display || short_padding) &&
                       pixel_due && sample_x < 9'd320;

    reg [1:0] cache_valid;
    reg [8:0] cache_row [0:1];
    reg [31:0] cache_epoch [0:1];
    reg [8:0] cache_loss [0:1];
    reg cache_margin [0:1];
    reg [17:0] cache_start_word [0:1];
    reg [1:0] cache_window_valid;
    // A valid window ends within 512 KiB. An 18-bit subtraction that wraps
    // below its start is therefore at least its full80/81-word length and
    // fails the index bound. Invalid bases never acquire a snoop window.
    wire [17:0] snoop_offset0 = ram_write_addr - cache_start_word[0];
    wire [17:0] snoop_offset1 = ram_write_addr - cache_start_word[1];
    // The reference ST-compatibility MMU loads word zero six CPU cycles
    // after DE, then one word per four cycles. Freeze each word at its own
    // native load pulse, rather than freezing four planes at group start.
    reg [6:0] fetched_words;
    reg line_word_margin;
    reg [8:0] fetch_start_cycle;
    wire [6:0] first_unsampled_word = fetched_words + 7'(line_word_margin);
    wire before_display = native_cycle < (frame_pal ? 9'd56 : 9'd52);
    wire future_cache0 = native_line < frame_top || cache_row[0] > row ||
        (cache_row[0] == row && (before_display || snoop_offset0[6:0] >= first_unsampled_word));
    wire future_cache1 = native_line < frame_top || cache_row[1] > row ||
        (cache_row[1] == row && (before_display || snoop_offset1[6:0] >= first_unsampled_word));
    wire snoop_enabled = ram_write_committed && owned && enabled && !hold && !reset_sys;
    wire snoop_hit0 = snoop_enabled && cache_window_valid[0] && cache_epoch[0] == epoch && future_cache0 &&
        !(|snoop_offset0[17:7]) && snoop_offset0[6:0] <= (cache_margin[0] ? 7'd80 : 7'd79);
    wire snoop_hit1 = snoop_enabled && cache_window_valid[1] && cache_epoch[1] == epoch && future_cache1 &&
        !(|snoop_offset1[17:7]) && snoop_offset1[6:0] <= (cache_margin[1] ? 7'd80 : 7'd79);
    wire loss_matches = native_word_loss == cache_loss[row[0]] ||
        (cache_margin[row[0]] && native_word_loss == cache_loss[row[0]] + 9'd1);
    wire row_ready = cache_valid[row[0]] && cache_row[row[0]] == row && cache_epoch[row[0]] == epoch && loss_matches;
    wire sample_valid = display_rise ? row_ready : line_valid;
    // The next row can be prefetched before the current row's late frequency
    // switch. Its 81-word window covers either normal or one-word-short DMA.
    wire word_offset = cache_margin[row[0]] && native_word_loss == cache_loss[row[0]];
    localparam integer OUTPUT_DELAY = 28;
    localparam integer FETCH_RESOLVE_DELAY = 20;
    typedef struct packed {
        logic pixel, visible, active, pal, valid, blank, margin;
        logic [1:0] bank;
        logic [8:0] line_number, cycle_number, x, row_number;
        logic [3:0] index;
    } output_token_t;
    output_token_t output_pipe [0:OUTPUT_DELAY-1];
    output_token_t output_token;
    wire resolve_bank = output_pipe[FETCH_RESOLVE_DELAY-1].row_number[0];
    wire resolve_margin = output_pipe[FETCH_RESOLVE_DELAY-1].margin;
    wire [3:0] bit_index = 4'd15 - output_pipe[FETCH_RESOLVE_DELAY-1].x[3:0];
    // The immutable words are resolved after all four individual loads.
    // Keep the original coordinate-to-palette deadline at 28 native pulses.
    typedef enum logic [1:0] { IDLE, READ, GAP } state_t;
    state_t memory_state;
    reg job_bank;
    reg [6:0] word_index;
    reg [6:0] job_last_word;
    reg [31:0] job_epoch;

    reg [15:0] cache_words0 [0:3], cache_words1 [0:3];
    wire [4:0] cache_read_group = output_pipe[FETCH_RESOLVE_DELAY-2].x[8:4];
    wire read_margin = output_pipe[FETCH_RESOLVE_DELAY-2].margin;
    reg [4:0] serial_group;
    reg saved_margin, read_pending, copy_pending;
    reg [1:0] read_lane, copy_lane;
    wire cache_read = pixel_due || read_pending;
    wire [1:0] cache_lane = pixel_due ? 2'd0 : read_lane;
    wire [4:0] cache_group = pixel_due ? cache_read_group : serial_group;
    wire serial_margin = pixel_due ? read_margin : saved_margin;
    wire [6:0] cache_address = {cache_group, 2'b00} +
        ((cache_lane == 0 && serial_margin) ? 7'd4 : {5'd0, cache_lane});
    // Four synchronous reads and their output copies fit within the minimum
    // six system edges between native pulses. Words are already frozen at
    // this interval; index resolution and the palette deadline stay unchanged.
    always @(posedge clk_sys) begin
        if (reset_sys) begin
            serial_group <= 0; saved_margin <= 0; read_lane <= 0;
            read_pending <= 0; copy_pending <= 0; copy_lane <= 0;
        end else begin
            if (pixel_due) begin
                serial_group <= cache_read_group; saved_margin <= read_margin;
                read_lane <= 1; read_pending <= 1;
            end else if (read_pending) begin
                if (read_lane == 3) read_pending <= 0;
                else read_lane <= read_lane + 1'b1;
            end
            copy_pending <= cache_read;
            copy_lane <= cache_lane;
        end
    end
    for (genvar bank = 0; bank < 2; bank = bank + 1) begin : cache_banks
        (* ramstyle = "M10K" *) reg [15:0] words [0:80];
        reg [15:0] read_word;
        wire fill = !reset_sys && memory_state == READ && memory_ready && job_bank == 1'(bank);
        wire hit = bank == 0 ? snoop_hit0 : snoop_hit1;
        wire [6:0] snoop_word = bank == 0 ? snoop_offset0[6:0] : snoop_offset1[6:0];
        wire update_word = !fill && hit;
        wire [6:0] write_address = fill ? word_index : snoop_word;
        wire [15:0] write_data = fill ? memory_data : ram_write_data;
        always @(posedge clk_sys) begin
            if (fill || (update_word && ram_write_lanes[1])) words[write_address][15:8] <= write_data[15:8];
            if (fill || (update_word && ram_write_lanes[0])) words[write_address][7:0] <= write_data[7:0];
            if (cache_read && !reset_sys) read_word <= words[cache_address];
        end
        if (bank == 0) begin : row0
            always @(posedge clk_sys)
                if (copy_pending && !reset_sys) cache_words0[copy_lane] <= read_word;
        end else begin : row1
            always @(posedge clk_sys)
                if (copy_pending && !reset_sys) cache_words1[copy_lane] <= read_word;
        end
    end
    wire [15:0] selected_lane0 = resolve_bank ? cache_words1[0] : cache_words0[0];
    wire [15:0] selected_lane1 = resolve_bank ? cache_words1[1] : cache_words0[1];
    wire [15:0] selected_lane2 = resolve_bank ? cache_words1[2] : cache_words0[2];
    wire [15:0] selected_lane3 = resolve_bank ? cache_words1[3] : cache_words0[3];
    wire [15:0] plane0 = resolve_margin ? selected_lane1 : selected_lane0;
    wire [15:0] plane1 = resolve_margin ? selected_lane2 : selected_lane1;
    wire [15:0] plane2 = resolve_margin ? selected_lane3 : selected_lane2;
    wire [15:0] plane3 = resolve_margin ? selected_lane0 : selected_lane3;
    // With only three final loads the shifter cannot reload this group.
    // Resolve all sixteen indices to zero after the final DMA deadline;
    // a late switch may follow their raw coordinate preparation.
    wire [3:0] color_index = (!output_pipe[FETCH_RESOLVE_DELAY-1].active ||
        (native_short_line && output_pipe[FETCH_RESOLVE_DELAY-1].x >= 9'd304)) ? 4'd0 :
        {plane3[bit_index],
         plane2[bit_index], plane1[bit_index], plane0[bit_index]};
    output_token_t resolved_token;
    always @* begin
        resolved_token = output_pipe[FETCH_RESOLVE_DELAY-1];
        resolved_token.index = color_index;
    end
    reg output_tick;
    // Phase 2, palette and hold cross from the machine. Register them together so
    // lookup reads phase-2 values one edge later, on the registered native pulse;
    // the retained token cannot change before that edge.
    reg lookup_ce, lookup_hold;
    reg [143:0] lookup_palette;
    always @(posedge clk_sys) begin
        lookup_ce <= native_lookup_ce;
        lookup_hold <= hold;
        lookup_palette <= palette;
    end
    wire prepared_visible = native_line >= (frame_pal ? 9'd34 : 9'd5) &&
        native_line < (frame_pal ? 9'd310 : 9'd260) &&
        native_cycle >= (frame_pal ? 9'd8 : 9'd4) &&
        native_cycle < (frame_pal ? 9'd424 : 9'd420);
    wire lookup_due = lookup_ce && output_tick;
    wire write_pixel = lookup_due && output_token.pixel && !lookup_hold;
    wire [8:0] output_x = output_token.x;
    wire [8:0] output_row = output_token.row_number;
    wire [8:0] pixel_rgb = output_token.blank ? 9'd0 : lookup_palette[output_token.index * 9 +: 9];
    wire [8:0] sample_rgb = output_token.valid ? pixel_rgb : 9'd0;
    reg output_pending;
    always @* begin
        output_pending = output_tick && (output_token.pixel || output_token.visible);
        for (integer i = 0; i < OUTPUT_DELAY; i = i + 1)
            output_pending = output_pending || output_pipe[i].pixel || output_pipe[i].visible;
    end
    // Retain the sampled index/coordinates until the next raw CPU phase 2.
    // Actual phase-1 palette commits occur 4/5 fabric edges after the registered
    // native pulse. Phase 2 closes that admission window after they settle;
    // it precedes the next registered native pulse by one fabric edge.
    // RGB and border resolve the same retained token at that boundary.
    reg frame_write, frame_write_black;
    reg [1:0] frame_write_bank;
    reg [16:0] frame_write_address;
    reg [8:0] frame_write_rgb;
    always @(posedge clk_sys) begin
        if (reset_sys || hold || native_vblank) begin
            output_tick <= 1'b0; output_token <= '0;
            for (integer i = 0; i < OUTPUT_DELAY; i = i + 1) output_pipe[i] <= '0;
            frame_write <= 1'b0; frame_write_black <= 1'b1;
            frame_write_bank <= 2'd0; frame_write_address <= 17'd0; frame_write_rgb <= 9'd0;
        end else begin
            if (lookup_ce) output_tick <= 1'b0;
            if (pixel_due) begin
                output_tick <= 1'b1;
                output_pipe[0] <= {prepare_pixel, owned && border_enabled && prepared_visible,
                    native_display, frame_pal, sample_valid, native_blank, word_offset,
                    write_bank,
                    native_line, native_cycle, sample_x, row, 4'd0};
                for (integer i = 1; i < OUTPUT_DELAY; i = i + 1)
                    output_pipe[i] <= output_pipe[i-1];
                output_pipe[FETCH_RESOLVE_DELAY] <= resolved_token;
                output_token <= output_pipe[OUTPUT_DELAY-1];
            end
            frame_write <= write_pixel;
            frame_write_bank <= output_token.bank;
            frame_write_address <= 17'(output_row) * 17'd320 + 17'(output_x);
            frame_write_rgb <= pixel_rgb;
            frame_write_black <= !output_token.valid;
        end
    end
    generate for (genvar chunk = 0; chunk < FRAME_CHUNKS; chunk = chunk + 1) begin : frame_chunks
        reg [8:0] frame0 [0:1023];
        reg [8:0] frame1 [0:1023];
        reg [8:0] frame2 [0:1023];
        reg [8:0] word0, word1, word2;
        assign chunk_read0[chunk] = word0;
        assign chunk_read1[chunk] = word1;
        assign chunk_read2[chunk] = word2;
        // Unconditional clocked ports infer independent dual-clock M10Ks.
        // Only the owned write bank changes; the displayed bank is immutable.
        always @(posedge clk_pixel) begin
            word0 <= frame0[output_address[9:0]];
            word1 <= frame1[output_address[9:0]];
            word2 <= frame2[output_address[9:0]];
        end
        always @(posedge clk_sys) begin
            if (frame_write && !reset_sys && !lookup_hold && frame_write_address[16:10] == 7'(chunk)) begin
                case (frame_write_bank)
                    2'd0: frame0[frame_write_address[9:0]] <= frame_write_black ? 9'd0 : frame_write_rgb;
                    2'd1: frame1[frame_write_address[9:0]] <= frame_write_black ? 9'd0 : frame_write_rgb;
                    2'd2: frame2[frame_write_address[9:0]] <= frame_write_black ? 9'd0 : frame_write_rgb;
                    default: ;
                endcase
            end
        end
    end endgenerate

    wire [23:0] selected_base = base_valid ? frame_base : {screen_base[23:8], 8'd0};
    wire next_prefetch_lines = native_line >= frame_top - 9'd2 && native_line < frame_top + maximum_height - 9'd1;
    // Keep top-1 alongside frame_top so desired_row uses one subtraction
    // instead of a row subtraction followed by a serial increment.
    reg [8:0] frame_top_minus_one;
    wire [8:0] next_desired_row = native_line < frame_top ? 9'd0 : native_line - frame_top_minus_one;
    wire next_margin = next_desired_row != 9'd0;
    // Fold the leading-margin adjustment into the word-loss subtraction.
    wire [9:0] next_word_offset = {1'b0, native_word_loss} + {9'd0, next_margin};
    wire [23:0] next_start_word = {1'b0, selected_base[23:8], 7'd0} + 24'(next_desired_row) * 24'd80 -
        {14'd0, next_word_offset};
    // Compare constant limits directly rather than adding another24-bit
    // carry chain after the row/base/loss calculation. Retain the original
    // 24-bit addition's wrap cases too; all start/margin combinations match.
    wire [5:0] next_start_high = next_start_word[23:18];
    wire [17:0] next_start_low = next_start_word[17:0];
    wire start_positive = next_start_high == 6'd0;
    wire start_wrapped_negative = &next_start_high;
    wire next_address_valid = next_margin ?
        ((start_positive && next_start_low <= 18'h3ffaf) ||
         (start_wrapped_negative && next_start_low >= 18'h3ffaf)) :
        ((start_positive && next_start_low <= 18'h3ffb0) ||
         (start_wrapped_negative && next_start_low >= 18'h3ffb0));
    // Register the row and address with the line, top, mode, base and loss
    // they came from. A prefetch starts only once those inputs have held for
    // an edge, so the registered values always match the live ones and the
    // row/base/loss arithmetic has a whole cycle before the start decision.
    reg prefetch_lines, address_valid, prefetch_pal;
    reg [8:0] desired_row, prefetch_line, prefetch_top, prefetch_loss;
    reg [15:0] prefetch_base;
    reg [17:0] start_word;
    always @(posedge clk_sys) begin
        prefetch_lines <= next_prefetch_lines;
        desired_row <= next_desired_row;
        start_word <= next_start_word[17:0];
        address_valid <= next_address_valid;
        prefetch_line <= native_line;
        prefetch_top <= frame_top;
        prefetch_pal <= frame_pal;
        prefetch_loss <= native_word_loss;
        prefetch_base <= selected_base[23:8];
    end
    wire prefetch_settled = native_line == prefetch_line && frame_top == prefetch_top && frame_pal == prefetch_pal &&
                            native_word_loss == prefetch_loss && selected_base[23:8] == prefetch_base;
    wire prefetch_window = owned && !hold && prefetch_settled && prefetch_lines;
    // Row 200 must be ready before the late bottom-stop latch can open DE.
    // Do not read further border RAM until DE actually reaches that row.
    wire desired_needed = desired_row <= 9'd200 || bottom_seen;
    wire desired_bank = desired_row[0];
    wire desired_ready = cache_valid[desired_bank] && cache_row[desired_bank] == desired_row &&
                         cache_epoch[desired_bank] == epoch;
    wire desired_margin = desired_row != 9'd0;
    wire border_start = native_vblank && bank_free && !hold;
    wire missing_row = display_rise && owned && enabled && active_row && !hold && !row_ready;
    wire border_failed = border_overflow && !border_overflow_reported;
    st_native_border borders (
        .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel),
        .capture_start(border_start), .capture_active(output_token.visible && !lookup_hold),
        .capture_pal(output_token.pal), .capture_tick(lookup_due), .capture_display(output_token.active),
        .capture_bank(border_start ? free_bank : output_token.bank), .native_line(output_token.line_number),
        .native_cycle(output_token.cycle_number), .palette_zero(output_token.blank ? 9'd0 : lookup_palette[8:0]),
        .capture_overflow(border_overflow), .raster_banks(raster_banks),
        .output_bank(output_sof && choose_valid ? choose_bank : front_bank),
        .output_consume_bank(front_bank),
        .output_sof(output_sof), .output_line_start(output_border_line_start), .output_ce(output_border_ce),
        .output_row(output_border_row), .output_x(output_border_x), .output_rgb(output_raster_rgb)
    );
    integer b;
    always @(posedge clk_sys) begin
        if (reset_sys) begin
            release_meta <= 3'd0; release_sync <= 3'd0; published <= 3'd0;
            publication_counter <= 3'd0;
            owned <= 1'b0; enabled <= 1'b0; base_valid <= 1'b0;
            good_frame <= 1'b0; previous_display <= 1'b0; line_valid <= 1'b0;
            frame_pal <= 1'b1; bottom_seen <= 1'b0; border_enabled <= 1'b0; border_overflow_reported <= 1'b0; pal_banks <= 3'b111;
            write_bank <= 2'd0; frame_top <= 9'd63; frame_top_minus_one <= 9'd62; expected_row <= 9'd0;
            fetched_words <= 7'd0; line_word_margin <= 1'b0; fetch_start_cycle <= 9'd511;
            pixel_x <= 9'd0; frame_base <= 24'd0; epoch <= 32'd0;
            pixels <= 17'd0;
            cache_valid <= 2'd0; cache_window_valid <= 2'd0; memory_state <= IDLE;
            job_bank <= 1'b0; word_index <= 7'd0; job_epoch <= 32'd0;
            job_last_word <= 7'd79;
            memory_req <= 1'b0; memory_addr <= 18'd0;
            debug_frames <= 32'd0; debug_skipped <= 32'd0; debug_underruns <= 32'd0;
            for (b = 0; b < 3; b = b + 1) begin
                sequence_number[b] <= 32'd0; publication_number[b] <= 3'd0; border[b] <= 9'd0; height[b] <= 9'd200;
            end
            for (b = 0; b < 2; b = b + 1) begin
                cache_row[b] <= 9'd0; cache_epoch[b] <= 32'd0;
                cache_loss[b] <= 9'd0; cache_margin[b] <= 1'b0;
                cache_start_word[b] <= 18'd0;
            end
        end else begin
            release_meta <= released; release_sync <= release_meta;
            previous_display <= native_display;
            if (hold || native_vblank || (pixel_due && native_cycle == 9'd0)) begin
                fetched_words <= 7'd0;
                line_word_margin <= 1'b0;
                fetch_start_cycle <= 9'd511;
            end else begin
                if (display_rise) begin
                    line_word_margin <= word_offset;
                    fetch_start_cycle <= native_cycle + 9'd6;
                end
                if (pixel_due && owned && enabled && active_row &&
                    native_cycle >= fetch_start_cycle && native_cycle[1:0] == 2'd2 &&
                    fetched_words < (native_short_line ? 7'd79 : 7'd80))
                    fetched_words <= fetched_words + 7'd1;
            end
            if (missing_row || border_failed)
                debug_underruns <= debug_underruns + (missing_row ? 32'd1 : 32'd0) + (border_failed ? 32'd1 : 32'd0);
            if (border_failed) border_overflow_reported <= 1'b1;
            // capture_overflow stays set until capture_start. Clearing this
            // latch on the same start counts each overflow once.
            if (border_start) border_overflow_reported <= 1'b0;
            if (hold) begin
                owned <= 1'b0; line_valid <= 1'b0;
            end
            if (native_vblank) begin
                epoch <= epoch + 32'd1;
                owned <= bank_free && !hold;
                write_bank <= free_bank;
                frame_top <= sync_mode[1] ? 9'd63 : 9'd34;
                frame_top_minus_one <= sync_mode[1] ? 9'd62 : 9'd33;
                frame_pal <= sync_mode[1]; bottom_seen <= 1'b0; border_enabled <= resolution == 2'd0;
                enabled <= 1'b0; base_valid <= 1'b0;
                expected_row <= 9'd0; good_frame <= 1'b1;
                pixels <= 17'd0; pixel_x <= 9'd0;
                cache_valid <= 2'd0;
                if (!bank_free && resolution == 2'd0 && !hold) debug_skipped <= debug_skipped + 32'd1;
            end else begin
                if (display_rise && owned && enabled && active_row && !hold) begin
                    line_valid <= row_ready;
                    if (row >= 9'd200) bottom_seen <= 1'b1;
                    if (row != expected_row) good_frame <= 1'b0;
                    if (row == 9'd0) border[write_bank] <= palette[8:0];
                end
                if (prepare_pixel) begin
                    pixel_x <= sample_x + 9'd1;
                    pixels <= pixels + 17'd1;
                end
                if (display_fall && owned && enabled && active_row && !hold) begin
                    expected_row <= expected_row + 9'd1;
                    if (pixel_x != (native_short_line ? 9'd316 : 9'd320) || row != expected_row)
                        good_frame <= 1'b0;

                end
            end
            // Wait past the bottom-stop decision and any opened DE rows.
            // Ordinary frames still publish only their original 200 rows;
            // height travels with the immutable bank and changes only at SOF.
            if (!native_vblank && owned && enabled && !hold &&
                native_line == frame_top + maximum_height && !output_pending && !frame_write) begin
                owned <= 1'b0;
                if (good_frame && !border_overflow && expected_row == (bottom_seen ? maximum_height : 9'd200) &&
                    pixels == (bottom_seen ? (frame_pal ? 17'd79040 : 17'd72320) : 17'd64000)) begin
                    height[write_bank] <= expected_row;
                    pal_banks[write_bank] <= frame_pal;
                    sequence_number[write_bank] <= epoch;
                    publication_number[write_bank] <= publication_counter;
                    publication_counter <= publication_counter + 3'd1;
                    published[write_bank] <= !published[write_bank];
                    debug_frames <= debug_frames + 32'd1;
                end
            end
            case (memory_state)
                IDLE: if (prefetch_window && desired_needed &&
                          (base_valid ? enabled : resolution == 2'd0) && !desired_ready) begin
                    if (!base_valid) begin
                        frame_base <= selected_base; base_valid <= 1'b1;
                        enabled <= resolution == 2'd0;
                    end
                    cache_valid[desired_bank] <= 1'b0;
                    cache_row[desired_bank] <= desired_row;
                    cache_epoch[desired_bank] <= epoch;
                    cache_loss[desired_bank] <= native_word_loss;
                    cache_margin[desired_bank] <= desired_margin;
                    cache_start_word[desired_bank] <= start_word;
                    cache_window_valid[desired_bank] <= address_valid;
                    if (address_valid) begin
                        job_bank <= desired_bank; job_epoch <= epoch; word_index <= 7'd0;
                        job_last_word <= desired_margin ? 7'd80 : 7'd79;
                        memory_addr <= start_word; memory_req <= 1'b1; memory_state <= READ;
                    end
                end
                READ: if (memory_ready) begin
                    memory_req <= 1'b0; memory_state <= GAP;
                end
                GAP: begin
                    if (word_index == job_last_word || job_epoch != epoch || !owned || hold) begin
                        cache_valid[job_bank] <= word_index == job_last_word && job_epoch == epoch && owned && !hold;
                        memory_state <= IDLE;
                    end else begin
                        word_index <= word_index + 7'd1;
                        memory_addr <= memory_addr + 18'd1; memory_req <= 1'b1; memory_state <= READ;
                    end
                end
                default: begin memory_req <= 1'b0; memory_state <= IDLE; end
            endcase
        end
    end
    // Retained only for simulation inspection of the selected publication.
    wire unused_input_bits = ^{front_sequence, sample_rgb, output_token.margin, sync_mode[7:2], sync_mode[0], screen_base[7:0]};
endmodule
