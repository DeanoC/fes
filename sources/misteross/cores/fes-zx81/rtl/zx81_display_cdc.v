// SPDX-License-Identifier: GPL-2.0-or-later
// One held GP command crosses to the pixel clock with a toggle handshake.
// Payload registers stay stable until the synchronized acknowledgement returns;
// neither clock samples a changing multi-bit bus. No execution reset is used.
module zx81_display_cdc (
    input wire clk_sys, pixel_clk,
    input wire request,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    output reg response_valid = 1'b0,
    output reg response_error = 1'b0,
    output reg [15:0] response_data = 16'd0,
    output reg pixel_request = 1'b0,
    output reg [6:0] pixel_opcode = 7'd0,
    output reg [7:0] pixel_index = 8'd0,
    output reg [15:0] pixel_argument = 16'd0,
    input wire pixel_response_valid, pixel_response_error,
    input wire [15:0] pixel_response_data
);
    reg request_tag = 1'b0, busy = 1'b0;
    reg [30:0] payload = 31'd0;
    reg response_tag = 1'b0;
    reg [16:0] returned_payload = 17'd0;
    (* async_reg = "true" *) reg [1:0] request_sync = 2'd0;
    (* async_reg = "true" *) reg [1:0] response_sync = 2'd0;
    always @(posedge clk_sys) begin
        response_sync <= {response_sync[0], response_tag};
        if (!request) response_valid <= 1'b0;
        if (request && !busy && !response_valid) begin
            payload <= {opcode, index, argument};
            request_tag <= ~request_tag;
            busy <= 1'b1;
        end else if (busy && response_sync[1] == request_tag) begin
            response_valid <= 1'b1;
            response_error <= returned_payload[16];
            response_data <= returned_payload[15:0];
            busy <= 1'b0;
        end
    end
    always @(posedge pixel_clk) begin
        request_sync <= {request_sync[0], request_tag};
        if (pixel_request && pixel_response_valid) begin
            returned_payload <= {pixel_response_error, pixel_response_data};
            response_tag <= request_sync[1];
            pixel_request <= 1'b0;
        end else if (!pixel_request && !pixel_response_valid &&
                     request_sync[1] != response_tag) begin
            {pixel_opcode, pixel_index, pixel_argument} <= payload;
            pixel_request <= 1'b1;
        end
    end
endmodule
