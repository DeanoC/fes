module mailbox_fsm #(
    parameter integer MESSAGE_BYTES = 12,
    parameter [7:0] START_SEQUENCE = 8'h00
) (
    input wire FPGA_CLK1_50,
    input wire [31:0] gpo,
    output wire [31:0] gpi
);

    localparam [31:0] HELLO = 32'hD3100000;
    localparam [31:0] DONE = 32'hD3130C00;
    localparam [31:0] START = 32'hAC100000;
    localparam [31:0] DATA_PREFIX = 32'hD3110000;
    localparam [31:0] END_PREFIX = 32'hD3120000;
    localparam [31:0] DONE_PREFIX = 32'hD3130000;
    localparam [31:0] ACK_PREFIX = 32'hAC000000;
    localparam [95:0] MESSAGE = {8'h4f,8'h53,8'h53,8'h20,8'h46,8'h50,8'h47,8'h41,8'h20,8'h4f,8'h4b,8'h0a};
    localparam [7:0] LAST_BYTE = (MESSAGE_BYTES == 2) ? 8'd1 : 8'd11;

    localparam [1:0] STATE_HELLO = 2'd0;
    localparam [1:0] STATE_DATA = 2'd1;
    localparam [1:0] STATE_END = 2'd2;
    localparam [1:0] STATE_DONE = 2'd3;

    reg [1:0] state = STATE_HELLO;
    reg [7:0] seq = START_SEQUENCE;
    reg [7:0] byte_index = 8'd0;
    reg [7:0] message_byte;
    reg [31:0] current_word;
    reg start_armed = 1'b0;

    initial begin
        state = STATE_HELLO;
        seq = START_SEQUENCE;
        byte_index = 8'd0;
        start_armed = 1'b0;
    end

    always @* begin
        case (byte_index)
            8'd0: message_byte = MESSAGE[95:88];
            8'd1: message_byte = MESSAGE[87:80];
            8'd2: message_byte = MESSAGE[79:72];
            8'd3: message_byte = MESSAGE[71:64];
            8'd4: message_byte = MESSAGE[63:56];
            8'd5: message_byte = MESSAGE[55:48];
            8'd6: message_byte = MESSAGE[47:40];
            8'd7: message_byte = MESSAGE[39:32];
            8'd8: message_byte = MESSAGE[31:24];
            8'd9: message_byte = MESSAGE[23:16];
            8'd10: message_byte = MESSAGE[15:8];
            8'd11: message_byte = MESSAGE[7:0];
            default: message_byte = 8'h00;
        endcase
    end

    always @* begin
        case (state)
            STATE_HELLO: current_word = HELLO;
            STATE_DATA: current_word = DATA_PREFIX | ({24'h0, seq} << 8) | {24'h0, message_byte};
            STATE_END: current_word = END_PREFIX | ({24'h0, seq} << 8);
            default: current_word = DONE_PREFIX | ({24'h0, seq} << 8);
        endcase
    end

    assign gpi = current_word;

    always @(posedge FPGA_CLK1_50) begin
        case (state)
            STATE_HELLO: begin
                if (gpo == 32'h00000000) begin
                    start_armed <= 1'b1;
                end else if (start_armed && gpo == START) begin
                    state <= STATE_DATA;
                    seq <= START_SEQUENCE;
                    byte_index <= 8'd0;
                end
            end
            STATE_DATA: begin
                if (gpo == ((current_word & 32'h00ffffff) | ACK_PREFIX)) begin
                    seq <= seq + 1'b1;
                    if (byte_index == LAST_BYTE) begin
                        state <= STATE_END;
                    end else begin
                        byte_index <= byte_index + 1'b1;
                    end
                end
            end
            STATE_END: begin
                if (gpo == ((current_word & 32'h00ffffff) | ACK_PREFIX)) begin
                    state <= STATE_DONE;
                end
            end
            default: begin
                state <= STATE_DONE;
            end
        endcase
    end

endmodule

module top (
    input wire FPGA_CLK1_50
);

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    mailbox_fsm protocol (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .gpo(hps_to_fpga),
        .gpi(fpga_to_hps)
    );

endmodule
