// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_gp.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.simple-game 1.0.
// The host holds all payload bits stable before and after changing request.
module fes_gp (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output reg          game_reset,
    output reg  [7:0]   buttons
);
    localparam [31:0] CAPABILITIES =
        `FES_GP_INTERFACE_GAMEPAD_CAPABILITY_MASK |
        `FES_GP_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK;
    localparam [31:0] ID_MAGIC0_INDEX = `FES_GP_IDENTITY_MAGIC0_INDEX;
    localparam [31:0] ID_MAGIC1_INDEX = `FES_GP_IDENTITY_MAGIC1_INDEX;
    localparam [31:0] ID_TRANSPORT_MAJOR_INDEX = `FES_GP_IDENTITY_TRANSPORT_MAJOR_INDEX;
    localparam [31:0] ID_TRANSPORT_MINOR_INDEX = `FES_GP_IDENTITY_TRANSPORT_MINOR_INDEX;
    localparam [31:0] ID_ABI_TAG_INDEX = `FES_GP_IDENTITY_ABI_TAG_INDEX;
    localparam [31:0] ID_ABI_MAJOR_INDEX = `FES_GP_IDENTITY_ABI_MAJOR_INDEX;
    localparam [31:0] ID_ABI_MINOR_INDEX = `FES_GP_IDENTITY_ABI_MINOR_INDEX;
    localparam [31:0] ID_CAPABILITIES_INDEX = `FES_GP_IDENTITY_CAPABILITIES_INDEX;
    localparam [31:0] ID_BUILD_START_INDEX = `FES_GP_IDENTITY_BUILD_IDSTART_INDEX;
    localparam [31:0] ID_MAGIC0 = `FES_GP_IDENTITY_MAGIC0;
    localparam [31:0] ID_MAGIC1 = `FES_GP_IDENTITY_MAGIC1;
    localparam [31:0] TRANSPORT_MAJOR = `FES_GP_TRANSPORT_MAJOR;
    localparam [31:0] TRANSPORT_MINOR = `FES_GP_TRANSPORT_MINOR;
    localparam [31:0] ABI_TAG = `FES_GP_ABI_TAG;
    localparam [31:0] ABI_MAJOR = `FES_GP_ABI_MAJOR;
    localparam [31:0] ABI_MINOR = `FES_GP_ABI_MINOR;
    localparam [31:0] ERROR_INVALID_OPCODE = `FES_GP_ERROR_INVALID_OPCODE;
    localparam [31:0] ERROR_INVALID_INDEX = `FES_GP_ERROR_INVALID_INDEX;
    localparam [31:0] ERROR_INVALID_ARGUMENT = `FES_GP_ERROR_INVALID_ARGUMENT;

    (* async_reg = "true" *) reg request_meta;
    (* async_reg = "true" *) reg request_sync;
    reg acknowledged_toggle;
    reg response_error;
    reg [15:0] response_data;

    wire request_toggle = (gpo & `FES_GP_REQUEST_MASK) != 32'h00000000;
    wire [31:0] command_opcode = (gpo & `FES_GP_OPCODE_MASK) >> 24;
    wire [31:0] command_index = (gpo & `FES_GP_INDEX_MASK) >> 16;
    wire [31:0] command_argument = gpo & `FES_GP_ARGUMENT_MASK;

    assign gpi = `FES_GP_SIGNATURE |
                 (acknowledged_toggle ? `FES_GP_ACK_MASK : 32'h00000000) |
                 (response_error ? `FES_GP_ERROR_MASK : 32'h00000000) |
                 {16'h0000, response_data};

    function [15:0] identity_word;
        input [31:0] index;
        input [127:0] identity;
        begin
            case (index)
                ID_MAGIC0_INDEX: identity_word = ID_MAGIC0[15:0];
                ID_MAGIC1_INDEX: identity_word = ID_MAGIC1[15:0];
                ID_TRANSPORT_MAJOR_INDEX: identity_word = TRANSPORT_MAJOR[15:0];
                ID_TRANSPORT_MINOR_INDEX: identity_word = TRANSPORT_MINOR[15:0];
                ID_ABI_TAG_INDEX: identity_word = ABI_TAG[15:0];
                ID_ABI_MAJOR_INDEX: identity_word = ABI_MAJOR[15:0];
                ID_ABI_MINOR_INDEX: identity_word = ABI_MINOR[15:0];
                ID_CAPABILITIES_INDEX: identity_word = CAPABILITIES[15:0];
                ID_BUILD_START_INDEX + 0:
                    identity_word = {identity[119:112], identity[127:120]};
                ID_BUILD_START_INDEX + 1:
                    identity_word = {identity[103:96], identity[111:104]};
                ID_BUILD_START_INDEX + 2:
                    identity_word = {identity[87:80], identity[95:88]};
                ID_BUILD_START_INDEX + 3:
                    identity_word = {identity[71:64], identity[79:72]};
                ID_BUILD_START_INDEX + 4:
                    identity_word = {identity[55:48], identity[63:56]};
                ID_BUILD_START_INDEX + 5:
                    identity_word = {identity[39:32], identity[47:40]};
                ID_BUILD_START_INDEX + 6:
                    identity_word = {identity[23:16], identity[31:24]};
                ID_BUILD_START_INDEX + 7:
                    identity_word = {identity[7:0], identity[15:8]};
                default: identity_word = 16'h0000;
            endcase
        end
    endfunction

    initial begin
        request_meta = 1'b0;
        request_sync = 1'b0;
        acknowledged_toggle = 1'b0;
        response_error = 1'b0;
        response_data = 16'h0000;
        game_reset = 1'b1;
        buttons = 8'h00;
    end

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;

        // By the time the synchronized toggle arrives, the held payload has
        // crossed at least two destination clocks and is sampled as one word.
        if (request_sync != acknowledged_toggle) begin
            acknowledged_toggle <= request_sync;
            response_error <= 1'b0;
            response_data <= 16'h0000;

            case (command_opcode)
                `FES_GP_OPCODE_IDENTITY: begin
                    if (command_index >= `FES_GP_IDENTITY_WORD_COUNT) begin
                        response_error <= 1'b1;
                        response_data <= ERROR_INVALID_INDEX[15:0];
                    end else if (command_argument != 32'h00000000) begin
                        response_error <= 1'b1;
                        response_data <= ERROR_INVALID_ARGUMENT[15:0];
                    end else begin
                        response_data <= identity_word(command_index, build_id);
                    end
                end
                `FES_GP_OPCODE_GAMEPLAY: begin
                    if (command_index != `FES_GP_CONTROL_INDEX) begin
                        response_error <= 1'b1;
                        response_data <= ERROR_INVALID_INDEX[15:0];
                    end else if (command_argument == `FES_GP_GAMEPLAY_HOLD_RESET) begin
                        game_reset <= 1'b1;
                        buttons <= 8'h00;
                    end else if (command_argument == `FES_GP_GAMEPLAY_RELEASE) begin
                        game_reset <= 1'b0;
                    end else begin
                        response_error <= 1'b1;
                        response_data <= ERROR_INVALID_ARGUMENT[15:0];
                    end
                end
                `FES_GP_OPCODE_BUTTONS: begin
                    if (command_index != `FES_GP_CONTROL_INDEX) begin
                        response_error <= 1'b1;
                        response_data <= ERROR_INVALID_INDEX[15:0];
                    end else if ((command_argument & ~`FES_GP_BUTTON_MASK) != 32'h00000000) begin
                        response_error <= 1'b1;
                        response_data <= ERROR_INVALID_ARGUMENT[15:0];
                    end else begin
                        buttons <= command_argument[7:0];
                        response_data <= command_argument[15:0];
                    end
                end
                default: begin
                    response_error <= 1'b1;
                    response_data <= ERROR_INVALID_OPCODE[15:0];
                end
            endcase
        end
    end
endmodule
