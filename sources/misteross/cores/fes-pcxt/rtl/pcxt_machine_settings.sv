// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
// Per-game persistent settings for fes.pcxt.machine 1.0 (opcodes 24..27).
// Words: 0 video_card (0 CGA, 1 MDA, 2 Hercules), 1 mono_tint (0 white,
// 1 green, 2 amber), 2..3 reserved (0). Settings commit while held
// (exec_reset == 1) and apply before the next execution release.
module pcxt_machine_settings (
    input  wire        clk,
    input  wire        exec_reset,
    input  wire        execution_accepted,
    input  wire        request,
    input  wire [6:0]  opcode,
    input  wire [7:0]  index,
    input  wire [15:0] argument,
    output reg         response_valid,
    output reg         response_error,
    output reg  [15:0] response_data,
    output reg  [1:0]  video_card,
    output reg  [1:0]  mono_tint
);
    localparam [15:0] E_OPCODE = 16'(`FES_COMPUTER_ERROR_INVALID_OPCODE);
    localparam [15:0] E_INDEX = 16'(`FES_COMPUTER_ERROR_INVALID_INDEX);
    localparam [15:0] E_ARGUMENT = 16'(`FES_COMPUTER_ERROR_INVALID_ARGUMENT);
    localparam [15:0] E_STATE = 16'(`FES_COMPUTER_ERROR_INVALID_STATE);
    localparam [7:0] WORDS = 8'(`FES_COMPUTER_PCXT_MACHINE_WORD_COUNT);

    reg replied, staging, snapshot;
    reg [3:0] staged;
    reg [15:0] stage_w [0:3];
    reg [15:0] snap_w [0:3];
    wire fresh = request && !replied;
    wire settings_valid =
        stage_w[0] <= 16'(`FES_COMPUTER_PCXT_VIDEO_CARD_MAX) &&
        stage_w[1] <= 16'(`FES_COMPUTER_PCXT_MONO_TINT_MAX) &&
        stage_w[2] == 16'd0 && stage_w[3] == 16'd0;

    task reply;
        input error;
        input [15:0] data;
        begin
            response_valid <= 1'b1;
            response_error <= error;
            response_data <= data;
            replied <= 1'b1;
        end
    endtask

    integer init_i;
    initial begin
        response_valid = 1'b0;
        response_error = 1'b0;
        response_data = 16'd0;
        replied = 1'b0;
        staging = 1'b0;
        snapshot = 1'b0;
        staged = 4'd0;
        video_card = 2'd0;
        mono_tint = 2'd0;
        for (init_i = 0; init_i < 4; init_i = init_i + 1) begin
            stage_w[init_i] = 16'd0;
            snap_w[init_i] = 16'd0;
        end
    end

    always @(posedge clk) begin
        response_valid <= 1'b0;
        if (!request) replied <= 1'b0;

        if (execution_accepted) begin
            staging <= 1'b0;
            if (exec_reset) snapshot <= 1'b0;
        end

        if (fresh) begin
            case (opcode)
                7'(`FES_COMPUTER_OPCODE_DATA_CONTROL): begin
                    if (index != 8'd0) reply(1'b1, E_INDEX);
                    else case (argument)
                        16'(`FES_COMPUTER_DATA_FREEZE): begin
                            if (exec_reset) reply(1'b1, E_STATE);
                            else if (snapshot) reply(1'b0, 16'd0);
                            else begin
                                snap_w[0] <= {14'd0, video_card};
                                snap_w[1] <= {14'd0, mono_tint};
                                snap_w[2] <= 16'd0;
                                snap_w[3] <= 16'd0;
                                snapshot <= 1'b1;
                                reply(1'b0, 16'd0);
                            end
                        end
                        16'(`FES_COMPUTER_DATA_BEGIN): begin
                            if (!exec_reset) reply(1'b1, E_STATE);
                            else begin
                                staging <= 1'b1;
                                staged <= 4'd0;
                                reply(1'b0, 16'd0);
                            end
                        end
                        16'(`FES_COMPUTER_DATA_COMMIT): begin
                            if (!exec_reset || !staging || staged != 4'b1111) reply(1'b1, E_STATE);
                            else if (!settings_valid) reply(1'b1, E_ARGUMENT);
                            else begin
                                video_card <= stage_w[0][1:0];
                                mono_tint <= stage_w[1][1:0];
                                staging <= 1'b0;
                                reply(1'b0, 16'd0);
                            end
                        end
                        16'(`FES_COMPUTER_DATA_RESUME): begin
                            if (!snapshot) reply(1'b1, E_STATE);
                            else begin
                                snapshot <= 1'b0;
                                reply(1'b0, 16'd0);
                            end
                        end
                        default: reply(1'b1, E_ARGUMENT);
                    endcase
                end
                7'(`FES_COMPUTER_OPCODE_DATA_READ): begin
                    if (index >= WORDS) reply(1'b1, E_INDEX);
                    else if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                    else if (!snapshot) reply(1'b1, E_STATE);
                    else reply(1'b0, snap_w[index[1:0]]);
                end
                7'(`FES_COMPUTER_OPCODE_DATA_WRITE): begin
                    if (index >= WORDS) reply(1'b1, E_INDEX);
                    else if (!exec_reset || !staging) reply(1'b1, E_STATE);
                    else begin
                        stage_w[index[1:0]] <= argument;
                        staged[index[1:0]] <= 1'b1;
                        reply(1'b0, 16'd0);
                    end
                end
                7'(`FES_COMPUTER_OPCODE_DATA_INFO): begin
                    if (index > 8'd3) reply(1'b1, E_INDEX);
                    else if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                    else case (index[1:0])
                        2'd0: reply(1'b0, {8'd0, WORDS});
                        2'd1: reply(1'b0, 16'(`FES_COMPUTER_PCXT_MACHINE_TAG));
                        2'd2: reply(1'b0, 16'd1);
                        default: reply(1'b0, 16'd0);
                    endcase
                end
                default: reply(1'b1, E_OPCODE);
            endcase
        end
    end
endmodule
