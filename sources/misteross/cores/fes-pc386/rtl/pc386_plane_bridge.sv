// SPDX-License-Identifier: MIT
// The display plane's DDR reads on port 2 (docs/pc386-machine-design.md,
// "HPS DDR window and ports"). fes_display_plane reads 128-bit beats in the
// pixel clock with port-0 word addresses (byte address / 16); port 2 is
// 64-bit in the system clock. The bridge crosses the plane's commands into
// the system clock, doubles addresses and burst lengths, splits a burst into
// commands of at most 128 port beats, pairs the returned beats and crosses
// them back.
//
// It also stands in for the port guard the plane would have on its own port:
// ddr_hold resets only the plane's side (ddr_reset, synchronized to the pixel
// clock), the bridge stops taking commands, still finishes every port beat it
// asked for and hides the data, and ddr_drained says when nothing it accepted
// is outstanding. The port itself, shared with capture and the presenter, is
// never held.
module pc386_plane_bridge (
    input wire clk, reset,             // system clock
    input wire pixel_clk, pixel_reset,
    // Plane side, pixel clock
    input wire ddr_hold,
    output wire ddr_reset, ddr_drained,
    input wire [27:0] plane_address,
    input wire [7:0] plane_burstcount,
    input wire plane_read,
    output wire plane_waitrequest,
    output wire [127:0] plane_readdata,
    output wire plane_readdatavalid,
    // Port 2 side, system clock
    output reg [28:0] port_address,
    output reg [7:0] port_burstcount,
    output reg port_read,
    input wire port_waitrequest,
    input wire [63:0] port_readdata,
    input wire port_readdatavalid
);
    (* async_reg = "true" *) reg [1:0] hold_sync;
    always @(posedge pixel_clk) hold_sync <= pixel_reset ? 2'b11 : {hold_sync[0], ddr_hold};
    assign ddr_reset = hold_sync[1];

    // Commands: pixel clock to system clock.
    wire command_full, command_empty;
    wire [35:0] command_head;
    wire command_take = plane_read && !plane_waitrequest;
    assign plane_waitrequest = command_full || ddr_reset;
    reg command_pop;
    pc386_async_fifo #(.WIDTH(36), .DEPTH(4)) commands (
        .wclk(pixel_clk), .wreset(pixel_reset), .push(command_take), .din({plane_address, plane_burstcount}),
        .full(command_full), .rclk(clk), .rreset(reset), .pop(command_pop), .dout(command_head),
        .empty(command_empty)
    );

    // Issue: each plane beat is two port beats; at most 128 port beats a command.
    reg issuing;
    reg [28:0] next_address;
    reg [8:0] beats_left;
    wire [8:0] chunk = beats_left > 9'd128 ? 9'd128 : beats_left;
    always @(posedge clk) begin
        command_pop <= 0;
        if (reset) begin
            issuing <= 0; next_address <= 0; beats_left <= 0;
            port_read <= 0; port_address <= 0; port_burstcount <= 0;
        end else begin
            if (port_read && !port_waitrequest) port_read <= 0;
            if (!issuing && !command_empty && !command_pop) begin
                issuing <= 1;
                command_pop <= 1;
                next_address <= {command_head[35:8], 1'b0};
                beats_left <= {command_head[7:0], 1'b0};
            end else if (issuing && (!port_read || !port_waitrequest)) begin
                if (beats_left == 0) issuing <= 0;
                else begin
                    port_read <= 1;
                    port_address <= next_address;
                    port_burstcount <= chunk[7:0];
                    next_address <= next_address + {20'b0, chunk};
                    beats_left <= beats_left - chunk;
                end
            end
        end
    end

    // Responses: pair beats, low beat first, and cross to the pixel clock. The
    // plane reads one burst at a time, so the FIFO only needs a little slack;
    // it drains faster than port beats arrive.
    reg pair;
    reg [63:0] low;
    reg response_push;
    reg [127:0] response;
    always @(posedge clk) begin
        response_push <= 0;
        if (reset) begin pair <= 0; low <= 0; response <= 0; end
        else if (port_readdatavalid) begin
            if (pair) begin response_push <= 1; response <= {port_readdata, low}; end
            else low <= port_readdata;
            pair <= !pair;
        end
    end
    wire response_empty, response_full;
    pc386_async_fifo #(.WIDTH(128), .DEPTH(16)) responses (
        .wclk(clk), .wreset(reset), .push(response_push), .din(response), .full(response_full),
        .rclk(pixel_clk), .rreset(pixel_reset), .pop(!response_empty), .dout(plane_readdata),
        .empty(response_empty)
    );
    // Outstanding plane beats, counted in the pixel clock: added when the plane
    // hands over a command, removed as pairs come back, delivered or hidden.
    reg [9:0] outstanding;
    always @(posedge pixel_clk) begin
        if (pixel_reset) outstanding <= 0;
        else outstanding <= outstanding + (command_take ? {2'b0, plane_burstcount} : 10'd0)
                                        - {9'b0, !response_empty};
    end
    assign plane_readdatavalid = !response_empty && !ddr_reset;
    assign ddr_drained = outstanding == 0 && !command_take;
    wire unused = response_full;
endmodule
