// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
module fes_menu_control #(
    // A broken session plane can be disabled without resetting its machine.
    // Idle firmware retains its original fault containment behavior.
    parameter [0:0] FAULT_QUIESCE_ACK = 1'b0
) (
    input wire clk, request, exec_reset, drained, faulted, submit_ready,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    input wire [31:0] displayed_sequence, underflows,
    output reg response_valid = 1'b0,
    output reg response_error = 1'b0,
    output reg [15:0] response_data = 16'd0,
    output reg enable = 1'b0,
    output reg quiesce = 1'b1,
    output reg submit_valid = 1'b0,
    output reg submit_slot = 1'b0,
    output reg [31:0] submit_sequence = 32'd0
);
    reg configured = 1'b0, pending = 1'b0;
    reg [31:0] accepted = 32'd0;
    reg [1:0] stage_count = 2'd0;
    reg [31:0] stage_sequence = 32'd0;
    reg wait_drain = 1'b0, wait_submit = 1'b0;
    reg sequence_snapshot_valid = 1'b0, underflow_snapshot_valid = 1'b0;
    reg [15:0] sequence_snapshot = 16'd0, underflow_snapshot = 16'd0;
    wire [15:0] state_word = {11'd0, faulted, drained, pending, enable, configured};
    task reject;
        input [15:0] code;
        begin response_valid <= 1'b1; response_error <= 1'b1; response_data <= code; end
    endtask
    always @(posedge clk) begin
        if (pending && displayed_sequence == accepted) pending <= 1'b0;
        if (response_valid) begin
            if (!request) response_valid <= 1'b0;
        end else if (wait_drain) begin
            if (drained && (!faulted || FAULT_QUIESCE_ACK)) begin
                enable <= 1'b0; pending <= 1'b0; stage_count <= 2'd0;
                wait_drain <= 1'b0; response_valid <= 1'b1;
            end
        end else if (wait_submit) begin
            if (faulted) begin
                submit_valid <= 1'b0; wait_submit <= 1'b0;
                reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
            end else if (submit_ready) begin
                submit_valid <= 1'b0; wait_submit <= 1'b0;
                accepted <= submit_sequence; pending <= 1'b1; stage_count <= 2'd0;
                response_valid <= 1'b1;
            end
        end else if (request) begin
            response_valid <= 1'b1; response_error <= 1'b0; response_data <= 16'd0;
            case ({25'd0,opcode})
                `FES_APPLICATION_OPCODE_MENU_INFO: begin
                    if (index > 8'(`FES_APPLICATION_MENU_INFO_UNDERFLOWS_HI_INDEX))
                        reject(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (argument != 16'd0) reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else case ({24'd0,index})
                        `FES_APPLICATION_MENU_INFO_WIDTH_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_WIDTH);
                        `FES_APPLICATION_MENU_INFO_HEIGHT_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_HEIGHT);
                        `FES_APPLICATION_MENU_INFO_STRIDE_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_STRIDE);
                        `FES_APPLICATION_MENU_INFO_FRAME_BYTES_LO_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_FRAME_BYTES);
                        `FES_APPLICATION_MENU_INFO_FRAME_BYTES_HI_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_FRAME_BYTES >> 16);
                        `FES_APPLICATION_MENU_INFO_SLOT_BYTES_LO_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_SLOT_BYTES);
                        `FES_APPLICATION_MENU_INFO_SLOT_BYTES_HI_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_SLOT_BYTES >> 16);
                        `FES_APPLICATION_MENU_INFO_PIXEL_FORMAT_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_PIXEL_FORMAT);
                        `FES_APPLICATION_MENU_INFO_SLOT_COUNT_INDEX: response_data <= 16'(`FES_APPLICATION_MENU_SLOT_COUNT);
                        `FES_APPLICATION_MENU_INFO_STATE_INDEX: response_data <= state_word;
                        `FES_APPLICATION_MENU_INFO_SEQUENCE_LO_INDEX: begin
                            response_data <= displayed_sequence[15:0]; sequence_snapshot <= displayed_sequence[31:16]; sequence_snapshot_valid <= 1'b1;
                        end
                        `FES_APPLICATION_MENU_INFO_SEQUENCE_HI_INDEX: begin
                            if (!sequence_snapshot_valid) reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                            else begin response_data <= sequence_snapshot; sequence_snapshot_valid <= 1'b0; end
                        end
                        `FES_APPLICATION_MENU_INFO_UNDERFLOWS_LO_INDEX: begin
                            response_data <= underflows[15:0]; underflow_snapshot <= underflows[31:16]; underflow_snapshot_valid <= 1'b1;
                        end
                        `FES_APPLICATION_MENU_INFO_UNDERFLOWS_HI_INDEX: begin
                            if (!underflow_snapshot_valid) reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                            else begin response_data <= underflow_snapshot; underflow_snapshot_valid <= 1'b0; end
                        end
                        default: reject(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    endcase
                end
                `FES_APPLICATION_OPCODE_MENU_CONFIGURE: begin
                    if (index != 0) reject(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (argument != 16'(`FES_APPLICATION_MENU_LAYOUT)) reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (enable || !drained || faulted) reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else configured <= 1'b1;
                end
                `FES_APPLICATION_OPCODE_MENU_CONTROL: begin
                    if (index != 0) reject(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (argument == 16'(`FES_APPLICATION_MENU_CONTROL_QUIESCE)) begin
                        quiesce <= 1'b1; wait_drain <= 1'b1; response_valid <= 1'b0;
                    end else if (argument != 16'(`FES_APPLICATION_MENU_CONTROL_ENABLE)) reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (!configured || exec_reset || faulted) reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else begin enable <= 1'b1; quiesce <= 1'b0; end
                end
                `FES_APPLICATION_OPCODE_MENU_SUBMIT: begin
                    if (index > 8'(`FES_APPLICATION_MENU_SUBMIT_COMMIT_INDEX)) reject(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (index == 8'(`FES_APPLICATION_MENU_SUBMIT_COMMIT_INDEX) && argument > 1) reject(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (!enable || pending || faulted || quiesce || index != {6'd0,stage_count}) reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if (index == 8'(`FES_APPLICATION_MENU_SUBMIT_SEQUENCE_LO_INDEX)) begin stage_sequence[15:0] <= argument; stage_count <= 2'd1; end
                    else if (index == 8'(`FES_APPLICATION_MENU_SUBMIT_SEQUENCE_HI_INDEX)) begin stage_sequence[31:16] <= argument; stage_count <= 2'd2; end
                    else if (stage_sequence == 0 || stage_sequence <= accepted) reject(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else begin
                        submit_valid <= 1'b1; submit_slot <= argument[0]; submit_sequence <= stage_sequence;
                        wait_submit <= 1'b1; response_valid <= 1'b0;
                    end
                end
                default: reject(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
            endcase
        end
    end
endmodule
