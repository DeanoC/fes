// Port-2 stand-in for cyclonev_hps_interface_fpga2sdram.
// Command layout matches the MiSTer 64-bit f2h_sdram2 wiring.
module cyclonev_hps_interface_fpga2sdram (
    input wire [5:0] cfg_axi_mm_select,
    input wire [17:0] cfg_cport_rfifo_map,
    input wire [11:0] cfg_cport_type,
    input wire [17:0] cfg_cport_wfifo_map,
    input wire [11:0] cfg_port_width,
    input wire [15:0] cfg_rfifo_cport_map,
    input wire [15:0] cfg_wfifo_cport_map,
    input wire cmd_port_clk_0,
    input wire cmd_port_clk_1,
    input wire cmd_port_clk_2,
    input wire cmd_port_clk_3,
    input wire cmd_port_clk_4,
    input wire cmd_port_clk_5,
    input wire cmd_valid_0,
    input wire cmd_valid_1,
    input wire cmd_valid_2,
    input wire cmd_valid_3,
    input wire cmd_valid_4,
    input wire cmd_valid_5,
    input wire [59:0] cmd_data_2,
    output wire cmd_ready_2,
    input wire wr_clk_0,
    input wire wr_clk_1,
    input wire wr_clk_2,
    input wire wr_clk_3,
    input wire wr_valid_0,
    input wire wr_valid_1,
    input wire wr_valid_2,
    input wire wr_valid_3,
    input wire [89:0] wr_data_3,
    input wire rd_clk_0,
    input wire rd_clk_1,
    input wire rd_clk_2,
    input wire rd_clk_3,
    input wire rd_ready_0,
    input wire rd_ready_1,
    input wire rd_ready_2,
    input wire rd_ready_3,
    output reg [79:0] rd_data_3,
    output reg rd_valid_3,
    input wire wrack_ready_0,
    input wire wrack_ready_1,
    input wire wrack_ready_2,
    input wire wrack_ready_3,
    input wire wrack_ready_4,
    input wire wrack_ready_5
);
    reg [15:0] mem [0:65535];
    reg busy = 1'b0;
    integer index;
    initial begin
        rd_data_3 = 80'd0;
        rd_valid_3 = 1'b0;
        for (index = 0; index < 65536; index = index + 1)
            mem[index] = 16'h0000;
    end

    assign cmd_ready_2 = !busy;

    wire [15:0] word = cmd_data_2[17:2];
    wire do_write = cmd_data_2[1];
    wire do_read = cmd_data_2[0];

    always @(posedge cmd_port_clk_2) begin
        rd_valid_3 <= 1'b0;
        busy <= 1'b0;
        if (cmd_valid_2 && !busy && do_write && wr_data_3[80])
            mem[word] <= wr_data_3[15:0];
        if (cmd_valid_2 && !busy && do_read) begin
            rd_data_3 <= {64'd0, mem[word]};
            rd_valid_3 <= 1'b1;
            busy <= 1'b1;
        end
    end

    // The remaining ports are the MiSTer tie-offs. They are inputs to the
    // atom, so the model accepts them without changing the memory.
    /* verilator lint_off UNUSED */
    wire unused_tie = |{cfg_axi_mm_select, cfg_cport_rfifo_map, cfg_cport_type,
                        cfg_cport_wfifo_map, cfg_port_width, cfg_rfifo_cport_map,
                        cfg_wfifo_cport_map, cmd_port_clk_0, cmd_port_clk_1,
                        cmd_port_clk_3, cmd_port_clk_4, cmd_port_clk_5,
                        cmd_valid_0, cmd_valid_1, cmd_valid_3, cmd_valid_4,
                        cmd_valid_5, wr_clk_0, wr_clk_1, wr_clk_2, wr_clk_3,
                        wr_valid_0, wr_valid_1, wr_valid_2, wr_valid_3,
                        rd_clk_0, rd_clk_1, rd_clk_2, rd_clk_3,
                        rd_ready_0, rd_ready_1, rd_ready_2, rd_ready_3,
                        wrack_ready_0, wrack_ready_1, wrack_ready_2,
                        wrack_ready_3, wrack_ready_4, wrack_ready_5,
                        cmd_port_clk_2};
    /* verilator lint_on UNUSED */
endmodule
