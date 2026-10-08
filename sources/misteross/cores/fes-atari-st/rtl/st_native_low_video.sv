// SPDX-License-Identifier: GPL-3.0-or-later
// Ordinary 320x200 ST raster capture. RAM rows are prefetched in the system
// domain; palette selection occurs at each native pixel, not at HDMI SOF.
// Three RGB333 banks have explicit publish/release ownership. Pixel may read
// only an immutable completed bank, and changes banks only at output SOF.
// This is not a cycle-exact MMU/shifter or opened-border implementation.
module st_native_low_video (
    input wire clk_sys, clk_pixel, reset_sys, reset_pixel, hold,
    input wire native_vblank, native_display,
    input wire [8:0] native_line,
    input wire [7:0] sync_mode,
    input wire [23:0] screen_base,
    input wire [1:0] resolution,
    input wire [143:0] palette,
    output reg memory_req,
    output reg [18:1] memory_addr,
    input wire memory_ready,
    input wire [15:0] memory_data,
    input wire output_sof,
    input wire [15:0] output_address,
    output wire [8:0] output_rgb, output_border,
    output reg output_valid,
    output reg [31:0] debug_frames, debug_skipped, debug_underruns
);
    localparam [31:0] SYSTEM_HZ = 32'd52224000;
    reg [8:0] frame0 [0:63999];
    reg [8:0] frame1 [0:63999];
    reg [8:0] frame2 [0:63999];
    reg [8:0] read0, read1, read2;
    reg [1:0] front_bank;
    reg [2:0] published, released, seen;
    (* async_reg = "true" *) reg [2:0] pub_meta, pub_sync, release_meta, release_sync;
    reg [31:0] sequence_number [0:2];
    reg [8:0] border [0:2];
    reg [31:0] front_sequence;
    reg [8:0] front_border;
    assign output_rgb = front_bank == 2'd0 ? read0 : front_bank == 2'd1 ? read1 : read2;
    assign output_border = front_border;
    // Unconditional clocked reads infer independent dual-clock M10K ports.
    // Unselected banks may be written; the selected bank remains owned by pixel.
    always @(posedge clk_pixel) begin
        read0 <= frame0[output_address];
        read1 <= frame1[output_address];
        read2 <= frame2[output_address];
    end
    integer candidate;
    reg choose_valid;
    reg [1:0] choose_bank;
    reg [31:0] choose_sequence;
    always @* begin
        choose_valid = 1'b0;
        choose_bank = 2'd0;
        choose_sequence = 32'd0;
        for (integer i = 0; i < 3; i = i + 1) begin
            if (pub_sync[i] != seen[i] &&
                (!choose_valid || $signed(sequence_number[i] - choose_sequence) > 0)) begin
                choose_valid = 1'b1;
                choose_bank = 2'(i);
                choose_sequence = sequence_number[i];
            end
        end
    end
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            pub_meta <= 3'd0; pub_sync <= 3'd0;
            released <= 3'd0; seen <= 3'd0;
            front_bank <= 2'd0; front_sequence <= 32'd0;
            front_border <= 9'd0; output_valid <= 1'b0;
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
                front_sequence <= choose_sequence;
                front_border <= border[choose_bank];
                output_valid <= 1'b1;
            end
        end
    end

    wire [2:0] free_banks = ~(published ^ release_sync);
    wire bank_free = |free_banks;
    wire [1:0] free_bank = free_banks[0] ? 2'd0 : free_banks[1] ? 2'd1 : 2'd2;
    reg owned, enabled, base_valid, good_frame, previous_display;
    reg [1:0] write_bank;
    reg [8:0] frame_top, expected_row, pixel_x;
    reg [23:0] frame_base;
    reg [31:0] epoch, pixel_phase;
    reg [16:0] pixels;
    reg line_valid;
    wire [8:0] row = native_line - frame_top;
    wire active_row = native_line >= frame_top && row < 9'd200;
    wire display_rise = native_display && !previous_display;
    wire display_fall = !native_display && previous_display;
    wire [32:0] pixel_sum = {1'b0, pixel_phase} + 33'd8000000;
    wire pixel_due = display_rise || pixel_sum >= {1'b0, SYSTEM_HZ};
    wire [8:0] sample_x = display_rise ? 9'd0 : pixel_x;
    wire write_pixel = owned && enabled && !hold && active_row && native_display &&
                       pixel_due && sample_x < 9'd320;
    wire [15:0] write_address = 16'(row) * 16'd320 + 16'(sample_x);

    reg [15:0] cache0 [0:79];
    reg [15:0] cache1 [0:79];
    reg [1:0] cache_valid;
    reg [8:0] cache_row [0:1];
    reg [31:0] cache_epoch [0:1];
    wire row_ready = cache_valid[row[0]] && cache_row[row[0]] == row && cache_epoch[row[0]] == epoch;
    wire sample_valid = display_rise ? row_ready : line_valid;
    wire [6:0] group_word = {sample_x[8:4], 2'b00};
    wire [3:0] bit_index = 4'd15 - sample_x[3:0];
    wire [15:0] plane0 = row[0] ? cache1[group_word] : cache0[group_word];
    wire [15:0] plane1 = row[0] ? cache1[group_word + 7'd1] : cache0[group_word + 7'd1];
    wire [15:0] plane2 = row[0] ? cache1[group_word + 7'd2] : cache0[group_word + 7'd2];
    wire [15:0] plane3 = row[0] ? cache1[group_word + 7'd3] : cache0[group_word + 7'd3];
    wire [3:0] color_index = {plane3[bit_index], plane2[bit_index], plane1[bit_index], plane0[bit_index]};
    wire [8:0] sample_rgb = sample_valid ? palette[color_index * 9 +: 9] : 9'd0;
    always @(posedge clk_sys) begin
        if (write_pixel) begin
            case (write_bank)
                2'd0: frame0[write_address] <= sample_rgb;
                2'd1: frame1[write_address] <= sample_rgb;
                2'd2: frame2[write_address] <= sample_rgb;
                default: ;
            endcase
        end
    end

    typedef enum logic [1:0] { IDLE, READ, GAP } state_t;
    state_t memory_state;
    reg job_bank;
    reg [6:0] word_index;
    reg [31:0] job_epoch;
    wire prefetch_window = owned && !hold && native_line >= frame_top - 9'd2 &&
                           native_line < frame_top + 9'd199;
    wire [8:0] desired_row = native_line < frame_top ? 9'd0 : row + 9'd1;
    wire desired_bank = desired_row[0];
    wire desired_ready = cache_valid[desired_bank] && cache_row[desired_bank] == desired_row &&
                         cache_epoch[desired_bank] == epoch;
    wire [23:0] selected_base = base_valid ? frame_base : {screen_base[23:8], 8'd0};
    wire [23:0] start_word = {1'b0, selected_base[23:8], 7'd0} + 24'(desired_row) * 24'd80;
    wire address_valid = start_word + 24'd80 <= 24'h040000;
    integer b;
    always @(posedge clk_sys) begin
        if (reset_sys) begin
            release_meta <= 3'd0; release_sync <= 3'd0; published <= 3'd0;
            owned <= 1'b0; enabled <= 1'b0; base_valid <= 1'b0;
            good_frame <= 1'b0; previous_display <= 1'b0; line_valid <= 1'b0;
            write_bank <= 2'd0; frame_top <= 9'd63; expected_row <= 9'd0;
            pixel_x <= 9'd0; frame_base <= 24'd0; epoch <= 32'd0;
            pixel_phase <= 32'd0; pixels <= 17'd0;
            cache_valid <= 2'd0; memory_state <= IDLE;
            job_bank <= 1'b0; word_index <= 7'd0; job_epoch <= 32'd0;
            memory_req <= 1'b0; memory_addr <= 18'd0;
            debug_frames <= 32'd0; debug_skipped <= 32'd0; debug_underruns <= 32'd0;
            for (b = 0; b < 3; b = b + 1) begin
                sequence_number[b] <= 32'd0; border[b] <= 9'd0;
            end
            for (b = 0; b < 2; b = b + 1) begin
                cache_row[b] <= 9'd0; cache_epoch[b] <= 32'd0;
            end
        end else begin
            release_meta <= released; release_sync <= release_meta;
            previous_display <= native_display;
            if (hold) begin
                owned <= 1'b0; line_valid <= 1'b0;
            end
            if (native_vblank) begin
                epoch <= epoch + 32'd1;
                owned <= bank_free && !hold;
                write_bank <= free_bank;
                frame_top <= sync_mode[1] ? 9'd63 : 9'd34;
                enabled <= 1'b0; base_valid <= 1'b0;
                expected_row <= 9'd0; good_frame <= 1'b1;
                pixels <= 17'd0; pixel_x <= 9'd0; pixel_phase <= 32'd0;
                cache_valid <= 2'd0;
                if (!bank_free && resolution == 2'd0 && !hold) debug_skipped <= debug_skipped + 32'd1;
            end else begin
                if (display_rise && owned && enabled && active_row && !hold) begin
                    line_valid <= row_ready;
                    if (!row_ready) debug_underruns <= debug_underruns + 32'd1;
                    if (row != expected_row) good_frame <= 1'b0;
                    if (row == 9'd0) border[write_bank] <= palette[8:0];
                end
                if (write_pixel) begin
                    pixel_x <= sample_x + 9'd1;
                    pixels <= pixels + 17'd1;
                end
                if (display_rise || !native_display) pixel_phase <= 32'd0;
                else pixel_phase <= pixel_due ? 32'(pixel_sum - {1'b0, SYSTEM_HZ}) : pixel_sum[31:0];
                if (display_fall && owned && enabled && active_row && !hold) begin
                    expected_row <= expected_row + 9'd1;
                    if (pixel_x != 9'd320 || row != expected_row) good_frame <= 1'b0;
                    if (row == 9'd199) begin
                        owned <= 1'b0;
                        if (good_frame && expected_row == 9'd199 && pixel_x == 9'd320 && pixels == 17'd64000) begin
                            sequence_number[write_bank] <= epoch;
                            published[write_bank] <= !published[write_bank];
                            debug_frames <= debug_frames + 32'd1;
                        end
                    end
                end
            end
            case (memory_state)
                IDLE: if (prefetch_window && (base_valid ? enabled : resolution == 2'd0) && !desired_ready) begin
                    if (!base_valid) begin
                        frame_base <= selected_base; base_valid <= 1'b1;
                        enabled <= resolution == 2'd0;
                    end
                    cache_valid[desired_bank] <= 1'b0;
                    cache_row[desired_bank] <= desired_row;
                    cache_epoch[desired_bank] <= epoch;
                    if (address_valid) begin
                        job_bank <= desired_bank; job_epoch <= epoch; word_index <= 7'd0;
                        memory_addr <= start_word[17:0]; memory_req <= 1'b1; memory_state <= READ;
                    end
                end
                READ: if (memory_ready) begin
                    if (job_bank) cache1[word_index] <= memory_data;
                    else cache0[word_index] <= memory_data;
                    memory_req <= 1'b0; memory_state <= GAP;
                end
                GAP: begin
                    if (word_index == 7'd79 || job_epoch != epoch || !owned || hold) begin
                        cache_valid[job_bank] <= word_index == 7'd79 && job_epoch == epoch && owned && !hold;
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
    wire unused_input_bits = ^{front_sequence, sync_mode[7:2], sync_mode[0], screen_base[7:0]};
endmodule
