// SPDX-License-Identifier: GPL-3.0-or-later
`include "fes_video_part.vh"

// 52.224 MHz memory/configuration to 74.25 MHz fixed-raster adapter.
// Request/done toggles transfer held bundles; validated cache data is consumed
// only after completion crosses to pixel. Its bank stays unwritten during display.
// The shell supplies coordinated resets synchronized in each clock domain.
// Pixel lookup tags and cache data each cross one registered boundary before
// the renderer's original plane-capture edge. Memory inference and HDMI timing
// require separate native compilation/qualification.
module st_video_adapter #(parameter bit NATIVE_LOW_CAPTURE = 1'b1) (
    input wire clk_sys, clk_pixel,
    input wire reset_sys, reset_pixel,
    input wire hold,
    input wire native_vblank, native_display,
    input wire [8:0] native_line,
    input wire [7:0] sync_mode,
    // The original ST base has no writable low byte.
    /* verilator lint_off UNUSEDSIGNAL */
    input wire [23:0] screen_base,
    /* verilator lint_on UNUSEDSIGNAL */
    input wire [1:0] resolution,
    input wire [143:0] palette,
    output wire video_req,
    output wire [18:1] video_addr,
    input wire video_ready,
    input wire [15:0] video_rdata,
    output wire [`FES_VIDEO_PART_REQUEST_BITS-1:0] video_request,
    output reg [31:0] debug_underruns,
    output reg [31:0] debug_frame
);
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_TOTAL = 10'd750;

    // Configuration stays fixed from system capture until pixel acknowledges
    // it by initiating the next request. The snapshot is activated at SOF.
    reg config_request, config_ack;
    (* async_reg = "true" *) reg config_req_meta, config_req_sync;
    (* async_reg = "true" *) reg config_ack_meta, config_ack_sync;
    reg [169:0] config_bundle_sys;
    reg [169:0] pending_config;
    reg config_waiting, config_started, pending_valid, configured;
    reg [23:0] active_base;
    reg [1:0] active_resolution;
    reg [143:0] active_palette;
    wire config_complete = config_waiting && config_ack_sync == config_request;

    reg [15:0] cache0 [0:79];
    reg [15:0] cache1 [0:79];
    reg [1:0] job_request, job_done;
    (* async_reg = "true" *) reg [1:0] job_req_meta, job_req_sync;
    (* async_reg = "true" *) reg [1:0] job_done_meta, job_done_sync;
    reg [1:0] cache_busy, cache_valid, job_ok;
    // Pixel-owned metadata is held throughout each request/completion round trip.
    reg [23:0] job_base [0:1];
    reg [8:0] job_row [0:1];
    reg job_high [0:1];
    reg [31:0] job_frame [0:1];

    typedef enum logic [1:0] { MEMORY_IDLE, MEMORY_READ, MEMORY_GAP } memory_state_t;
    memory_state_t memory_state;
    reg memory_bank, next_bank, memory_token;
    reg [6:0] memory_index, memory_words;
    wire pending0 = job_req_sync[0] != job_done[0];
    wire pending1 = job_req_sync[1] != job_done[1];
    wire selected_bank = pending0 && pending1 ? next_bank : pending1;
    wire [23:0] selected_start = {1'b0, job_base[selected_bank][23:8], 7'd0} +
        (job_high[selected_bank] ? 24'(job_row[selected_bank]) * 24'd40 :
                                  24'(job_row[selected_bank]) * 24'd80);
    wire [6:0] selected_words = job_high[selected_bank] ? 7'd40 : 7'd80;
    wire selected_valid = selected_start + 24'(selected_words) <= 24'h040000;

    reg legacy_req;
    reg [18:1] legacy_addr;
    wire legacy_ready;
    always @(posedge clk_sys) begin
        if (reset_sys) begin
            config_req_meta <= 1'b0;
            config_req_sync <= 1'b0;
            config_ack <= 1'b0;
            config_bundle_sys <= 170'd0;
            job_req_meta <= 2'd0;
            job_req_sync <= 2'd0;
            job_done <= 2'd0;
            job_ok <= 2'd0;
            memory_state <= MEMORY_IDLE;
            memory_bank <= 1'b0;
            next_bank <= 1'b0;
            memory_token <= 1'b0;
            memory_index <= 7'd0;
            memory_words <= 7'd0;
            legacy_req <= 1'b0;
            legacy_addr <= 18'd0;
        end else begin
            config_req_meta <= config_request;
            config_req_sync <= config_req_meta;
            job_req_meta <= job_request;
            job_req_sync <= job_req_meta;
            if (config_req_sync != config_ack) begin
                config_bundle_sys <= {{screen_base[23:8], 8'd0}, resolution, palette};
                config_ack <= config_req_sync;
            end
            case (memory_state)
                MEMORY_IDLE: if (pending0 || pending1) begin
                    memory_bank <= selected_bank;
                    memory_token <= job_req_sync[selected_bank];
                    memory_index <= 7'd0;
                    memory_words <= selected_words;
                    next_bank <= !selected_bank;
                    if (selected_valid) begin
                        legacy_addr <= selected_start[17:0];
                        legacy_req <= 1'b1;
                        memory_state <= MEMORY_READ;
                    end else begin
                        // Reject the entire line, never truncate a bad base into RAM.
                        job_ok[selected_bank] <= 1'b0;
                        job_done[selected_bank] <= job_req_sync[selected_bank];
                    end
                end
                MEMORY_READ: if (legacy_ready) begin
                    if (memory_bank) cache1[memory_index] <= video_rdata;
                    else cache0[memory_index] <= video_rdata;
                    legacy_req <= 1'b0;
                    memory_state <= MEMORY_GAP;
                end
                MEMORY_GAP: begin
                    // st_memory rearms a held request only after req is low.
                    if (memory_index == memory_words - 7'd1) begin
                        job_ok[memory_bank] <= 1'b1;
                        job_done[memory_bank] <= memory_token;
                        memory_state <= MEMORY_IDLE;
                    end else begin
                        memory_index <= memory_index + 7'd1;
                        legacy_addr <= legacy_addr + 18'd1;
                        legacy_req <= 1'b1;
                        memory_state <= MEMORY_READ;
                    end
                end
                default: begin
                    legacy_req <= 1'b0;
                    memory_state <= MEMORY_IDLE;
                end
            endcase
        end
    end

    // Raster counters use the same reset/step rule as the existing renderer.
    // Keeping its public interface unchanged also preserves its unit simulation.
    reg [10:0] horizontal;
    reg [9:0] vertical;
    (* async_reg = "true" *) reg hold_meta, hold_sync;
    reg line_available;
    wire high_resolution = active_resolution == 2'd2;
    wire mode_valid = active_resolution != 2'd3;
    wire [9:0] image_top = high_resolution ? 10'd160 : 10'd60;
    wire [9:0] image_height = high_resolution ? 10'd400 : 10'd600;
    wire [8:0] native_height = high_resolution ? 9'd400 : 9'd200;
    wire image_line = mode_valid && vertical >= image_top && vertical < image_top + image_height;
    // The renderer tracks native row/repetition at EOL. Sharing its coordinates
    // removes color scaling division from both lookup and scheduling paths.
    wire [8:0] current_row, next_row;
    wire color_scheduling = vertical >= 10'd52 && vertical < 10'd660;
    wire mono_scheduling = vertical >= 10'd152 && vertical < 10'd560;
    wire scheduling = configured && mode_valid && (!NATIVE_LOW_CAPTURE || active_resolution != 2'd0) &&
        (high_resolution ? mono_scheduling : color_scheduling);
    wire [8:0] desired_row0 = current_row[0] ? current_row + 9'd1 : current_row;
    wire [8:0] desired_row1 = current_row[0] ? current_row : current_row + 9'd1;
    wire [8:0] desired_rows [0:1];
    assign desired_rows[0] = desired_row0;
    assign desired_rows[1] = desired_row1;

    wire first_lookup = active_resolution == 2'd0 ? horizontal == 11'd1644 :
        high_resolution ? horizontal == 11'd1647 : horizontal == 11'd1646;
    wire [9:0] next_vertical = vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 10'd1;
    wire next_image_line = mode_valid && next_vertical >= image_top &&
                           next_vertical < image_top + image_height;
    wire next_row_ready = cache_valid[next_row[0]] && !cache_busy[next_row[0]] &&
        job_frame[next_row[0]] == debug_frame && job_row[next_row[0]] == next_row;
    integer bank;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            horizontal <= 11'd0;
            vertical <= 10'd0;
            hold_meta <= 1'b0;
            hold_sync <= 1'b0;
            config_ack_meta <= 1'b0;
            config_ack_sync <= 1'b0;
            config_request <= 1'b0;
            config_started <= 1'b0;
            config_waiting <= 1'b0;
            pending_config <= 170'd0;
            pending_valid <= 1'b0;
            configured <= 1'b0;
            active_base <= 24'd0;
            active_resolution <= 2'd0;
            active_palette <= 144'd0;
            job_done_meta <= 2'd0;
            job_done_sync <= 2'd0;
            job_request <= 2'd0;
            cache_busy <= 2'd0;
            cache_valid <= 2'd0;
            line_available <= 1'b0;
            debug_underruns <= 32'd0;
            debug_frame <= 32'd0;
            for (bank = 0; bank < 2; bank = bank + 1) begin
                job_base[bank] <= 24'd0;
                job_row[bank] <= 9'd0;
                job_high[bank] <= 1'b0;
                job_frame[bank] <= 32'd0;
            end
        end else begin
            hold_meta <= hold;
            hold_sync <= hold_meta;
            config_ack_meta <= config_ack;
            config_ack_sync <= config_ack_meta;
            job_done_meta <= job_done;
            job_done_sync <= job_done_meta;
            if (!config_started || (horizontal == 11'd0 && vertical == 10'd720)) begin
                if (!config_waiting) begin
                    config_request <= !config_request;
                    config_waiting <= 1'b1;
                    config_started <= 1'b1;
                end
            end
            if (config_complete) begin
                pending_config <= config_bundle_sys;
                pending_valid <= 1'b1;
                config_waiting <= 1'b0;
            end
            for (bank = 0; bank < 2; bank = bank + 1) begin
                if (cache_busy[bank] && job_done_sync[bank] == job_request[bank]) begin
                    cache_busy[bank] <= 1'b0;
                    cache_valid[bank] <= job_ok[bank] && job_frame[bank] == debug_frame;
                end
                if (scheduling && !cache_busy[bank] && desired_rows[bank] < native_height &&
                    (!cache_valid[bank] || job_frame[bank] != debug_frame ||
                     job_row[bank] != desired_rows[bank])) begin
                    job_base[bank] <= active_base;
                    job_row[bank] <= desired_rows[bank];
                    job_high[bank] <= high_resolution;
                    job_frame[bank] <= debug_frame;
                    job_request[bank] <= !job_request[bank];
                    cache_busy[bank] <= 1'b1;
                    cache_valid[bank] <= 1'b0;
                end
            end
            // Decide at the first registered cache lookup for group0. Both
            // decisions use the bank's readiness from before this clock edge.
            // A completion later in this line cannot expose a partial picture.
            if (first_lookup) begin
                line_available <= next_image_line && next_row_ready;
                if (configured && next_image_line && !next_row_ready &&
                    (!NATIVE_LOW_CAPTURE || active_resolution != 2'd0))
                    debug_underruns <= debug_underruns + 32'd1;
            end
            if (horizontal == H_TOTAL - 11'd1) begin
                horizontal <= 11'd0;
                vertical <= next_vertical;
                if (vertical == V_TOTAL - 10'd1) begin
                    debug_frame <= debug_frame + 32'd1;
                    cache_valid <= 2'd0;
                    if (pending_valid || config_complete) begin
                        {active_base, active_resolution, active_palette} <=
                            config_complete ? config_bundle_sys : pending_config;
                        configured <= 1'b1;
                        pending_valid <= 1'b0;
                    end
                end
            end else horizontal <= horizontal + 11'd1;
        end
    end

    // Use the renderer's native coordinates instead of reversing its physical
    // address through wide subtraction, division and modulo in the pixel path.
    wire renderer_fetch_valid;
    wire [8:0] renderer_row;
    wire [6:0] renderer_column;
    reg lookup_valid, lookup_bank;
    // Retained by the focused simulation's registered-word/tag inspection.
    /* verilator lint_off UNUSEDSIGNAL */
    reg [6:0] lookup_column;
    /* verilator lint_on UNUSEDSIGNAL */
    reg [15:0] renderer_data;
    // Unreset, unconditional per-bank read registers allow each pixel read
    // clock to be absorbed into its own synchronous RAM port.
    // Tags and bank data are captured together; the existing second-edge
    // result register selects the previous edge's validated bank.
    reg [15:0] cache0_read, cache1_read;
    always @(posedge clk_pixel) begin
        cache0_read <= cache0[renderer_column];
        cache1_read <= cache1[renderer_column];
    end
    // Capture the column/bank and validated ownership together. The final
    // current-row lookup completes well before EOL can recycle its bank;
    // lookups in the last blanking clocks use the retained next-row bank.
    always @(posedge clk_pixel) begin
        if (reset_pixel || (horizontal == H_TOTAL - 1'b1 && vertical == V_TOTAL - 1'b1)) begin
            lookup_valid <= 1'b0;
            lookup_bank <= 1'b0;
            lookup_column <= 7'd0;
            renderer_data <= 16'd0;
        end else begin
            lookup_valid <= renderer_fetch_valid && cache_valid[renderer_row[0]] &&
                !cache_busy[renderer_row[0]] && job_frame[renderer_row[0]] == debug_frame &&
                job_row[renderer_row[0]] == renderer_row;
            lookup_bank <= renderer_row[0];
            lookup_column <= renderer_column;
            renderer_data <= !lookup_valid ? 16'd0 :
                lookup_bank ? cache1_read : cache0_read;
        end
    end
    wire [31:0] source_request;
    st_video #(.CACHED_WORD_PORT(1'b1)) renderer (
        .clk(clk_pixel), .reset(reset_pixel), .hold(1'b0),
        .screen_base(active_base), .resolution(active_resolution), .palette(active_palette),
        /* verilator lint_off PINCONNECTEMPTY */
        .mem_addr(),
        /* verilator lint_on PINCONNECTEMPTY */
        .mem_data(renderer_data), .fetch_valid(renderer_fetch_valid),
        .fetch_row(renderer_row), .fetch_column(renderer_column),
        .raster_row(current_row), .raster_next_row(next_row), .video_request(source_request)
    );
    wire native_req, native_ready, native_valid;
    wire [18:1] native_addr;
    wire [8:0] native_rgb, native_border, native_height_captured;
    wire native_bottom = native_height_captured != 9'd200;
    wire [9:0] native_top = !native_bottom ? 10'd60 :
                          native_height_captured == 9'd247 ? 10'd113 : 10'd134;
    wire [9:0] native_end = !native_bottom ? 10'd660 :
                          native_height_captured == 9'd247 ? 10'd607 : 10'd586;
    wire native_image_line = vertical >= native_top && vertical < native_end;
    // Forecast two pixels ahead: address register, then synchronous RGB read.
    // Low output rows repeat three times. Advance the row base in blanking,
    // before the next-line lookahead, without a vertical-coordinate multiply.
    // Units of 64 pixels keep the address addition confined to its upper bits.
    reg [10:0] rgb_row_base;
    reg [1:0] rgb_row_repeat;
    wire [8:0] rgb_x = horizontal < 11'd1278 ? 9'((horizontal + 11'd2) >> 2) : 9'd0;
    wire [10:0] rgb_address_upper = rgb_row_base + {8'd0, rgb_x[8:6]};
    reg [16:0] rgb_ram_address;
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            rgb_row_base <= 11'd0; rgb_row_repeat <= 2'd0;
            rgb_ram_address <= 17'd0;
        end else begin
            rgb_ram_address <= {rgb_address_upper, rgb_x[5:0]};
            if (horizontal == H_TOTAL - 11'd3) begin
                if (vertical < native_top || vertical >= native_end - 10'd1) begin
                    rgb_row_base <= 11'd0; rgb_row_repeat <= 2'd0;
                end else if (rgb_row_repeat == (native_bottom ? 2'd1 : 2'd2)) begin
                    rgb_row_base <= rgb_row_base + 11'd5;
                    rgb_row_repeat <= 2'd0;
                end else rgb_row_repeat <= rgb_row_repeat + 2'd1;
            end
        end
    end
    generate if (NATIVE_LOW_CAPTURE) begin : native_capture
        /* verilator lint_off PINCONNECTEMPTY */
        st_native_low_video capture (
            .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel),
            .hold(hold), .native_vblank(native_vblank), .native_display(native_display),
            .native_line(native_line), .sync_mode(sync_mode), .screen_base(screen_base),
            .resolution(resolution), .palette(palette),
            .memory_req(native_req), .memory_addr(native_addr), .memory_ready(native_ready), .memory_data(video_rdata),
            .output_sof(horizontal == H_TOTAL - 11'd1 && vertical == V_TOTAL - 10'd1),
            .output_address(rgb_ram_address), .output_rgb(native_rgb), .output_border(native_border),
            .output_valid(native_valid), .output_height(native_height_captured), .debug_frames(), .debug_skipped(), .debug_underruns()
        );
        /* verilator lint_on PINCONNECTEMPTY */
        reg bus_active, bus_native, bus_gap, prefer_native;
        wire select_native = native_req && (!legacy_req || prefer_native);
        assign video_req = bus_active && (bus_native ? native_req : legacy_req);
        assign video_addr = bus_native ? native_addr : legacy_addr;
        assign native_ready = bus_active && bus_native && video_ready;
        assign legacy_ready = bus_active && !bus_native && video_ready;
        always @(posedge clk_sys) begin
            if (reset_sys) begin
                bus_active <= 1'b0; bus_native <= 1'b0;
                bus_gap <= 1'b0; prefer_native <= 1'b1;
            end else if (bus_gap) bus_gap <= 1'b0;
            else if (!bus_active && (native_req || legacy_req)) begin
                bus_native <= select_native; bus_active <= 1'b1;
            end else if (bus_active && video_ready) begin
                bus_active <= 1'b0; bus_gap <= 1'b1; prefer_native <= !bus_native;
            end
        end
    end else begin : cached_renderer_test
        // The focused cache/renderer test isolates the retained indexed path.
        wire unused_native_inputs = ^{native_vblank, native_display, native_line, sync_mode,
                                      rgb_ram_address, native_req, native_ready, native_addr};
        assign video_req = legacy_req;
        assign video_addr = legacy_addr;
        assign legacy_ready = video_ready;
        assign native_req = 1'b0; assign native_addr = 18'd0; assign native_ready = 1'b0;
        assign native_rgb = 9'd0; assign native_border = 9'd0; assign native_valid = 1'b0;
        assign native_height_captured = 9'd200;
    end endgenerate
    function automatic [23:0] expand_rgb(input [8:0] c);
        expand_rgb = {c[8:6], c[8:6], c[8:7], c[5:3], c[5:3], c[5:4], c[2:0], c[2:0], c[2:1]};
    endfunction
    wire native_low = NATIVE_LOW_CAPTURE && active_resolution == 2'd0;
    wire [23:0] captured_rgb = !source_request[`FES_VIDEO_PART_REQUEST_DE_BIT] ? 24'd0 :
        expand_rgb(native_image_line ? native_rgb : native_border);
    wire [31:0] selected_request = native_low ? {source_request[31:24], captured_rgb} : source_request;
    wire mute = hold_sync || !configured || (native_low ? !native_valid :
        image_line && horizontal < 11'd1280 && !line_available);
    assign video_request = mute ?
        ((selected_request & 32'hff000000) | (32'd1 << `FES_VIDEO_PART_REQUEST_HOLD_BIT)) : selected_request;
endmodule
