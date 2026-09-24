// SPDX-License-Identifier: GPL-2.0-or-later
// Cycle protocol matches the closed 910/911 probes. This core does not add a memory opcode.
// One 64-bit bridge beat. The halfword is the low 16 data bits.
// The address fills the same field the 911 probe used. Bits above the
// original 16 were zeros there; a wider scan uses them.
module hps_ddr_port (
    input wire clk,
    input wire start,
    input wire write,
    input wire [31:0] addr,
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
            cmd_data <= {18'd0, 8'd1, addr, write, ~write};
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
