// SPDX-License-Identifier: GPL-2.0-or-later
// Read-only frame fetcher. WINDOW_BASE is supplied by the shared DDR contract
// at board integration; the zero default deliberately cannot select HPS RAM.
module fes_menu_reader #(
    parameter [31:0] WINDOW_BASE = 32'd0,
    parameter [0:0] DYNAMIC_BASE = 1'b0,
    parameter [7:0] MAX_BURST_BEATS = 8'd128,
    parameter [0:0] CHECK_RESPONSE = 1'b0,
    // DE25's short F2SDRAM bursts need independent requests to hide latency.
    // Defaults retain the existing single-burst behavior on other platforms.
    parameter [0:0] READ_AHEAD = 1'b0,
    parameter [0:0] RESET_ABORTS_TRAFFIC = 1'b0
) (
    input wire clk,
    input wire rst,
    input wire enable,
    input wire slot,
    input wire start,
    input wire [31:0] window_base,
    input wire window_base_valid,
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
    input wire readdatavalid,
    input wire [1:0] response,
    output wire [1:0] memory_errors,
    output wire [8:0] outstanding_beats
);
    localparam [17:0] FRAME_BEATS = 18'd230400;
    localparam [19:0] LAST_PIXEL = 20'd921599;
    reg active = 1'b0;
    reg selected_slot = 1'b0;
    reg [31:0] selected_window_base = WINDOW_BASE;
    reg [1:0] latched_memory_errors = 2'b00;
    reg [17:0] issued = 18'd0;
    reg [8:0] outstanding = 9'd0;
    reg [8:0] count = 9'd0;
    reg [7:0] head = 8'd0;
    reg [7:0] tail = 8'd0;
    reg [1:0] lane = 2'd0;
    reg [19:0] index = 20'd0;
    reg [127:0] front = 128'd0;
    (* ramstyle = "M10K" *) reg [127:0] fifo [0:255];
    wire [7:0] next_head = head + 8'd1;
    reg [127:0] fifo_peek;
    reg [127:0] last_write_data;
    reg [7:0] last_write_address;
    reg last_write_valid = 1'b0;
    wire reset_abort = rst && RESET_ABORTS_TRAFFIC;
    // Keep the RAM read register unconditional for synchronous M10K inference.
    // Forward the most recent write when it collided with the look-ahead read.
    always @(posedge clk) begin
        fifo_peek <= fifo[next_head];
        if (reset_abort) begin
            last_write_valid <= 1'b0;
        end else if (push) begin
            last_write_data <= readdata;
            last_write_address <= tail;
            last_write_valid <= 1'b1;
        end
    end
    wire accepting = !reset_abort && read && !waitrequest;
    // A first response may arrive on the command-acceptance edge. Include
    // that burst before subtracting the response; never overwrite its budget.
    wire [9:0] response_budget = {1'b0, outstanding} +
        (accepting ? {2'b00, burstcount} : 10'd0);
    wire returning = !reset_abort && readdatavalid && response_budget != 10'd0;
    wire [1:0] new_memory_errors = reset_abort || !CHECK_RESPONSE ? 2'b00 :
        {readdatavalid && response_budget == 10'd0, returning && response != 2'b00};
    assign memory_errors = CHECK_RESPONSE ? latched_memory_errors | new_memory_errors : 2'b00;
    wire memory_fault = |memory_errors;
    wire push = returning && active && enable && !rst && !memory_fault;
    wire consume = pixel_valid && pixel_ready;
    wire pop = consume && lane == 2'd3;
    wire [17:0] remaining = FRAME_BEATS - issued;
    // Returning beats move from outstanding to FIFO count without consuming
    // another reservation. Reserve each entire command before asserting read.
    wire [9:0] reserved_beats = {1'b0, count} + {1'b0, outstanding};
    wire [27:0] slot_base = selected_window_base[31:4] + (selected_slot ? 28'h0040000 : 28'd0);
    wire [27:0] next_address = slot_base + {10'd0, issued};
    wire [8:0] page_beats = 9'd256 - {1'b0, next_address[7:0]};
    wire [8:0] burst_limit = page_beats < {1'b0, MAX_BURST_BEATS} ?
                            page_beats : {1'b0, MAX_BURST_BEATS};
    wire [7:0] next_burst = remaining >= {9'd0, burst_limit} ? burst_limit[7:0] : remaining[7:0];
    wire [32:0] dynamic_window_end = {1'b0, window_base} + 33'h000800000;
    wire window_valid = DYNAMIC_BASE ? window_base_valid && window_base[4:0] == 5'd0 &&
        dynamic_window_end <= 33'h100000000 : WINDOW_BASE != 32'd0 && WINDOW_BASE[3:0] == 4'd0;
    assign pixel_valid = active && enable && !rst && !memory_fault && count != 9'd0;
    assign pixel_bgrx = front[{lane, 5'b00000} +: 32];
    assign pixel_index = index;
    assign idle = !active && !read && outstanding == 9'd0 && count == 9'd0;
    // Include a command held under waitrequest; zero never means that a
    // stalled request may be withdrawn or its backing allocation released.
    assign outstanding_beats = outstanding + (read ? {1'b0, burstcount} : 9'd0);

    always @(posedge clk) begin
        if (reset_abort) begin
            active <= 1'b0;
            selected_slot <= 1'b0;
            selected_window_base <= WINDOW_BASE;
            latched_memory_errors <= 2'b00;
            issued <= 18'd0;
            outstanding <= 9'd0;
            count <= 9'd0;
            head <= 8'd0;
            tail <= 8'd0;
            lane <= 2'd0;
            index <= 20'd0;
            front <= 128'd0;
            address <= 28'd0;
            burstcount <= 8'd0;
            read <= 1'b0;
            done <= 1'b0;
        end else begin
            done <= 1'b0;
            // Never withdraw a command under waitrequest, even on cancellation.
            if (accepting) begin
                read <= 1'b0;
                issued <= issued + {10'd0, burstcount};
            end
            if (accepting || returning)
                outstanding <= response_budget[8:0] - (returning ? 9'd1 : 9'd0);
            if (new_memory_errors != 2'b00)
                latched_memory_errors <= latched_memory_errors | new_memory_errors;
            else if (rst && idle)
                latched_memory_errors <= 2'b00;

            if (push) begin
                fifo[tail] <= readdata;
                tail <= tail + 8'd1;
                if (count == 9'd0 || (count == 9'd1 && pop))
                    front <= readdata;
            end
            if (pop) begin
                head <= head + 8'd1;
                if (count > 9'd1)
                    front <= last_write_valid && last_write_address == next_head ?
                        last_write_data : fifo_peek;
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
            if (!enable || rst || memory_fault) begin
                active <= 1'b0;
                count <= 9'd0;
                head <= 8'd0;
                tail <= 8'd0;
                lane <= 2'd0;
            end else if (start && idle && window_valid) begin
                active <= 1'b1;
                selected_slot <= slot;
                selected_window_base <= DYNAMIC_BASE ? window_base : WINDOW_BASE;
                issued <= 18'd0;
                index <= 20'd0;
                lane <= 2'd0;
            end
            // A held command cannot be replaced. In read-ahead mode all
            // accepted commands plus this command still fit in the 256-beat
            // FIFO, including cancellation while responses remain in flight.
            if (active && enable && !rst && !memory_fault && !read && (READ_AHEAD || outstanding == 9'd0) &&
                issued < FRAME_BEATS && next_burst != 8'd0 && MAX_BURST_BEATS <= 8'd128 &&
                (READ_AHEAD ? reserved_beats <= 10'd256 - {2'b00, next_burst} :
                              count <= 9'd256 - {1'b0, next_burst})) begin
                read <= 1'b1;
                address <= next_address;
                burstcount <= next_burst;
            end
        end
    end
endmodule
