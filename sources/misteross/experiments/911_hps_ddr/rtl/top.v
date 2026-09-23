// HPS DDR probe through the FPGA-to-HPS bridge, port 2.
// This is on-SoC DDR, separate from the GPIO memory addon.
// Host traffic uses fes.application 1.0 framing. Opcode 18 is experiment-local.
module top (
    input wire FPGA_CLK1_50
);
    localparam [127:0] BUILD_ID = 128'h4631_3139_5f48_5053_4444_5200_0000_0000;

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire mem_start;
    wire mem_write;
    wire [15:0] mem_addr;
    wire [15:0] mem_wdata;
    wire mem_done;
    wire [15:0] mem_rdata;

    fes_mem_window #(.BUILD_ID(BUILD_ID)) window (
        .clk(FPGA_CLK1_50),
        .gpo(gp_out),
        .gpi(gp_in),
        .mem_start(mem_start),
        .mem_write(mem_write),
        .mem_addr(mem_addr),
        .mem_wdata(mem_wdata),
        .mem_done(mem_done),
        .mem_rdata(mem_rdata)
    );

    hps_ddr_port ddr (
        .clk(FPGA_CLK1_50),
        .start(mem_start),
        .write(mem_write),
        .addr(mem_addr),
        .wdata(mem_wdata),
        .done(mem_done),
        .rdata(mem_rdata)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );
endmodule

