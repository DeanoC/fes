// SPDX-License-Identifier: GPL-2.0-or-later
// Read-only frame fetcher. WINDOW_BASE is supplied by the shared DDR contract
// at board integration; the zero default deliberately cannot select HPS RAM.
module fes_menu_reader #(
    parameter [31:0] WINDOW_BASE = 32'd0
) (
    input wire clk,
    input wire rst,
    input wire enable,
    input wire slot,
    input wire start,
    input wire pixel_ready,
    output wire pixel_valid,
    output wire [31:0] pixel_bgrx,
    output wire [19:0] pixel_index,
    output reg done = 1'b0,
    output wire idle,
    output reg [27:0] address = 28'd0,
    output reg [7:0] burstcount = 8'd0,
    output reg read = 1'b0,
    input wire waitrequest,
    input wire [127:0] readdata,
    input wire readdatavalid
);
    localparam [17:0] FRAME_BEATS = 18'd230400;
    localparam [19:0] LAST_PIXEL = 20'd921599;
    reg active = 1'b0;
    reg selected_slot = 1'b0;
    reg [17:0] issued = 18'd0;
    reg [8:0] outstanding = 9'd0;
    reg [8:0] count = 9'd0;
    reg [7:0] head = 8'd0;
    reg [7:0] tail = 8'd0;
    reg [1:0] lane = 2'd0;
    reg [19:0] index = 20'd0;
    reg [127:0] front = 128'd0;
    (* ramstyle = "M10K" *) reg [127:0] fifo [0:255];
    wire accepting = read && !waitrequest;
    wire returning = readdatavalid && outstanding != 9'd0;
    wire push = returning && active && enable && !rst;
    wire consume = pixel_valid && pixel_ready;
    wire pop = consume && lane == 2'd3;
    wire [17:0] remaining = FRAME_BEATS - issued;
    wire [7:0] next_burst = remaining >= 18'd128 ? 8'd128 : remaining[7:0];
    wire [27:0] slot_base = WINDOW_BASE[31:4] + (selected_slot ? 28'h0040000 : 28'd0);
    assign pixel_valid = active && enable && !rst && count != 9'd0;
    assign pixel_bgrx = front[{lane, 5'b00000} +: 32];
    assign pixel_index = index;
    assign idle = !active && !read && outstanding == 9'd0 && count == 9'd0;

    always @(posedge clk) begin
        done <= 1'b0;
        // Never withdraw a command under waitrequest, even on cancellation.
        if (accepting) begin
            read <= 1'b0;
            outstanding <= {1'b0, burstcount};
            issued <= issued + {10'd0, burstcount};
        end
        if (returning)
            outstanding <= outstanding - 9'd1;

        if (push) begin
            fifo[tail] <= readdata;
            tail <= tail + 8'd1;
            if (count == 9'd0 || (count == 9'd1 && pop))
                front <= readdata;
        end
        if (pop) begin
            head <= head + 8'd1;
            if (count > 9'd1)
                front <= fifo[head + 8'd1];
        end
        case ({push, pop})
            2'b10: count <= count + 9'd1;
            2'b01: count <= count - 9'd1;
            default: begin end
        endcase
        if (consume) begin
            lane <= lane + 2'd1;
            index <= index + 20'd1;
            if (index == LAST_PIXEL) begin
                active <= 1'b0;
                done <= 1'b1;
            end
        end
        if (!enable || rst) begin
            active <= 1'b0;
            count <= 9'd0;
            head <= 8'd0;
            tail <= 8'd0;
            lane <= 2'd0;
        end else if (start && idle && WINDOW_BASE != 32'd0 && WINDOW_BASE[3:0] == 4'd0) begin
            active <= 1'b1;
            selected_slot <= slot;
            issued <= 18'd0;
            index <= 20'd0;
            lane <= 2'd0;
        end
        // Reserve FIFO space for the entire next burst. One burst is in flight.
        if (active && enable && !rst && !read && outstanding == 9'd0 &&
            issued < FRAME_BEATS && count <= 9'd128) begin
            read <= 1'b1;
            address <= slot_base + {10'd0, issued};
            burstcount <= next_burst;
        end
    end
endmodule
