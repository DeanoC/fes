// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
// MenuBlit requests for fes.video.menu-blitter 1.1. Index 0 stages the first
// command of the list region, index 1 the count, and index 2 runs them into
// slot 0 or 1; its ACK means the blitter started. Staging follows
// MenuSubmit: in order, only while the menu is enabled and the blitter is
// idle and unfaulted; an invalid request keeps the staged fields and a run
// or a disable clears them. Index 3 reads the state, index 4 the completed
// run count.
module fes_menu_blit_control (
    input wire clk, request, enable, idle, faulted,
    input wire [2:0] fault_code,
    input wire [15:0] completed,
    input wire [7:0] index,
    input wire [15:0] argument,
    output reg response_valid = 1'b0,
    output reg response_error = 1'b0,
    output reg [15:0] response_data = 16'd0,
    output reg run_valid = 1'b0,
    output reg [31:0] list_offset = 32'd0,
    output reg [15:0] list_count = 16'd0,
    output reg target_slot = 1'b0
);
    localparam [31:0] COMMANDS = `FES_APPLICATION_MENU_BLIT_COMMAND_COUNT;
    reg [1:0] stage_count = 2'd0;
    reg [15:0] first = 16'd0;
    wire [16:0] end_command = {1'b0, first} + {1'b0, argument};
    task reject;
        input [15:0] code;
        begin response_valid <= 1'b1; response_error <= 1'b1; response_data <= code; end
    endtask
    always @(posedge clk) begin
        run_valid <= 1'b0;
        if (!enable) stage_count <= 2'd0;
        if (response_valid) begin
            if (!request) response_valid <= 1'b0;
        end else if (request) begin
            response_valid <= 1'b1; response_error <= 1'b0; response_data <= 16'd0;
            if (index > 8'(`FES_APPLICATION_MENU_BLIT_COMPLETED_INDEX)) reject(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
            else if (index == 8'(`FES_APPLICATION_MENU_BLIT_STATE_INDEX) || index == 8'(`FES_APPLICATION_MENU_BLIT_COMPLETED_INDEX)) begin
                if (argument != 16'd0) reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                else if (index == 8'(`FES_APPLICATION_MENU_BLIT_STATE_INDEX))
                    response_data <= {5'd0, faulted ? fault_code : 3'd0, 6'd0, faulted, !idle};
                else response_data <= completed;
            end
            else if (index == 8'(`FES_APPLICATION_MENU_BLIT_FIRST_INDEX) && {16'd0, argument} >= COMMANDS)
                reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
            else if (index == 8'(`FES_APPLICATION_MENU_BLIT_RUN_INDEX) && argument > 16'd1)
                reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
            else if (!enable || !idle || faulted || index != {6'd0, stage_count})
                reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
            else if (index == 8'(`FES_APPLICATION_MENU_BLIT_FIRST_INDEX)) begin
                first <= argument; stage_count <= 2'd1;
            end else if (index == 8'(`FES_APPLICATION_MENU_BLIT_COUNT_INDEX)) begin
                if (argument == 16'd0 || {15'd0, end_command} > COMMANDS) reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                else begin list_count <= argument; stage_count <= 2'd2; end
            end else begin
                run_valid <= 1'b1; target_slot <= argument[0]; stage_count <= 2'd0;
                list_offset <= `FES_APPLICATION_MENU_BLIT_LIST_OFFSET + {11'd0, first, 5'd0};
            end
        end
    end
endmodule
