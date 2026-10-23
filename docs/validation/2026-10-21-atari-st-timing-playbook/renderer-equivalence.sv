// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_video_part.vh"

// Atari ST display-memory slice, scaled before the existing RGB888 socket.
// Layout/palette: Atari Corp., Engineering Hardware Specification, 7 Jan 1986,
// section 3. The word port is combinational and shares this clock in simulation;
// a hardware shell must supply coherent configuration and a RAM/CDC adapter.
// This is fixed 720p scanout, not the original GLUE/MMU/Shifter bus timing.
module st_video_gold #(
    // The default retains the original combinational physical word port.
    // A cache adapter may validate whole lines in its memory domain and return
    // words two clocks after these lookup coordinates. Configuration in this
    // mode must remain coherent for the complete raster frame.
    parameter bit CACHED_WORD_PORT = 1'b0
) (
    input  wire         clk,
    input  wire         reset,
    input  wire         hold,
    // The original ST lacks the later STe low-byte base register.
    /* verilator lint_off UNUSEDSIGNAL */
    input  wire [23:0]  screen_base,
    /* verilator lint_on UNUSEDSIGNAL */
    input  wire [1:0]   resolution,
    // Entry n occupies n*9 +: 9, packed {red[2:0],green[2:0],blue[2:0]}.
    input  wire [143:0] palette,
    output wire [18:1]  mem_addr,
    input  wire [15:0]  mem_data,
    // Optional line-cache lookup coordinates for the same word-port cycle.
    // Keeping them explicit avoids decoding the wide physical address back
    // into a row and column in a pixel-domain cache adapter.
    output wire         fetch_valid,
    output wire [8:0]   fetch_row,
    output wire [6:0]   fetch_column,
    output wire [8:0]   raster_row,
    output wire [8:0]   raster_next_row,
    output wire [`FES_VIDEO_PART_REQUEST_BITS-1:0] video_request
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_ACTIVE = 10'd720;
    localparam [9:0] V_TOTAL = 10'd750;

    reg [10:0] horizontal = 11'd0;
    reg [9:0] vertical = 10'd0;
    reg [15:0] staging0 = 16'd0, staging1 = 16'd0, staging2 = 16'd0;
    reg [15:0] plane0 = 16'd0, plane1 = 16'd0;
    reg [15:0] plane2 = 16'd0, plane3 = 16'd0;
    reg picture_valid = 1'b0;

    wire low_resolution = resolution == 2'd0;
    wire high_resolution = resolution == 2'd2;
    wire mode_valid = resolution != 2'd3;
    // Low 4x3 and medium 2x3 fill 1280x600; high 2x1 fills 1280x400.
    // This first slice uses nearest-neighbor integer scaling, not aspect correction.
    // Select complete constant windows after comparison; avoid putting the
    // mode-dependent top/height arithmetic on the RGB output path.
    wire picture_line = high_resolution ?
        (vertical >= 10'd160 && vertical < 10'd560) :
        (vertical >= 10'd60 && vertical < 10'd660);
    wire in_picture = horizontal < H_ACTIVE && picture_line && mode_valid;
    // Lookup both colour modes before selecting the completed pixel. Keep
    // all four plane bits in each path, including during mode transitions.
    wire [3:0] low_bit_index = 4'd15 - horizontal[5:2];
    wire [3:0] fast_bit_index = 4'd15 - horizontal[4:1];

    // Capture each plane immediately before its 16-pixel group. Group zero
    // uses the preceding line's blanking; staging preserves its final pixels.
    wire capture_enable, capture_commit;
    wire [1:0] capture_stage;
    wire address_valid;
    generate
        if (CACHED_WORD_PORT) begin : cached_coordinates
            reg [8:0] native_row;
            reg [1:0] row_repeat;
            wire color_line = vertical >= 10'd60 && vertical < 10'd660;
            wire mono_line = vertical >= 10'd160 && vertical < 10'd560;
            // The next-line windows are the same bounds shifted back one.
            // Frame/10-bit wrap both produce zero, outside either window.
            wire next_color_line = vertical >= 10'd59 && vertical < 10'd659;
            wire next_mono_line = vertical >= 10'd159 && vertical < 10'd559;
            // These are the original h+planes capture cycles, expressed before
            // addition/wrap comparisons. Select the mode after each constant
            // window; cache lookup still occurs exactly two clocks earlier.
            wire low_capture =
                (color_line && horizontal < 11'd1276 && horizontal[5:0] >= 6'd60) ||
                (next_color_line && horizontal >= 11'd1646);
            wire medium_capture =
                (color_line && horizontal < 11'd1278 && horizontal[4:0] >= 5'd30) ||
                (next_color_line && horizontal >= 11'd1648);
            wire mono_capture =
                (mono_line && horizontal < 11'd1279 && horizontal[4:0] == 5'd31) ||
                (next_mono_line && horizontal == 11'd1649);
            assign capture_enable = low_resolution ? low_capture :
                high_resolution ? mono_capture : resolution == 2'd1 && medium_capture;
            // Low group zero starts at h=1646, two phases after h[1:0].
            // Medium uses two phases and mono always captures plane zero.
            assign capture_stage = low_resolution ?
                (horizontal >= 11'd1646 ? horizontal[1:0] ^ 2'd2 : horizontal[1:0]) :
                high_resolution ? 2'd0 : {1'b0, horizontal[0]};
            assign capture_commit = horizontal == 11'd1649 ||
                (low_resolution ? &horizontal[5:0] : &horizontal[4:0]);
            wire cached_next_image_line = mode_valid &&
                (high_resolution ? next_mono_line : next_color_line);
            wire before_image = high_resolution ? vertical < 10'd160 : vertical < 10'd60;
            wire advance_row = high_resolution || row_repeat == 2'd2;
            // EOL clears native_row outside the image, including the frame
            // boundary where coherent configuration can change. The row is
            // already zero there, so no current-line geometry mask is needed.
            assign raster_row = native_row;
            assign raster_next_row = !cached_next_image_line ? 9'd0 :
                before_image ? 9'd0 : native_row + {8'd0, advance_row};
            always @(posedge clk) begin
                if (reset) begin
                    native_row <= 9'd0;
                    row_repeat <= 2'd0;
                end else if (horizontal == H_TOTAL - 1'b1) begin
                    native_row <= raster_next_row;
                    row_repeat <= !cached_next_image_line || before_image || advance_row ?
                        2'd0 : row_repeat + 2'd1;
                end
            end
            // Complete-line bounds, ownership and readiness are checked by the
            // cache adapter. No physical address is reconstructed in this path.
            assign address_valid = 1'b1;
            assign mem_addr = 18'd0;
            // Fixed lookaheads are evaluated in parallel; resolution selects
            // their results after the arithmetic. The cache captures each word
            // two clocks before the unchanged plane-capture edge.
            wire low_wrap = horizontal >= 11'd1644;
            wire medium_wrap = horizontal >= 11'd1646;
            wire mono_wrap = horizontal >= 11'd1647;
            wire [10:0] low_ahead = horizontal + 11'd6;
            wire [10:0] medium_ahead = horizontal + 11'd4;
            wire [10:0] mono_ahead = horizontal + 11'd3;
            wire [10:0] low_x = low_wrap ? horizontal - 11'd1644 : low_ahead;
            wire [10:0] medium_x = medium_wrap ? horizontal - 11'd1646 : medium_ahead;
            wire [10:0] mono_x = mono_wrap ? horizontal - 11'd1647 : mono_ahead;
            // The phase windows below supply these low bits without addition.
            wire unused_cached_phase = ^{low_x[5:2], medium_x[4:1], mono_x[4:0]};
            // These constant windows are the corresponding x<1280 and plane
            // phase tests, expressed before the lookahead carry chain. Group0
            // uses the next line during the last blanking clocks.
            wire low_fetching =
                (color_line && horizontal < 11'd1274 &&
                 horizontal[5:0] >= 6'd58 && horizontal[5:0] < 6'd62) ||
                (next_color_line && horizontal >= 11'd1644 && horizontal < 11'd1648);
            wire medium_fetching =
                (color_line && horizontal < 11'd1276 && horizontal[4:1] == 4'd14) ||
                (next_color_line && horizontal >= 11'd1646 && horizontal < 11'd1648);
            wire mono_fetching =
                (mono_line && horizontal < 11'd1277 && horizontal[4:0] == 5'd29) ||
                (next_mono_line && horizontal == 11'd1647);
            assign fetch_valid = low_resolution ? low_fetching :
                high_resolution ? mono_fetching : resolution == 2'd1 && medium_fetching;
            wire read_wrap = low_resolution ? low_wrap : high_resolution ? mono_wrap : medium_wrap;
            assign fetch_row = read_wrap ? raster_next_row : raster_row;
            assign fetch_column = low_resolution ? {low_x[10:6], low_x[1:0]} :
                high_resolution ? {1'b0, mono_x[10:5]} : {medium_x[10:5], medium_x[0]};
        end else begin : physical_coordinates
            // Retain the combinational physical word-port address and capture
            // arithmetic, including its invalid-address behavior.
            wire [9:0] image_top = high_resolution ? 10'd160 : 10'd60;
            wire [9:0] image_height = high_resolution ? 10'd400 : 10'd600;
            wire [9:0] next_vertical = vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 10'd1;
            wire [2:0] planes = low_resolution ? 3'd4 : high_resolution ? 3'd1 : 3'd2;
            wire [11:0] ahead = {1'b0, horizontal} + {9'd0, planes};
            wire wrap_line = ahead >= {1'b0, H_TOTAL};
            wire [10:0] fetch_x = wrap_line ? ahead[10:0] - H_TOTAL : ahead[10:0];
            wire [9:0] fetch_y = wrap_line ?
                (vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 1'b1) : vertical;
            wire [5:0] group_phase = low_resolution ? fetch_x[5:0] : {1'b0, fetch_x[4:0]};
            wire fetching = mode_valid && fetch_x < H_ACTIVE &&
                fetch_y >= image_top && fetch_y < image_top + image_height &&
                group_phase < {3'd0, planes};
            assign capture_enable = fetching;
            assign capture_stage = group_phase[1:0];
            assign capture_commit = group_phase == {3'd0, planes} - 6'd1;
            wire image_line = mode_valid && vertical >= image_top && vertical < image_top + image_height;
            wire next_image_line = mode_valid && next_vertical >= image_top &&
                                   next_vertical < image_top + image_height;
            wire [5:0] group_number = low_resolution ? {1'b0, fetch_x[10:6]} : fetch_x[10:5];
            wire [9:0] image_y = fetch_y - image_top;
            wire [9:0] native_y = high_resolution ? image_y : image_y / 10'd3;
            assign fetch_valid = fetching && address_valid;
            assign fetch_row = native_y[8:0];
            assign fetch_column = low_resolution ? {group_number[4:0], group_phase[1:0]} :
                high_resolution ? {1'b0, group_number} : {group_number, group_phase[0]};
            wire [9:0] raster_y = vertical - image_top;
            wire [9:0] next_raster_y = next_vertical - image_top;
            assign raster_row = !image_line ? 9'd0 : high_resolution ?
                raster_y[8:0] : 9'(raster_y / 10'd3);
            assign raster_next_row = !next_image_line ? 9'd0 : high_resolution ?
                next_raster_y[8:0] : 9'(next_raster_y / 10'd3);
            wire [6:0] stride_words = high_resolution ? 7'd40 : 7'd80;
            wire [16:0] line_offset = {7'd0, native_y} * {10'd0, stride_words};
            wire [8:0] group_offset = {3'd0, group_number} * {6'd0, planes};
            // ST base registers only provide bits23:8. Keep the addition wide
            // enough to reject bad scanout without wrapping into 512KiB RAM.
            wire [23:0] address_word = {1'b0, screen_base[23:8], 7'd0} +
                {7'd0, line_offset} + {15'd0, group_offset} + {18'd0, group_phase};
            assign address_valid = address_word < 24'h040000;
            assign mem_addr = fetching && address_valid ? address_word[17:0] : 18'd0;
        end
    endgenerate
    wire [15:0] fetched_word = address_valid ? mem_data : 16'd0;

    always @(posedge clk) begin
        if (reset) begin
            horizontal <= 11'd0;
            vertical <= 10'd0;
            staging0 <= 16'd0;
            staging1 <= 16'd0;
            staging2 <= 16'd0;
            plane0 <= 16'd0;
            plane1 <= 16'd0;
            plane2 <= 16'd0;
            plane3 <= 16'd0;
            picture_valid <= 1'b0;
        end else begin
            if (horizontal == H_TOTAL - 1'b1) begin
                horizontal <= 11'd0;
                vertical <= vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 1'b1;
            end else begin
                horizontal <= horizontal + 1'b1;
            end
            if (capture_enable) begin
                case (capture_stage)
                    2'd0: staging0 <= fetched_word;
                    2'd1: staging1 <= fetched_word;
                    2'd2: staging2 <= fetched_word;
                    default: begin end
                endcase
                if (capture_commit) begin
                    plane0 <= high_resolution ? fetched_word : staging0;
                    plane1 <= low_resolution ? staging1 : high_resolution ? 16'd0 : fetched_word;
                    plane2 <= low_resolution ? staging2 : 16'd0;
                    plane3 <= low_resolution ? fetched_word : 16'd0;
                    picture_valid <= address_valid;
                end
            end
        end
    end

    wire [3:0] low_color_index = {plane3[low_bit_index], plane2[low_bit_index],
                                  plane1[low_bit_index], plane0[low_bit_index]};
    wire [3:0] fast_color_index = {plane3[fast_bit_index], plane2[fast_bit_index],
                                   plane1[fast_bit_index], plane0[fast_bit_index]};
    wire [8:0] low_color = palette[low_color_index * 9 +: 9];
    wire [8:0] fast_color = palette[fast_color_index * 9 +: 9];
    wire [8:0] border = palette[8:0];
    function automatic [7:0] expand3(input [2:0] intensity);
        expand3 = {intensity, intensity, intensity[2:1]};
    endfunction
    wire mono_white = plane0[fast_bit_index] ^ palette[0];
    wire [23:0] low_rgb =
        {expand3(low_color[8:6]), expand3(low_color[5:3]), expand3(low_color[2:0])};
    wire [23:0] fast_rgb =
        {expand3(fast_color[8:6]), expand3(fast_color[5:3]), expand3(fast_color[2:0])};
    wire [23:0] picture_rgb = high_resolution ? {24{mono_white}} :
        low_resolution ? low_rgb : fast_rgb;
    wire [23:0] border_rgb = high_resolution ? 24'd0 :
        {expand3(border[8:6]), expand3(border[5:3]), expand3(border[2:0])};
    wire active_display = horizontal < H_ACTIVE && vertical < V_ACTIVE;
    wire [23:0] rgb = !active_display || !mode_valid ? 24'd0 :
        in_picture ? (picture_valid ? picture_rgb : 24'd0) : border_rgb;
    assign video_request = {
        1'b0, hold || reset,
        horizontal == H_TOTAL - 1'b1,
        horizontal == 11'd0 && vertical == 10'd0,
        1'b1,
        vertical >= 10'd725 && vertical < 10'd730,
        horizontal >= 11'd1390 && horizontal < 11'd1430,
        active_display, rgb
    };
endmodule

// SPDX-License-Identifier: GPL-2.0-or-later

// Atari ST display-memory slice, scaled before the existing RGB888 socket.
// Layout/palette: Atari Corp., Engineering Hardware Specification, 7 Jan 1986,
// section 3. The word port is combinational and shares this clock in simulation;
// a hardware shell must supply coherent configuration and a RAM/CDC adapter.
// This is fixed 720p scanout, not the original GLUE/MMU/Shifter bus timing.
module st_video_gate #(
    // The default retains the original combinational physical word port.
    // A cache adapter may validate whole lines in its memory domain and return
    // words two clocks after these lookup coordinates. Configuration in this
    // mode must remain coherent for the complete raster frame.
    parameter bit CACHED_WORD_PORT = 1'b0
) (
    input  wire         clk,
    input  wire         reset,
    input  wire         hold,
    // The original ST lacks the later STe low-byte base register.
    /* verilator lint_off UNUSEDSIGNAL */
    input  wire [23:0]  screen_base,
    /* verilator lint_on UNUSEDSIGNAL */
    input  wire [1:0]   resolution,
    // Entry n occupies n*9 +: 9, packed {red[2:0],green[2:0],blue[2:0]}.
    input  wire [143:0] palette,
    output wire [18:1]  mem_addr,
    input  wire [15:0]  mem_data,
    // Optional line-cache lookup coordinates for the same word-port cycle.
    // Keeping them explicit avoids decoding the wide physical address back
    // into a row and column in a pixel-domain cache adapter.
    output wire         fetch_valid,
    output wire [8:0]   fetch_row,
    output wire [6:0]   fetch_column,
    output wire [8:0]   raster_row,
    output wire [8:0]   raster_next_row,
    output wire [`FES_VIDEO_PART_REQUEST_BITS-1:0] video_request
);
    localparam [10:0] H_ACTIVE = 11'd1280;
    localparam [10:0] H_TOTAL = 11'd1650;
    localparam [9:0] V_ACTIVE = 10'd720;
    localparam [9:0] V_TOTAL = 10'd750;

    reg [10:0] horizontal = 11'd0;
    reg [9:0] vertical = 10'd0;
    reg [15:0] staging0 = 16'd0, staging1 = 16'd0, staging2 = 16'd0;
    reg [15:0] plane0 = 16'd0, plane1 = 16'd0;
    reg [15:0] plane2 = 16'd0, plane3 = 16'd0;
    reg picture_valid = 1'b0;

    // Next-state counters. Registers loaded from them always equal the same
    // function of the current counters, without its compares in their users.
    wire [10:0] following_horizontal = reset || horizontal == H_TOTAL - 1'b1 ?
        11'd0 : horizontal + 1'b1;
    wire [9:0] following_vertical = reset ? 10'd0 :
        horizontal != H_TOTAL - 1'b1 ? vertical :
        vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 1'b1;
    wire low_resolution = resolution == 2'd0;
    wire high_resolution = resolution == 2'd2;
    wire mode_valid = resolution != 2'd3;
    // Capture each plane immediately before its 16-pixel group. Group zero
    // uses the preceding line's blanking; staging preserves its final pixels.
    // A load commits the staged planes for the next 16-pixel group.
    wire capture_enable, capture_load;
    wire [1:0] capture_stage;
    wire address_valid;
    generate
        if (CACHED_WORD_PORT) begin : cached_coordinates
            reg [8:0] native_row;
            reg [1:0] row_repeat;
            // Each mode's capture and fetch decisions depend only on the
            // counters. They are registered from the next counters, and
            // resolution selects among the registered results where used.
            wire following_color_line = following_vertical >= 10'd60 && following_vertical < 10'd660;
            wire following_mono_line = following_vertical >= 10'd160 && following_vertical < 10'd560;
            // The next-line windows are the same bounds shifted back one.
            // Frame/10-bit wrap both produce zero, outside either window.
            wire following_next_color_line = following_vertical >= 10'd59 && following_vertical < 10'd659;
            wire following_next_mono_line = following_vertical >= 10'd159 && following_vertical < 10'd559;
            // These are the original h+planes capture cycles, expressed before
            // addition/wrap comparisons. Cache lookup still occurs exactly two
            // clocks earlier. Low group zero starts at h=1646, two phases after
            // h[1:0]; medium uses two phases and mono always captures plane zero.
            reg low_capture = 1'b0, medium_capture = 1'b0, mono_capture = 1'b0;
            reg low_load = 1'b0, medium_load = 1'b0, mono_load = 1'b0;
            reg [1:0] low_stage = 2'd0;
            wire low_window =
                (following_color_line && following_horizontal < 11'd1276 && following_horizontal[5:0] >= 6'd60) ||
                (following_next_color_line && following_horizontal >= 11'd1646);
            wire medium_window =
                (following_color_line && following_horizontal < 11'd1278 && following_horizontal[4:0] >= 5'd30) ||
                (following_next_color_line && following_horizontal >= 11'd1648);
            wire mono_window =
                (following_mono_line && following_horizontal < 11'd1279 && following_horizontal[4:0] == 5'd31) ||
                (following_next_mono_line && following_horizontal == 11'd1649);
            wire low_commit = following_horizontal == 11'd1649 || &following_horizontal[5:0];
            wire fast_commit = following_horizontal == 11'd1649 || &following_horizontal[4:0];
            always @(posedge clk) begin
                low_capture <= low_window;
                medium_capture <= medium_window;
                mono_capture <= mono_window;
                low_load <= low_window && low_commit;
                medium_load <= medium_window && fast_commit;
                mono_load <= mono_window && fast_commit;
                low_stage <= following_horizontal >= 11'd1646 ?
                    following_horizontal[1:0] ^ 2'd2 : following_horizontal[1:0];
            end
            assign capture_enable = low_resolution ? low_capture :
                high_resolution ? mono_capture : resolution == 2'd1 && medium_capture;
            assign capture_load = low_resolution ? low_load :
                high_resolution ? mono_load : resolution == 2'd1 && medium_load;
            assign capture_stage = low_resolution ? low_stage :
                high_resolution ? 2'd0 : {1'b0, horizontal[0]};
            // Native rows are tracked at EOL. Each scale's next row is
            // registered from the next counters and row state.
            wire cached_next_image_line = mode_valid && (high_resolution ?
                vertical >= 10'd159 && vertical < 10'd559 :
                vertical >= 10'd59 && vertical < 10'd659);
            wire before_image = high_resolution ? vertical < 10'd160 : vertical < 10'd60;
            wire advance_row = high_resolution || row_repeat == 2'd2;
            reg [8:0] mono_next_row = 9'd0, color_next_row = 9'd0;
            // EOL clears native_row outside the image, including the frame
            // boundary where coherent configuration can change. The row is
            // already zero there, so no current-line geometry mask is needed.
            assign raster_row = native_row;
            assign raster_next_row = !mode_valid ? 9'd0 :
                high_resolution ? mono_next_row : color_next_row;
            wire [8:0] following_native_row = reset ? 9'd0 :
                horizontal == H_TOTAL - 1'b1 ? raster_next_row : native_row;
            wire [1:0] following_row_repeat = reset ? 2'd0 :
                horizontal != H_TOTAL - 1'b1 ? row_repeat :
                !cached_next_image_line || before_image || advance_row ? 2'd0 : row_repeat + 2'd1;
            always @(posedge clk) begin
                native_row <= following_native_row;
                row_repeat <= following_row_repeat;
                mono_next_row <= following_vertical >= 10'd160 && following_vertical < 10'd559 ?
                    following_native_row + 9'd1 : 9'd0;
                color_next_row <= following_vertical >= 10'd60 && following_vertical < 10'd659 ?
                    following_native_row + {8'd0, following_row_repeat == 2'd2} : 9'd0;
            end
            // Complete-line bounds, ownership and readiness are checked by the
            // cache adapter. No physical address is reconstructed in this path.
            assign address_valid = 1'b1;
            assign mem_addr = 18'd0;
            // Fixed lookaheads are evaluated in parallel; resolution selects
            // their registered results. The cache captures each word two
            // clocks before the unchanged plane-capture edge.
            wire low_next_wrap = following_horizontal >= 11'd1644;
            wire medium_next_wrap = following_horizontal >= 11'd1646;
            wire mono_next_wrap = following_horizontal >= 11'd1647;
            wire [10:0] following_low_x = low_next_wrap ? following_horizontal - 11'd1644 : following_horizontal + 11'd6;
            wire [10:0] following_medium_x = medium_next_wrap ? following_horizontal - 11'd1646 : following_horizontal + 11'd4;
            wire [10:0] following_mono_x = mono_next_wrap ? following_horizontal - 11'd1647 : following_horizontal + 11'd3;
            // The phase windows below supply these low bits without addition.
            wire unused_following_phase = ^{following_low_x[5:2], following_medium_x[4:1], following_mono_x[4:0]};
            // These constant windows are the corresponding x<1280 and plane
            // phase tests, expressed before the lookahead carry chain. Group0
            // uses the next line during the last blanking clocks.
            reg low_fetching = 1'b0, medium_fetching = 1'b0, mono_fetching = 1'b0;
            reg low_wrap = 1'b0, medium_wrap = 1'b0, mono_wrap = 1'b0;
            reg [6:0] low_column = 7'd2, medium_column = 7'd0, mono_column = 7'd0;
            always @(posedge clk) begin
                low_fetching <=
                    (following_color_line && following_horizontal < 11'd1274 &&
                     following_horizontal[5:0] >= 6'd58 && following_horizontal[5:0] < 6'd62) ||
                    (following_next_color_line && following_horizontal >= 11'd1644 && following_horizontal < 11'd1648);
                medium_fetching <=
                    (following_color_line && following_horizontal < 11'd1276 && following_horizontal[4:1] == 4'd14) ||
                    (following_next_color_line && following_horizontal >= 11'd1646 && following_horizontal < 11'd1648);
                mono_fetching <=
                    (following_mono_line && following_horizontal < 11'd1277 && following_horizontal[4:0] == 5'd29) ||
                    (following_next_mono_line && following_horizontal == 11'd1647);
                low_wrap <= low_next_wrap;
                medium_wrap <= medium_next_wrap;
                mono_wrap <= mono_next_wrap;
                low_column <= {following_low_x[10:6], following_low_x[1:0]};
                medium_column <= {following_medium_x[10:5], following_medium_x[0]};
                mono_column <= {1'b0, following_mono_x[10:5]};
            end
            assign fetch_valid = low_resolution ? low_fetching :
                high_resolution ? mono_fetching : resolution == 2'd1 && medium_fetching;
            wire read_wrap = low_resolution ? low_wrap : high_resolution ? mono_wrap : medium_wrap;
            assign fetch_row = read_wrap ? raster_next_row : raster_row;
            assign fetch_column = low_resolution ? low_column :
                high_resolution ? mono_column : medium_column;
        end else begin : physical_coordinates
            // Retain the combinational physical word-port address and capture
            // arithmetic, including its invalid-address behavior.
            wire [9:0] image_top = high_resolution ? 10'd160 : 10'd60;
            wire [9:0] image_height = high_resolution ? 10'd400 : 10'd600;
            wire [9:0] next_vertical = vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 10'd1;
            wire [2:0] planes = low_resolution ? 3'd4 : high_resolution ? 3'd1 : 3'd2;
            wire [11:0] ahead = {1'b0, horizontal} + {9'd0, planes};
            wire wrap_line = ahead >= {1'b0, H_TOTAL};
            wire [10:0] fetch_x = wrap_line ? ahead[10:0] - H_TOTAL : ahead[10:0];
            wire [9:0] fetch_y = wrap_line ?
                (vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 1'b1) : vertical;
            wire [5:0] group_phase = low_resolution ? fetch_x[5:0] : {1'b0, fetch_x[4:0]};
            wire fetching = mode_valid && fetch_x < H_ACTIVE &&
                fetch_y >= image_top && fetch_y < image_top + image_height &&
                group_phase < {3'd0, planes};
            assign capture_enable = fetching;
            assign capture_stage = group_phase[1:0];
            assign capture_load = fetching && group_phase == {3'd0, planes} - 6'd1;
            wire image_line = mode_valid && vertical >= image_top && vertical < image_top + image_height;
            wire next_image_line = mode_valid && next_vertical >= image_top &&
                                   next_vertical < image_top + image_height;
            wire [5:0] group_number = low_resolution ? {1'b0, fetch_x[10:6]} : fetch_x[10:5];
            wire [9:0] image_y = fetch_y - image_top;
            wire [9:0] native_y = high_resolution ? image_y : image_y / 10'd3;
            assign fetch_valid = fetching && address_valid;
            assign fetch_row = native_y[8:0];
            assign fetch_column = low_resolution ? {group_number[4:0], group_phase[1:0]} :
                high_resolution ? {1'b0, group_number} : {group_number, group_phase[0]};
            wire [9:0] raster_y = vertical - image_top;
            wire [9:0] next_raster_y = next_vertical - image_top;
            assign raster_row = !image_line ? 9'd0 : high_resolution ?
                raster_y[8:0] : 9'(raster_y / 10'd3);
            assign raster_next_row = !next_image_line ? 9'd0 : high_resolution ?
                next_raster_y[8:0] : 9'(next_raster_y / 10'd3);
            wire [6:0] stride_words = high_resolution ? 7'd40 : 7'd80;
            wire [16:0] line_offset = {7'd0, native_y} * {10'd0, stride_words};
            wire [8:0] group_offset = {3'd0, group_number} * {6'd0, planes};
            // ST base registers only provide bits23:8. Keep the addition wide
            // enough to reject bad scanout without wrapping into 512KiB RAM.
            wire [23:0] address_word = {1'b0, screen_base[23:8], 7'd0} +
                {7'd0, line_offset} + {15'd0, group_offset} + {18'd0, group_phase};
            assign address_valid = address_word < 24'h040000;
            assign mem_addr = fetching && address_valid ? address_word[17:0] : 18'd0;
        end
    endgenerate
    wire [15:0] fetched_word = address_valid ? mem_data : 16'd0;

    wire [15:0] load_plane0 = high_resolution ? fetched_word : staging0;
    wire [15:0] load_plane1 = low_resolution ? staging1 : high_resolution ? 16'd0 : fetched_word;
    wire [15:0] load_plane2 = low_resolution ? staging2 : 16'd0;
    wire [15:0] load_plane3 = low_resolution ? fetched_word : 16'd0;

    always @(posedge clk) begin
        horizontal <= following_horizontal;
        vertical <= following_vertical;
        if (reset) begin
            staging0 <= 16'd0;
            staging1 <= 16'd0;
            staging2 <= 16'd0;
            plane0 <= 16'd0;
            plane1 <= 16'd0;
            plane2 <= 16'd0;
            plane3 <= 16'd0;
            picture_valid <= 1'b0;
        end else begin
            if (capture_enable) begin
                case (capture_stage)
                    2'd0: staging0 <= fetched_word;
                    2'd1: staging1 <= fetched_word;
                    2'd2: staging2 <= fetched_word;
                    default: begin end
                endcase
            end
            if (capture_load) begin
                plane0 <= load_plane0;
                plane1 <= load_plane1;
                plane2 <= load_plane2;
                plane3 <= load_plane3;
                picture_valid <= address_valid;
            end
        end
    end

    // Lookup both colour modes before selecting the completed pixel. Keep
    // all four plane bits in each path, including during mode transitions.
    // The indices and windows come from registers loaded with the next
    // state, so no counter compare or plane bit select reaches the RGB request.
    // The bit positions are registered one pixel further ahead, so each index
    // selects from the held planes and the loaded words in parallel.
    wire [10:0] pixel_after = following_horizontal == H_TOTAL - 1'b1 ?
        11'd0 : following_horizontal + 1'b1;
    // Only the pixel phase bits select plane bits.
    wire unused_pixel_after = ^{pixel_after[10:6], pixel_after[0]};
    reg [3:0] low_bit = 4'd15, fast_bit = 4'd15;
    reg [3:0] low_color_index = 4'd0, fast_color_index = 4'd0;
    reg active_display = 1'b1, color_rows = 1'b0, mono_rows = 1'b0;
    reg line_end = 1'b0, frame_start = 1'b1, vsync_rows = 1'b0, hsync_columns = 1'b0;
    always @(posedge clk) begin
        low_bit <= 4'd15 - pixel_after[5:2];
        fast_bit <= 4'd15 - pixel_after[4:1];
        low_color_index <= reset ? 4'd0 : capture_load ?
            {load_plane3[low_bit], load_plane2[low_bit], load_plane1[low_bit], load_plane0[low_bit]} :
            {plane3[low_bit], plane2[low_bit], plane1[low_bit], plane0[low_bit]};
        fast_color_index <= reset ? 4'd0 : capture_load ?
            {load_plane3[fast_bit], load_plane2[fast_bit], load_plane1[fast_bit], load_plane0[fast_bit]} :
            {plane3[fast_bit], plane2[fast_bit], plane1[fast_bit], plane0[fast_bit]};
        active_display <= following_horizontal < H_ACTIVE && following_vertical < V_ACTIVE;
        color_rows <= following_vertical >= 10'd60 && following_vertical < 10'd660;
        mono_rows <= following_vertical >= 10'd160 && following_vertical < 10'd560;
        line_end <= following_horizontal == H_TOTAL - 1'b1;
        frame_start <= following_horizontal == 11'd0 && following_vertical == 10'd0;
        vsync_rows <= following_vertical >= 10'd725 && following_vertical < 10'd730;
        hsync_columns <= following_horizontal >= 11'd1390 && following_horizontal < 11'd1430;
    end
    wire [8:0] low_color = palette[low_color_index * 9 +: 9];
    wire [8:0] fast_color = palette[fast_color_index * 9 +: 9];
    wire [8:0] border = palette[8:0];
    function automatic [7:0] expand3(input [2:0] intensity);
        expand3 = {intensity, intensity, intensity[2:1]};
    endfunction
    wire mono_white = fast_color_index[0] ^ palette[0];
    wire [23:0] low_rgb =
        {expand3(low_color[8:6]), expand3(low_color[5:3]), expand3(low_color[2:0])};
    wire [23:0] fast_rgb =
        {expand3(fast_color[8:6]), expand3(fast_color[5:3]), expand3(fast_color[2:0])};
    wire [23:0] border_rgb =
        {expand3(border[8:6]), expand3(border[5:3]), expand3(border[2:0])};
    // Low 4x3 and medium 2x3 fill 1280x600; high 2x1 fills 1280x400.
    // This first slice uses nearest-neighbor integer scaling, not aspect correction.
    // Each colour source has its own select, decoded from registered windows
    // and the mode in parallel with the palette lookups. High resolution has
    // a black border; an incomplete group shows black inside the picture.
    wire show_pixels = active_display && picture_valid;
    wire show_low = show_pixels && color_rows && low_resolution;
    wire show_medium = show_pixels && color_rows && resolution == 2'd1;
    wire show_mono = show_pixels && mono_rows && high_resolution;
    wire show_border = active_display && !color_rows && (low_resolution || resolution == 2'd1);
    wire [23:0] rgb = ({24{show_low}} & low_rgb) | ({24{show_medium}} & fast_rgb) |
        {24{show_mono && mono_white}} | ({24{show_border}} & border_rgb);
    assign video_request = {
        1'b0, hold || reset,
        line_end, frame_start, 1'b1, vsync_rows, hsync_columns,
        active_display, rgb
    };
endmodule
