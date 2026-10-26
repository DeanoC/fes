// SPDX-License-Identifier: MIT
// pc386 720p presenter (docs/pc386-machine-design.md, "Capture and
// presentation at 720p").
//
// At each 720p frame (from the start of vertical blanking) the presenter takes
// the newest complete capture buffer, never the one being written, and scales
// it to a 960 x 720 picture centred in the 1280 x 720 raster with an
// area-weighted filter. Per axis, output pixel o of N covers source interval
// [o S / N, (o + 1) S / N) for a source of S pixels (S <= N); it is one source
// pixel or a blend of two by coverage:
//
//   r = o S mod N, s = o S div N; if r + S > N:
//     w = min(256, ((r + S - N) * R + 2^15) >> 16) with R = (2^24 + S/2) div S,
//     out = a + (((b - a) w + 128) >> 8)   (arithmetic shift, 8-bit channels)
//
// with a = source s and b = source s + 1, horizontally in each of the two
// source rows, then vertically. Most output pixels are unblended. The 6-bit
// DAC values are kept as 18-bit pixels and widened by replication before the
// blend. sim/pc386_presenter_model.h holds the same arithmetic.
//
// The system-clock side reads source rows from DDR port 2 into two dual-clock
// line buffers, one for even rows and one for odd, each with two row slots,
// staying at most three rows ahead of the row the raster uses. A row that is
// not loaded in time shows black and counts as an underflow.
//
// video is the shell video word: RGB888 in 23..0 (red high), then DE, HS, VS
// and CE, for fes_display_plane.
module pc386_presenter #(
    parameter [28:0] CAPTURE_BASE = 29'h06140000
) (
    input wire clk, reset,
    input wire [1:0] frame_buffer,
    input wire [9:0] frame_width, frame_height,
    input wire [15:0] frame_sequence,
    output reg [1:0] present_buffer,
    output reg [28:0] ddr_address,
    output reg [7:0] ddr_burstcount,
    output reg ddr_read,
    input wire ddr_waitrequest,
    input wire [63:0] ddr_readdata,
    input wire ddr_readdatavalid,
    input wire pixel_clk, pixel_reset,
    output reg [27:0] video,
    output reg [31:0] underflows,
    output reg [31:0] frames_shown
);
    localparam [10:0] H_LAST = 11'd1649;
    localparam [9:0] V_LAST = 10'd749;
    localparam [10:0] LEFT = 11'd160;
    localparam [10:0] ROW_SETUP = 11'd1400;
    localparam [10:0] PRELOAD = 11'd1440;
    localparam [10:0] FIRST_STEP = LEFT - 11'd3;

    // ===== Pixel clock =================================================
    reg [10:0] h;
    reg [9:0] v;
    always @(posedge pixel_clk) begin
        if (pixel_reset) begin h <= 0; v <= 0; end
        else begin
            h <= h == H_LAST ? 11'd0 : h + 11'd1;
            if (h == H_LAST) v <= v == V_LAST ? 10'd0 : v + 10'd1;
        end
    end

    // The newest complete capture, crossed by the sequence's low bit.
    (* async_reg = "true" *) reg [2:0] sequence_sync;
    reg [1:0] latest_buffer;
    reg [9:0] latest_width, latest_height;
    reg latest_valid;
    always @(posedge pixel_clk) begin
        if (pixel_reset) begin
            sequence_sync <= 0; latest_valid <= 0; latest_buffer <= 0; latest_width <= 0; latest_height <= 0;
        end else begin
            sequence_sync <= {sequence_sync[1:0], frame_sequence[0]};
            if (sequence_sync[2] != sequence_sync[1]) begin
                latest_valid <= 1;
                latest_buffer <= frame_buffer;
                latest_width <= frame_width;
                latest_height <= frame_height;
            end
        end
    end

    // Frame setup at the start of vertical blanking; reciprocals by a
    // restoring divider during blanking.
    reg [1:0] shown_buffer;
    reg [9:0] width, height;
    reg showing;
    reg [1:0] epoch;
    reg [24:0] recip_w, recip_h;
    reg [5:0] divide_step;
    reg [24:0] quotient, remainder_acc;
    reg divide_height;
    wire frame_setup = h == 11'd0 && v == 10'd720;
    wire frame_ok = latest_valid && latest_width != 0 && latest_height != 0 &&
                    latest_width <= 10'd960 && latest_height <= 10'd720;
    // (2^24 + d/2) div d, one quotient bit per clock.
    wire [9:0] divisor = divide_height ? height : width;
    wire [25:0] dividend = 26'h1000000 + {17'b0, divisor[9:1]};
    wire [25:0] trial = {remainder_acc, dividend[5'd25 - divide_step[4:0]]} - {16'b0, divisor};
    always @(posedge pixel_clk) begin
        if (pixel_reset) begin
            shown_buffer <= 0; width <= 0; height <= 0; showing <= 0; epoch <= 0; frames_shown <= 0;
            recip_w <= 0; recip_h <= 0; divide_step <= 6'd63; quotient <= 0; remainder_acc <= 0; divide_height <= 0;
        end else begin
            if (frame_setup) begin
                showing <= frame_ok;
                if (frame_ok) begin
                    shown_buffer <= latest_buffer;
                    width <= latest_width;
                    height <= latest_height;
                    frames_shown <= frames_shown + 32'd1;
                end
                epoch <= epoch + 2'd1;
                divide_step <= 0;
                divide_height <= 0;
                quotient <= 0;
                remainder_acc <= 0;
            end else if (divide_step < 6'd26) begin
                if (!trial[25]) begin
                    remainder_acc <= trial[24:0];
                    quotient <= {quotient[23:0], 1'b1};
                end else begin
                    remainder_acc <= {remainder_acc[23:0], dividend[5'd25 - divide_step[4:0]]};
                    quotient <= {quotient[23:0], 1'b0};
                end
                divide_step <= divide_step + 6'd1;
            end else if (divide_step == 6'd26) begin
                if (divide_height) begin recip_h <= quotient; divide_step <= 6'd63; end
                else begin
                    recip_w <= quotient;
                    divide_height <= 1;
                    divide_step <= 0;
                    quotient <= 0;
                    remainder_acc <= 0;
                end
            end
        end
    end

    function automatic [8:0] weight(input [9:0] numerator, input [24:0] reciprocal);
        reg [34:0] product;
        begin
            product = {25'b0, numerator} * {10'b0, reciprocal} + 35'h8000;
            weight = product[34:24] != 0 ? 9'd256 : {1'b0, product[23:16]};
        end
    endfunction

    // Rows: oy steps the source row sy by H / 720.
    reg [9:0] ry, sy;
    reg [8:0] wy;
    reg rows_ready;
    wire row_blank = !rows_ready;
    reg [9:0] row_a, row_b;
    reg [9:0] loaded;          // rows below this are in the line buffers
    reg [1:0] loaded_epoch;
    wire [9:0] next_line = v == V_LAST ? 10'd0 : v + 10'd1;
    wire row_setup = h == ROW_SETUP && next_line < 10'd720 && showing;
    wire [10:0] ry_sum = {1'b0, ry} + {1'b0, height};
    // Fetch window message to the system clock: {epoch, buffer, width, height, limit}.
    reg request_toggle;
    reg [9:0] request_limit;
    always @(posedge pixel_clk) begin
        if (pixel_reset) begin
            ry <= 0; sy <= 0; wy <= 0; rows_ready <= 0; row_a <= 0; row_b <= 0;
            request_toggle <= 0; request_limit <= 0; underflows <= 0;
        end else begin
            if (frame_setup) begin
                ry <= 0; sy <= 0;
                request_limit <= 10'd3;
                request_toggle <= !request_toggle;
            end else if (row_setup) begin
                // Coverage of output row next_line: [ry, ry + H) in 1/720 rows from sy.
                row_a <= sy;
                row_b <= sy + 10'd1 >= height ? height - 10'd1 : sy + 10'd1;
                wy <= ry_sum > 11'd720 ? weight(ry_sum[9:0] - 10'd720, recip_h) : 9'd0;
                rows_ready <= loaded_epoch == epoch && sy < loaded &&
                              (ry_sum <= 11'd720 || sy + 10'd1 < loaded);
                if (!(loaded_epoch == epoch && sy < loaded && (ry_sum <= 11'd720 || sy + 10'd1 < loaded)))
                    underflows <= underflows + 32'd1;
                // Rows sy and sy + 1 are in use until the next row setup, so
                // the fetch may run to sy + 3 (sy + 4 shares sy's slot).
                if (request_limit != sy + 10'd3) begin
                    request_limit <= sy + 10'd3;
                    request_toggle <= !request_toggle;
                end
                // Advance for the following row.
                if (ry_sum >= 11'd720) begin
                    ry <= ry_sum[9:0] - 10'd720;
                    sy <= sy + 10'd1;
                end else ry <= ry_sum[9:0];
            end
        end
    end

    // Loaded-row reports from the system clock.
    (* async_reg = "true" *) reg [2:0] loaded_sync;
    wire loaded_toggle;
    wire [1:0] loaded_epoch_in;
    wire [9:0] loaded_in;
    always @(posedge pixel_clk) begin
        if (pixel_reset) begin loaded_sync <= 0; loaded <= 0; loaded_epoch <= 0; end
        else begin
            loaded_sync <= {loaded_sync[1:0], loaded_toggle};
            if (loaded_sync[2] != loaded_sync[1]) begin loaded <= loaded_in; loaded_epoch <= loaded_epoch_in; end
        end
    end

    // ---- Horizontal generator and line buffer window --------------------
    // g2 runs two output pixels ahead of s1; the line buffers read the pair
    // that s1 will need after its next step.
    reg [9:0] g1_sx, g2_sx, s1_sx;
    reg [9:0] g1_rx, g2_rx, s1_rx;
    reg stepping;
    wire [10:0] g2_sum = {1'b0, g2_rx} + {1'b0, width};
    wire [9:0] g3_rx = g2_sum >= 11'd960 ? g2_sum[9:0] - 10'd960 : g2_sum[9:0];
    wire [9:0] g3_sx = g2_sum >= 11'd960 ? g2_sx + 10'd1 : g2_sx;
    wire step = stepping;
    wire [9:0] g1_next = step ? g2_sx : g1_sx;
    reg [1:0] preload;
    // Row a lives in buffer a[0], slot a[1]; row b in the other buffer.
    wire a_odd = row_a[0];
    wire [9:0] read_pair = preload == 2'd1 ? 10'd0 : preload == 2'd2 ? 10'd1 : {1'b0, g1_next[9:1]} + 10'd1;
    wire [9:0] even_row = a_odd ? row_b : row_a;
    wire [9:0] odd_row = a_odd ? row_a : row_b;
    wire [35:0] even_data, odd_data;
    reg [35:0] even_q0, even_q1, odd_q0, odd_q1;
    // System-clock writes (declared below).
    wire buffer_write_even, buffer_write_odd;
    wire [9:0] buffer_waddress;
    wire [35:0] buffer_wdata;
    pc386_dcram #(.WIDTH(36), .DEPTH(1024)) even_rows (
        .wclk(clk), .write(buffer_write_even), .waddress(buffer_waddress), .wdata(buffer_wdata),
        .rclk(pixel_clk), .read(1'b1), .raddress({even_row[1], read_pair[8:0]}), .rdata(even_data)
    );
    pc386_dcram #(.WIDTH(36), .DEPTH(1024)) odd_rows (
        .wclk(clk), .write(buffer_write_odd), .waddress(buffer_waddress), .wdata(buffer_wdata),
        .rclk(pixel_clk), .read(1'b1), .raddress({odd_row[1], read_pair[8:0]}), .rdata(odd_data)
    );
    always @(posedge pixel_clk) begin
        if (pixel_reset) begin
            g1_sx <= 0; g2_sx <= 0; s1_sx <= 0; g1_rx <= 0; g2_rx <= 0; s1_rx <= 0; stepping <= 0; preload <= 0;
            even_q0 <= 0; even_q1 <= 0; odd_q0 <= 0; odd_q1 <= 0;
        end else begin
            stepping <= h >= FIRST_STEP - 11'd1 && h < FIRST_STEP + 11'd959;
            if (h == PRELOAD) begin
                // s1 = pixel 0, g1 = pixel 1, g2 = pixel 2.
                s1_sx <= 0; s1_rx <= 0;
                g1_rx <= width >= 10'd960 ? width - 10'd960 : width;
                g1_sx <= width >= 10'd960 ? 10'd1 : 10'd0;
                preload <= 2'd1;
            end else if (h == PRELOAD + 11'd1) begin
                g2_rx <= g3_rx_from_g1;
                g2_sx <= g3_sx_from_g1;
                preload <= 2'd2;
            end else if (h == PRELOAD + 11'd2) begin
                even_q0 <= even_data; odd_q0 <= odd_data;
                preload <= 2'd3;
            end else if (h == PRELOAD + 11'd3) begin
                even_q1 <= even_data; odd_q1 <= odd_data;
                preload <= 2'd0;
            end
            if (step) begin
                s1_sx <= g1_sx; s1_rx <= g1_rx;
                g1_sx <= g2_sx; g1_rx <= g2_rx;
                g2_sx <= g3_sx; g2_rx <= g3_rx;
                if (g1_sx[9:1] != s1_sx[9:1]) begin
                    even_q0 <= even_q1; even_q1 <= even_data;
                    odd_q0 <= odd_q1; odd_q1 <= odd_data;
                end
            end
        end
    end
    wire [10:0] g1_sum = {1'b0, g1_rx} + {1'b0, width};
    wire [9:0] g3_rx_from_g1 = g1_sum >= 11'd960 ? g1_sum[9:0] - 10'd960 : g1_sum[9:0];
    wire [9:0] g3_sx_from_g1 = g1_sum >= 11'd960 ? g1_sx + 10'd1 : g1_sx;

    // ---- Blend ---------------------------------------------------------------
    function automatic [23:0] widen(input [17:0] pixel);
        widen = {pixel[17:12], pixel[17:16], pixel[11:6], pixel[11:10], pixel[5:0], pixel[5:4]};
    endfunction
    // Weight 256 takes b, so the product is signed 9 x unsigned 8 bits: three
    // fit one DSP block in 9 x 9 mode.
    function automatic [7:0] lerp(input [7:0] a, input [7:0] b, input [8:0] w);
        reg signed [8:0] difference;
        reg signed [17:0] scaled;
        begin
            difference = $signed({1'b0, b}) - $signed({1'b0, a});
            scaled = difference * $signed({1'b0, w[7:0]}) + 18'sd128;
            lerp = w[8] ? b : a + 8'(scaled >>> 8);
        end
    endfunction
    function automatic [23:0] lerp3(input [23:0] a, input [23:0] b, input [8:0] w);
        lerp3 = {lerp(a[23:16], b[23:16], w), lerp(a[15:8], b[15:8], w), lerp(a[7:0], b[7:0], w)};
    endfunction
    // s1: the two source pixels of each row.
    wire [35:0] a_q0 = a_odd ? odd_q0 : even_q0;
    wire [35:0] a_q1 = a_odd ? odd_q1 : even_q1;
    wire [35:0] b_q0 = a_odd ? even_q0 : odd_q0;
    wire [35:0] b_q1 = a_odd ? even_q1 : odd_q1;
    wire [17:0] a0 = s1_sx[0] ? a_q0[35:18] : a_q0[17:0];
    wire [17:0] a1 = s1_sx[0] ? a_q1[17:0] : a_q0[35:18];
    wire [17:0] b0 = s1_sx[0] ? b_q0[35:18] : b_q0[17:0];
    wire [17:0] b1 = s1_sx[0] ? b_q1[17:0] : b_q0[35:18];
    wire [10:0] s1_sum = {1'b0, s1_rx} + {1'b0, width};
    reg [23:0] p_a0, p_a1, p_b0, p_b1, h_a, h_b;
    reg [8:0] p_wx;
    reg [23:0] picture;
    always @(posedge pixel_clk) begin
        p_a0 <= widen(a0); p_a1 <= widen(a1); p_b0 <= widen(b0); p_b1 <= widen(b1);
        p_wx <= s1_sum > 11'd960 ? weight(s1_sum[9:0] - 10'd960, recip_w) : 9'd0;
        h_a <= lerp3(p_a0, p_a1, p_wx);
        h_b <= lerp3(p_b0, p_b1, p_wx);
        picture <= lerp3(h_a, h_b, wy);
    end
    wire in_picture = h >= LEFT && h < LEFT + 11'd960 && v < 10'd720;
    always @(posedge pixel_clk) begin
        if (pixel_reset) video <= 28'h8000000;
        else video <= {1'b1, v >= 10'd725 && v < 10'd730, h >= 11'd1390 && h < 11'd1430, h < 11'd1280 && v < 10'd720,
                       in_picture && showing && !row_blank ? picture : 24'd0};
    end

    // ===== System clock ===================================================
    (* async_reg = "true" *) reg [2:0] request_sync;
    reg [1:0] f_epoch, f_buffer, f_row_buffer, f_row_epoch;
    reg [9:0] f_width, f_height, f_limit, f_row, f_loaded;
    reg f_busy, f_new_epoch;
    reg [8:0] f_beats, f_issued, f_received;
    reg loaded_flag;
    reg [9:0] loaded_out;
    reg [1:0] loaded_epoch_out;
    assign loaded_toggle = loaded_flag;
    assign loaded_in = loaded_out;
    assign loaded_epoch_in = loaded_epoch_out;
    wire [8:0] row_beats = {f_width[9:1]} + {8'b0, f_width[0]};
    wire [8:0] burst_left = f_beats - f_issued;
    wire [8:0] burst = burst_left > 9'd64 ? 9'd64 : burst_left;
    always @(posedge clk) begin
        if (reset) begin
            request_sync <= 0; f_epoch <= 0; f_buffer <= 0; f_width <= 0; f_height <= 0; f_limit <= 0;
            f_row <= 0; f_loaded <= 0; f_busy <= 0; f_row_buffer <= 0; f_row_epoch <= 0; f_new_epoch <= 0; f_beats <= 0; f_issued <= 0; f_received <= 0;
            loaded_flag <= 0; loaded_out <= 0; loaded_epoch_out <= 0; present_buffer <= 0;
            ddr_read <= 0; ddr_address <= 0; ddr_burstcount <= 0;
        end else begin
            request_sync <= {request_sync[1:0], request_toggle};
            if (request_sync[2] != request_sync[1]) begin
                // Fields are stable: they change a row or more before the toggle.
                if (f_epoch != epoch) f_new_epoch <= 1;
                f_epoch <= epoch;
                f_limit <= request_limit;
                f_buffer <= shown_buffer;
                f_width <= width;
                f_height <= height;
                present_buffer <= shown_buffer;
            end
            if (f_new_epoch && !f_busy) begin
                f_new_epoch <= 0;
                f_row <= 0;
                f_loaded <= 0;
            end else if (!f_busy && f_row <= f_limit && f_row < f_height) begin
                f_busy <= 1;
                f_row_buffer <= f_buffer;
                f_row_epoch <= f_epoch;
                f_beats <= row_beats;
                f_issued <= 0;
                f_received <= 0;
            end
            if (ddr_read && !ddr_waitrequest) ddr_read <= 0;
            if (f_busy && f_issued != f_beats && (!ddr_read || !ddr_waitrequest) && f_received == f_issued) begin
                // One burst at a time; line l of buffer b is at beat 0x40000 b + 400 l.
                ddr_read <= 1;
                ddr_burstcount <= burst[7:0];
                ddr_address <= CAPTURE_BASE + {9'b0, f_row_buffer, 18'b0} +
                               {10'b0, f_row, 9'b0} - {12'b0, f_row, 7'b0} + {15'b0, f_row, 4'b0} + {20'b0, f_issued};
                f_issued <= f_issued + burst;
            end
            if (ddr_readdatavalid) f_received <= f_received + 9'd1;
            if (f_busy && f_received == f_beats && f_issued == f_beats && !ddr_read) begin
                f_busy <= 0;
                f_row <= f_row + 10'd1;
                f_loaded <= f_row + 10'd1;
                loaded_out <= f_row + 10'd1;
                loaded_epoch_out <= f_row_epoch;
                loaded_flag <= !loaded_flag;
            end
        end
    end
    // Beat k of row r lands in pair k of the row's buffer and slot.
    assign buffer_waddress = {f_row[1], f_received[8:0]};
    function automatic [17:0] narrow(input [31:0] pixel);
        narrow = {pixel[23:18], pixel[15:10], pixel[7:2]};
    endfunction
    assign buffer_wdata = {narrow(ddr_readdata[63:32]), narrow(ddr_readdata[31:0])};
    assign buffer_write_even = ddr_readdatavalid && !f_row[0];
    assign buffer_write_odd = ddr_readdatavalid && f_row[0];
    wire unused = ^{f_loaded, g1_rx};
endmodule