// One 64-bit bridge beat. The halfword is the low 16 data bits.
module hps_ddr_port (
    input wire clk,
    input wire start,
    input wire write,
    input wire [15:0] addr,
    input wire [15:0] wdata,
    output reg done,
    output reg [15:0] rdata
);
    reg cmd_valid = 1'b0;
    reg [59:0] cmd_data = 60'd0;
    reg [89:0] wr_data = 90'd0;
    reg seen = 1'b0;
    reg writing = 1'b0;
    reg waiting_read = 1'b0;
    wire cmd_ready;
    wire [79:0] rd_data;
    wire rd_valid;

    cyclonev_hps_interface_fpga2sdram f2sdram (
        .cfg_axi_mm_select(6'b000000),
        .cfg_cport_rfifo_map(18'b000000000011010000),
        .cfg_cport_type(12'b000000111111),
        .cfg_cport_wfifo_map(18'b000000000011010000),
        .cfg_port_width(12'b000000010110),
        .cfg_rfifo_cport_map(16'b0010000100000000),
        .cfg_wfifo_cport_map(16'b0010000100000000),
        .cmd_port_clk_0(1'b0),
        .cmd_port_clk_1(1'b0),
        .cmd_port_clk_2(clk),
        .cmd_port_clk_3(1'b0),
        .cmd_port_clk_4(1'b0),
        .cmd_port_clk_5(1'b0),
        .cmd_valid_0(1'b0),
        .cmd_valid_1(1'b0),
        .cmd_valid_2(cmd_valid),
        .cmd_valid_3(1'b0),
        .cmd_valid_4(1'b0),
        .cmd_valid_5(1'b0),
        .cmd_data_2(cmd_data),
        .cmd_ready_2(cmd_ready),
        .wr_clk_0(1'b0),
        .wr_clk_1(1'b0),
        .wr_clk_2(1'b0),
        .wr_clk_3(clk),
        .wr_valid_0(1'b0),
        .wr_valid_1(1'b0),
        .wr_valid_2(1'b0),
        .wr_valid_3(cmd_valid & writing),
        .wr_data_3(wr_data),
        .rd_clk_0(1'b0),
        .rd_clk_1(1'b0),
        .rd_clk_2(1'b0),
        .rd_clk_3(clk),
        .rd_ready_0(1'b1),
        .rd_ready_1(1'b1),
        .rd_ready_2(1'b1),
        .rd_ready_3(1'b1),
        .rd_data_3(rd_data),
        .rd_valid_3(rd_valid),
        .wrack_ready_0(1'b1),
        .wrack_ready_1(1'b1),
        .wrack_ready_2(1'b1),
        .wrack_ready_3(1'b1),
        .wrack_ready_4(1'b1),
        .wrack_ready_5(1'b1)
    );

    // Avalon hold: keep the command asserted until cmd_ready is high in
    // that same cycle. f2h_sdram2 returns its 64-bit data on read port 3.
    always @(posedge clk) begin
        done <= 1'b0;
        if (!start) begin
            seen <= 1'b0;
            cmd_valid <= 1'b0;
        end else if (!seen && !waiting_read && !cmd_valid) begin
            cmd_valid <= 1'b1;
            writing <= write;
            cmd_data <= {18'd0, 8'd1, 3'd0, 13'd0, addr, write, ~write};
            wr_data <= {2'b00, 8'h03, 16'd0, 48'd0, wdata};
        end else if (cmd_valid && cmd_ready) begin
            cmd_valid <= 1'b0;
            seen <= 1'b1;
            if (writing)
                done <= 1'b1;
            else
                waiting_read <= 1'b1;
        end
        if (waiting_read && rd_valid) begin
            rdata <= rd_data[15:0];
            done <= 1'b1;
            waiting_read <= 1'b0;
        end
    end
endmodule

module fes_mem_window #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire mem_start,
    output wire mem_write,
    output wire [15:0] mem_addr,
    output wire [15:0] mem_wdata,
    input wire mem_done,
    input wire [15:0] mem_rdata
);
    localparam [31:0] SIGNATURE = 32'hf5000000;
    localparam [31:0] REQUEST_MASK = 32'h80000000;
    localparam [31:0] OPCODE_MASK = 32'h7f000000;
    localparam [31:0] INDEX_MASK = 32'h00ff0000;
    localparam [31:0] ARGUMENT_MASK = 32'h0000ffff;
    localparam [31:0] ACK_MASK = 32'h00800000;
    localparam [31:0] ERROR_MASK = 32'h00400000;
    localparam [15:0] MAGIC0 = 16'h4546;
    localparam [15:0] MAGIC1 = 16'h3153;
    localparam [6:0] OPCODE_IDENTITY = 7'd1;
    localparam [6:0] OPCODE_MEM = 7'd18;
    localparam [15:0] ERR_OPCODE = 16'd1;
    localparam [15:0] ERR_INDEX = 16'd2;
    localparam [15:0] ERR_ARGUMENT = 16'd3;

    reg request_meta = 1'b0;
    reg request_sync = 1'b0;
    reg acknowledged_toggle = 1'b0;
    reg response_error = 1'b0;
    reg [15:0] response_data = 16'h0000;
    reg waiting = 1'b0;
    reg write_op = 1'b0;
    reg [15:0] address = 16'h0000;
    reg [15:0] write_data = 16'h0000;

    wire request_toggle = (gpo & REQUEST_MASK) != 32'h00000000;
    wire [31:0] opcode_field = gpo & OPCODE_MASK;
    wire [31:0] index_field = gpo & INDEX_MASK;
    wire [31:0] argument_field = gpo & ARGUMENT_MASK;
    wire [6:0] opcode = opcode_field[30:24];
    wire [7:0] index = index_field[23:16];
    wire [15:0] argument = argument_field[15:0];

    assign gpi = SIGNATURE |
                 (acknowledged_toggle ? ACK_MASK : 32'h00000000) |
                 (response_error ? ERROR_MASK : 32'h00000000) |
                 {16'h0000, response_data};
    assign mem_start = waiting;
    assign mem_write = write_op;
    assign mem_addr = address;
    assign mem_wdata = write_data;

    function automatic [15:0] identity_word;
        input [7:0] word_index;
        begin
            case (word_index)
                8'd0: identity_word = MAGIC0;
                8'd1: identity_word = MAGIC1;
                8'd2: identity_word = 16'd1;
                8'd3: identity_word = 16'd0;
                8'd4: identity_word = 16'd3;
                8'd5: identity_word = 16'd1;
                8'd6: identity_word = 16'd0;
                8'd7: identity_word = 16'd0;
                8'd8: identity_word = {BUILD_ID[119:112], BUILD_ID[127:120]};
                8'd9: identity_word = {BUILD_ID[103:96], BUILD_ID[111:104]};
                8'd10: identity_word = {BUILD_ID[87:80], BUILD_ID[95:88]};
                8'd11: identity_word = {BUILD_ID[71:64], BUILD_ID[79:72]};
                8'd12: identity_word = {BUILD_ID[55:48], BUILD_ID[63:56]};
                8'd13: identity_word = {BUILD_ID[39:32], BUILD_ID[47:40]};
                8'd14: identity_word = {BUILD_ID[23:16], BUILD_ID[31:24]};
                8'd15: identity_word = {BUILD_ID[7:0], BUILD_ID[15:8]};
                default: identity_word = 16'h0000;
            endcase
        end
    endfunction

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;
        if (!waiting && (request_sync != acknowledged_toggle)) begin
            response_error <= 1'b0;
            response_data <= 16'h0000;
            if (opcode == OPCODE_IDENTITY) begin
                acknowledged_toggle <= request_sync;
                if (index > 8'd15) begin
                    response_error <= 1'b1;
                    response_data <= ERR_INDEX;
                end else if (argument != 16'h0000) begin
                    response_error <= 1'b1;
                    response_data <= ERR_ARGUMENT;
                end else begin
                    response_data <= identity_word(index);
                end
            end else if (opcode == OPCODE_MEM && index == 8'd0) begin
                acknowledged_toggle <= request_sync;
                address <= argument;
            end else if (opcode == OPCODE_MEM && index == 8'd1) begin
                write_data <= argument;
                write_op <= 1'b1;
                waiting <= 1'b1;
            end else if (opcode == OPCODE_MEM && index == 8'd2 && argument == 16'h0000) begin
                write_op <= 1'b0;
                waiting <= 1'b1;
            end else if (opcode == OPCODE_MEM && index == 8'd2) begin
                acknowledged_toggle <= request_sync;
                response_error <= 1'b1;
                response_data <= ERR_ARGUMENT;
            end else if (opcode == OPCODE_MEM) begin
                acknowledged_toggle <= request_sync;
                response_error <= 1'b1;
                response_data <= ERR_INDEX;
            end else begin
                acknowledged_toggle <= request_sync;
                response_error <= 1'b1;
                response_data <= ERR_OPCODE;
            end
        end else if (waiting && mem_done) begin
            waiting <= 1'b0;
            acknowledged_toggle <= request_sync;
            response_error <= 1'b0;
            response_data <= write_op ? 16'h0000 : mem_rdata;
        end
    end

endmodule
