// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_video_part.vh"

// Atari ST display-memory slice, scaled before the existing RGB888 socket.
// Layout/palette: Atari Corp., Engineering Hardware Specification, 7 Jan 1986,
// section 3. The word port is combinational and shares this clock in simulation;
// a hardware shell must supply coherent configuration and a RAM/CDC adapter.
// This is fixed 720p scanout, not the original GLUE/MMU/Shifter bus timing.
module st_video (
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
    wire [2:0] planes = low_resolution ? 3'd4 : high_resolution ? 3'd1 : 3'd2;
    // Low 4x3 and medium 2x3 fill 1280x600; high 2x1 fills 1280x400.
    // This first slice uses nearest-neighbor integer scaling, not aspect correction.
    wire [9:0] image_top = high_resolution ? 10'd160 : 10'd60;
    wire [9:0] image_height = high_resolution ? 10'd400 : 10'd600;
    wire in_picture = horizontal < H_ACTIVE && vertical >= image_top &&
                      vertical < image_top + image_height && mode_valid;
    wire [3:0] bit_index = 4'd15 - (low_resolution ? horizontal[5:2] : horizontal[4:1]);

    // Fetch every plane immediately before its 16-pixel group. Group zero is
    // fetched during the preceding line's blanking, including at frame wrap.
    // Staging words keep the preceding group's final pixels unchanged.
    wire [11:0] ahead = {1'b0, horizontal} + {9'd0, planes};
    wire wrap_line = ahead >= {1'b0, H_TOTAL};
    wire [10:0] fetch_x = wrap_line ? ahead[10:0] - H_TOTAL : ahead[10:0];
    wire [9:0] fetch_y = wrap_line ?
        (vertical == V_TOTAL - 1'b1 ? 10'd0 : vertical + 1'b1) : vertical;
    wire [5:0] group_phase = low_resolution ? fetch_x[5:0] : {1'b0, fetch_x[4:0]};
    wire [5:0] group_number = low_resolution ? {1'b0, fetch_x[10:6]} : fetch_x[10:5];
    wire fetching = mode_valid && fetch_x < H_ACTIVE &&
        fetch_y >= image_top && fetch_y < image_top + image_height &&
        group_phase < {3'd0, planes};
    wire [9:0] image_y = fetch_y - image_top;
    wire [9:0] native_y = high_resolution ? image_y : image_y / 10'd3;
    wire [6:0] stride_words = high_resolution ? 7'd40 : 7'd80;
    wire [16:0] line_offset = {7'd0, native_y} * {10'd0, stride_words};
    wire [8:0] group_offset = {3'd0, group_number} * {6'd0, planes};
    // ST base registers only provide bits23:8. Keep the addition wide enough
    // to reject out-of-range scanout without wrapping into the 512KiB RAM.
    wire [23:0] address_word = {1'b0, screen_base[23:8], 7'd0} +
        {7'd0, line_offset} + {15'd0, group_offset} + {18'd0, group_phase};
    wire address_valid = address_word < 24'h040000;
    assign mem_addr = fetching && address_valid ? address_word[17:0] : 18'd0;
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
            if (fetching) begin
                case (group_phase[1:0])
                    2'd0: staging0 <= fetched_word;
                    2'd1: staging1 <= fetched_word;
                    2'd2: staging2 <= fetched_word;
                    default: begin end
                endcase
                if (group_phase == {3'd0, planes} - 6'd1) begin
                    plane0 <= high_resolution ? fetched_word : staging0;
                    plane1 <= low_resolution ? staging1 : high_resolution ? 16'd0 : fetched_word;
                    plane2 <= low_resolution ? staging2 : 16'd0;
                    plane3 <= low_resolution ? fetched_word : 16'd0;
                    picture_valid <= address_valid;
                end
            end
        end
    end

    wire [3:0] color_index = {plane3[bit_index], plane2[bit_index],
                              plane1[bit_index], plane0[bit_index]};
    wire [8:0] color = palette[color_index * 9 +: 9];
    wire [8:0] border = palette[8:0];
    function automatic [7:0] expand3(input [2:0] intensity);
        expand3 = {intensity, intensity, intensity[2:1]};
    endfunction
    wire mono_white = plane0[bit_index] ^ palette[0];
    wire [23:0] picture_rgb = high_resolution ? {24{mono_white}} :
        {expand3(color[8:6]), expand3(color[5:3]), expand3(color[2:0])};
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
