// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
`include "fes_firmware_upload.vh"
// fes.fabric.firmware-upload 1.0 (mister-packages docs/firmware-upload.md).
// A GP front end between the board transport and fes_computer_mailbox_core.
// It answers the four firmware opcodes itself, refuses an Execution release
// until a complete image is committed, and passes every other request to the
// mailbox with a toggle of its own, returning the mailbox's reply. The mailbox
// therefore sees an ordinary host. A core holds its machine in reset on
// exec_reset alone; the release cannot be accepted without firmware.
//
// The image is BYTES long exactly. Accepted bytes leave on the write port for
// one clock, low byte at write_addr and the high byte at write_addr+1, as the
// mailbox presents media. The store is a plain synchronous RAM with no
// backpressure. Begin and Data are valid only while execution is held, so the
// running machine never sees a changing ROM. No FPGA-family primitive belongs
// here.
module fes_firmware_upload #(
    parameter integer AW = 14,
    parameter [31:0] BYTES = 32'd16384
) (
    input  wire          clk,
    input  wire [31:0]   gpo,
    output wire [31:0]   gpi,
    output reg  [31:0]   mailbox_gpo = 32'd0,
    input  wire [31:0]   mailbox_gpi,
    input  wire          exec_reset,
    output wire          ready,
    output wire [AW-1:0] write_addr,
    output wire [15:0]   write_data,
    output wire [1:0]    write_enable
);
    localparam [15:0] E_OPCODE = 16'(`FES_COMPUTER_ERROR_INVALID_OPCODE);
    localparam [15:0] E_INDEX = 16'(`FES_COMPUTER_ERROR_INVALID_INDEX);
    localparam [15:0] E_ARGUMENT = 16'(`FES_COMPUTER_ERROR_INVALID_ARGUMENT);
    localparam [15:0] E_STATE = 16'(`FES_COMPUTER_ERROR_INVALID_STATE);
    localparam [1:0] EMPTY = 2'(`FES_FIRMWARE_UPLOAD_STATE_EMPTY);
    localparam [1:0] LOADING = 2'(`FES_FIRMWARE_UPLOAD_STATE_LOADING);
    localparam [1:0] READY = 2'(`FES_FIRMWARE_UPLOAD_STATE_READY);
    localparam [31:0] CRC_INIT = `FES_COMPUTER_MEDIA_CRC32_INITIAL;
    localparam [31:0] CRC_POLY = `FES_COMPUTER_MEDIA_CRC32_POLYNOMIAL;
    localparam [31:0] CRC_XOR = `FES_COMPUTER_MEDIA_CRC32_FINAL_XOR;

    (* async_reg = "true" *) reg request_meta = 1'b0;
    (* async_reg = "true" *) reg request_sync = 1'b0;
    reg acknowledged = 1'b0;
    reg response_error = 1'b0;
    reg [15:0] response_data = 16'd0;
    reg forwarded = 1'b0;
    reg [1:0] state = EMPTY;
    reg armed = 1'b0;
    reg [1:0] begin_next = 2'd0;
    reg [15:0] begin0 = 16'd0, begin1 = 16'd0, begin2 = 16'd0;
    reg [31:0] expected_crc = 32'd0, crc = CRC_INIT, received = 32'd0;

    wire [6:0] opcode = gpo[30:24];
    wire [7:0] index = gpo[23:16];
    wire [15:0] argument = gpo[15:0];
    wire request = request_sync != acknowledged;
    wire mailbox_toggle = (mailbox_gpo & `FES_COMPUTER_REQUEST_MASK) != 32'd0;
    wire mailbox_ack = (mailbox_gpi & `FES_COMPUTER_ACK_MASK) != 32'd0;
    wire [31:0] remaining = BYTES - received;
    wire is_data = opcode == 7'(`FES_FIRMWARE_UPLOAD_OPCODE_DATA);
    wire data_ok = request && !forwarded && is_data && armed && exec_reset &&
                   index == received[8:1] && remaining != 32'd0 &&
                   !(remaining == 32'd1 && argument[15:8] != 8'd0);

    assign ready = state == READY;
    assign write_addr = received[AW-1:0];
    assign write_data = argument;
    assign write_enable = {data_ok && remaining != 32'd1, data_ok};
    assign gpi = `FES_COMPUTER_SIGNATURE |
                 (acknowledged ? `FES_COMPUTER_ACK_MASK : 32'd0) |
                 (response_error ? `FES_COMPUTER_ERROR_MASK : 32'd0) |
                 {16'd0, response_data};

    function automatic [31:0] crc32_byte;
        input [31:0] value;
        input [7:0] data;
        integer i;
        reg [31:0] c;
        begin
            c = value ^ {24'd0, data};
            for (i = 0; i < 8; i = i + 1)
                c = c[0] ? ((c >> 1) ^ CRC_POLY) : (c >> 1);
            crc32_byte = c;
        end
    endfunction

    task reject;
        input [15:0] code;
        begin
            response_error <= 1'b1;
            response_data <= code;
        end
    endtask

    always @(posedge clk) begin
        request_meta <= (gpo & `FES_COMPUTER_REQUEST_MASK) != 32'd0;
        request_sync <= request_meta;
        if (forwarded) begin
            // The mailbox holds its reply stable once it acknowledges.
            if (mailbox_ack == mailbox_toggle) begin
                response_error <= (mailbox_gpi & `FES_COMPUTER_ERROR_MASK) != 32'd0;
                response_data <= mailbox_gpi[15:0];
                acknowledged <= request_sync;
                forwarded <= 1'b0;
            end
        end else if (request) begin
            response_error <= 1'b0;
            response_data <= 16'd0;
            acknowledged <= request_sync;
            case (opcode)
                7'(`FES_FIRMWARE_UPLOAD_OPCODE_INFO): begin
                    if (index > 8'(`FES_FIRMWARE_UPLOAD_INFO_RECEIVED_HIGH)) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else case (index)
                        8'(`FES_FIRMWARE_UPLOAD_INFO_BYTES_LOW): response_data <= BYTES[15:0];
                        8'(`FES_FIRMWARE_UPLOAD_INFO_BYTES_HIGH): response_data <= BYTES[31:16];
                        8'(`FES_FIRMWARE_UPLOAD_INFO_STATE): response_data <= {14'd0, state};
                        8'(`FES_FIRMWARE_UPLOAD_INFO_RECEIVED_LOW): response_data <= received[15:0];
                        default: response_data <= received[31:16];
                    endcase
                end
                7'(`FES_FIRMWARE_UPLOAD_OPCODE_BEGIN): begin
                    if (index > 8'd3) reject(E_INDEX);
                    else if (!exec_reset) reject(E_STATE);
                    else if (index[1:0] != begin_next) reject(E_INDEX);
                    else if (index[1:0] == 2'd3) begin
                        if ({begin1, begin0} != BYTES) reject(E_ARGUMENT);
                        else begin
                            armed <= 1'b1;
                            expected_crc <= {argument, begin2};
                            received <= 32'd0;
                            crc <= CRC_INIT;
                        end
                        // A refused total abandons the header; start at word 0.
                        begin_next <= 2'd0;
                    end else begin
                        begin_next <= begin_next + 2'd1;
                        case (index[1:0])
                            2'd0: begin
                                // The old image is gone from the first word.
                                begin0 <= argument;
                                state <= LOADING;
                                armed <= 1'b0;
                            end
                            2'd1: begin1 <= argument;
                            default: begin2 <= argument;
                        endcase
                    end
                end
                7'(`FES_FIRMWARE_UPLOAD_OPCODE_DATA): begin
                    if (!armed || !exec_reset || remaining == 32'd0) reject(E_STATE);
                    else if (index != received[8:1]) reject(E_INDEX);
                    else if (remaining == 32'd1 && argument[15:8] != 8'd0) reject(E_ARGUMENT);
                    else if (remaining == 32'd1) begin
                        crc <= crc32_byte(crc, argument[7:0]);
                        received <= received + 32'd1;
                    end else begin
                        crc <= crc32_byte(crc32_byte(crc, argument[7:0]), argument[15:8]);
                        received <= received + 32'd2;
                    end
                end
                7'(`FES_FIRMWARE_UPLOAD_OPCODE_COMMIT): begin
                    if (index != 8'd0) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else if (!armed || remaining != 32'd0) reject(E_STATE);
                    else begin
                        // A wrong image needs a new Begin; it is never ready.
                        armed <= 1'b0;
                        if ((crc ^ CRC_XOR) != expected_crc) reject(E_ARGUMENT);
                        else state <= READY;
                    end
                end
                default: begin
                    if (opcode == 7'(`FES_COMPUTER_OPCODE_EXECUTION) && index == 8'd0 &&
                        argument == 16'd1 && state != READY) reject(E_STATE);
                    else begin
                        mailbox_gpo <= {!mailbox_toggle, gpo[30:0]};
                        forwarded <= 1'b1;
                        acknowledged <= acknowledged;
                    end
                end
            endcase
        end
    end
endmodule
