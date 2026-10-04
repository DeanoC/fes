// SPDX-License-Identifier: GPL-2.0-or-later
// Connects the utility core to a behavioral SDRAM chip. HPS DDR uses the
// cyclonev_hps_interface_fpga2sdram stand-in compiled beside this bench;
// the test reads its violation flag, open bursts and one 64-bit lane.
module bench (
    input wire FPGA_CLK1_50,
    input wire [2:0] mask_fault,
    output wire [106:0] byte_status,
    output wire [11:0] byte_coverage,
    output wire [31:0] masked_writes, no_writes, refresh_masked_writes,

    output wire HDMI_TX_DE, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    input wire [11:0] peek_slot,
    output wire [63:0] peek_data,
    output wire ddr_violation,
    output wire ddr_open,
    output wire ddr_finishing,
    output wire ddr_draining_reads
);
    assign byte_status = dut.byte_status;
    assign byte_coverage = {chip.banks_seen, chip.low_columns_seen, chip.rows_seen, chip.high_columns_seen};
    assign masked_writes = chip.masked_writes;
    assign no_writes = chip.no_writes;
    assign refresh_masked_writes = chip.refresh_masked_writes;
    // A guard finishing a write burst, or hiding reads issued before a hold.
    assign ddr_finishing = dut.hps_ddr.port0.finishing | dut.hps_ddr.port1.finishing |
        dut.hps_ddr.port2.finishing;
    assign ddr_draining_reads =
        (dut.hps_ddr.port0.draining && dut.hps_ddr.port0.reads_owed != 12'd0) ||
        (dut.hps_ddr.port1.draining && dut.hps_ddr.port1.reads_owed != 12'd0) ||
        (dut.hps_ddr.port2.draining && dut.hps_ddr.port2.reads_owed != 12'd0);
    assign peek_data = dut.hps_ddr.f2sdram.mem[peek_slot];
    assign ddr_violation = dut.hps_ddr.f2sdram.violation;
    assign ddr_open = dut.hps_ddr.f2sdram.owed[0] != 8'd0 ||
        dut.hps_ddr.f2sdram.owed[1] != 8'd0 || dut.hps_ddr.f2sdram.owed[2] != 8'd0;
    wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe;
    wire sdram_dqml, sdram_dqmh;
    wire [1:0] sdram_ba;
    wire [12:0] sdram_a;
    wire [15:0] sdram_dq;
    wire hdmi_clk, hdmi_hs;
    wire hdmi_scl, hdmi_sda;

    top dut (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .HDMI_TX_CLK(hdmi_clk),
        .HDMI_TX_DE(HDMI_TX_DE),
        .HDMI_TX_D(HDMI_TX_D),
        .HDMI_TX_HS(hdmi_hs),
        .HDMI_TX_VS(HDMI_TX_VS),
        .HDMI_I2C_SCL(hdmi_scl),
        .HDMI_I2C_SDA(hdmi_sda),
        .SDRAM_CLK(sdram_clk),
        .SDRAM_CKE(sdram_cke),
        .SDRAM_nCS(sdram_ncs),
        .SDRAM_nRAS(sdram_nras),
        .SDRAM_nCAS(sdram_ncas),
        .SDRAM_nWE(sdram_nwe),
        .SDRAM_DQML(sdram_dqml),
        .SDRAM_DQMH(sdram_dqmh),
        .SDRAM_BA(sdram_ba),
        .SDRAM_A(sdram_a),
        .SDRAM_DQ(sdram_dq)
    );

    sdram_model chip (
        .mask_fault(mask_fault),
        .clk(sdram_clk),
        .cke(sdram_cke),
        .ncs(sdram_ncs),
        .nras(sdram_nras),
        .ncas(sdram_ncas),
        .nwe(sdram_nwe),
        .ba(sdram_ba),
        .a(sdram_a),
        .dqml(sdram_dqml),
        .dqmh(sdram_dqmh),
        .dq(sdram_dq)
    );
endmodule
