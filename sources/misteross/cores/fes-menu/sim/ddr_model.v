// SPDX-License-Identifier: GPL-2.0-or-later
// Behavioral cyclonev_hps_interface_fpga2sdram for the fes.memory.hps-ddr
// layout: port 0 is 128-bit on command 0 and data 0/1, ports 1 and 2 are
// 64-bit on commands 1/2 and data 2/3, all Avalon-MM as Quartus wires them.
// Commands are refused at random and while 14 reads are pending, as the
// controller does, and read data returns late with gaps.
// The model flags a command outside the FPGA window or the simulated spans,
// a malformed command field, and a read or a new burst while a write burst
// is still owed beats. The bench clocks every port from one clock.
/* verilator lint_off UNUSEDSIGNAL */
/* verilator lint_off MULTIDRIVEN */
module cyclonev_hps_interface_fpga2sdram (
    input wire [5:0] cfg_axi_mm_select,
    input wire [17:0] cfg_cport_rfifo_map,
    input wire [11:0] cfg_cport_type,
    input wire [17:0] cfg_cport_wfifo_map,
    input wire [11:0] cfg_port_width,
    input wire [15:0] cfg_rfifo_cport_map,
    input wire [15:0] cfg_wfifo_cport_map,
    input wire cmd_port_clk_0, input wire cmd_port_clk_1, input wire cmd_port_clk_2,
    input wire cmd_port_clk_3, input wire cmd_port_clk_4, input wire cmd_port_clk_5,
    input wire cmd_valid_0, input wire cmd_valid_1, input wire cmd_valid_2,
    input wire cmd_valid_3, input wire cmd_valid_4, input wire cmd_valid_5,
    input wire [59:0] cmd_data_0, input wire [59:0] cmd_data_1, input wire [59:0] cmd_data_2,
    input wire [59:0] cmd_data_3, input wire [59:0] cmd_data_4, input wire [59:0] cmd_data_5,
    output wire cmd_ready_0, output wire cmd_ready_1, output wire cmd_ready_2,
    output wire cmd_ready_3, output wire cmd_ready_4, output wire cmd_ready_5,
    input wire wrack_ready_0, input wire wrack_ready_1, input wire wrack_ready_2,
    input wire wrack_ready_3, input wire wrack_ready_4, input wire wrack_ready_5,
    output wire [9:0] wrack_data_0, output wire [9:0] wrack_data_1, output wire [9:0] wrack_data_2,
    output wire [9:0] wrack_data_3, output wire [9:0] wrack_data_4, output wire [9:0] wrack_data_5,
    output wire wrack_valid_0, output wire wrack_valid_1, output wire wrack_valid_2,
    output wire wrack_valid_3, output wire wrack_valid_4, output wire wrack_valid_5,
    input wire wr_clk_0, input wire wr_clk_1, input wire wr_clk_2, input wire wr_clk_3,
    input wire wr_valid_0, input wire wr_valid_1, input wire wr_valid_2, input wire wr_valid_3,
    input wire [89:0] wr_data_0, input wire [89:0] wr_data_1,
    input wire [89:0] wr_data_2, input wire [89:0] wr_data_3,
    output wire wr_ready_0, output wire wr_ready_1, output wire wr_ready_2, output wire wr_ready_3,
    input wire rd_clk_0, input wire rd_clk_1, input wire rd_clk_2, input wire rd_clk_3,
    input wire rd_ready_0, input wire rd_ready_1, input wire rd_ready_2, input wire rd_ready_3,
    output reg [79:0] rd_data_0, output reg [79:0] rd_data_1,
    output reg [79:0] rd_data_2, output reg [79:0] rd_data_3,
    output reg rd_valid_0, output reg rd_valid_1, output reg rd_valid_2, output reg rd_valid_3
);

    reg block_commands /* verilator public_flat_rw */ = 1'b0;
    reg pause_responses /* verilator public_flat_rw */ = 1'b0;
    reg violation /* verilator public_flat_rd */ = 1'b0;
    reg [31:0] accepted /* verilator public_flat_rd */ = 32'd0;
    reg [7:0] remaining /* verilator public_flat_rd */ = 8'd0;
    reg [31:0] next_byte = 32'd0;
    assign cmd_ready_0 = !block_commands && remaining == 8'd0;
    assign {cmd_ready_1,cmd_ready_2,cmd_ready_3,cmd_ready_4,cmd_ready_5} = 5'd0;
    assign {wr_ready_0,wr_ready_1,wr_ready_2,wr_ready_3} = 4'd0;
    assign {wrack_valid_0,wrack_valid_1,wrack_valid_2,wrack_valid_3,wrack_valid_4,wrack_valid_5} = 6'd0;
    assign {wrack_data_0,wrack_data_1,wrack_data_2,wrack_data_3,wrack_data_4,wrack_data_5} = 60'd0;
    initial begin
        rd_valid_0=0;rd_valid_1=0;rd_valid_2=0;rd_valid_3=0;
        rd_data_0=0;rd_data_1=0;rd_data_2=0;rd_data_3=0;
    end
    function [31:0] pixel;
        input [31:0] a;
        reg [31:0] index;
        begin
            index=(a-32'h30000000-(a[22] ? 32'h00400000 : 32'd0))>>2;
            pixel={8'd0,index[7:0],index[15:8],a[22] ? 8'h77 : 8'h22};
        end
    endfunction
    always @(posedge cmd_port_clk_0) begin
        rd_valid_0 <= 0;rd_valid_1 <= 0;
        if ({cmd_valid_5,cmd_valid_4,cmd_valid_3,cmd_valid_2,cmd_valid_1} != 5'd0 ||
            {wr_valid_3,wr_valid_2,wr_valid_1,wr_valid_0} != 4'd0)
            violation <= 1;
        if (cmd_valid_0 && cmd_ready_0) begin
            if (cmd_data_0[1:0] != 2'b01 || cmd_data_0[59:42] != 18'd0 ||
                cmd_data_0[33:30] != 4'd0 || cmd_data_0[41:34] == 8'd0 ||
                cmd_data_0[41:34] > 8'd128 ||
                {cmd_data_0[29:2],4'd0} < 32'h30000000 ||
                {cmd_data_0[29:2],4'd0} + {20'd0,cmd_data_0[41:34],4'd0} > 32'h30784000 ||
                ({cmd_data_0[29:2],4'd0} >= 32'h30384000 && {cmd_data_0[29:2],4'd0} < 32'h30400000))
                violation <= 1;
            remaining <= cmd_data_0[41:34];
            next_byte <= {cmd_data_0[29:2],4'd0};
            accepted <= accepted + 32'd1;
        end
        if (remaining != 8'd0 && !pause_responses) begin
            rd_data_0 <= {16'd0,pixel(next_byte+32'd4),pixel(next_byte)};
            rd_data_1 <= {16'd0,pixel(next_byte+32'd12),pixel(next_byte+32'd8)};
            rd_valid_0 <= 1;rd_valid_1 <= 1;
            next_byte <= next_byte + 32'd16;
            remaining <= remaining - 8'd1;
        end
    end
endmodule
