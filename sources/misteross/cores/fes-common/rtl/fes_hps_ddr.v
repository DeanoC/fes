// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"

// fes.memory.hps-ddr 1.0: the FPGA-to-HPS SDRAM ports in the layout U-Boot
// latches from the boot splash. Port 0 is 128-bit, ports 1 and 2 are 64-bit.
// Each is Avalon-MM with word addresses (byte address / 16 on port 0, / 8 on
// ports 1 and 2) and bursts of 1 to 128 beats. Stay inside the window
// FES_APPLICATION_HPS_DDR_WINDOW_BASE/BYTES: the ports reach all of DDR.
//
// hold is the core's execution reset. It is synchronized to each port clock
// and returned as pN_reset; reset that port's master with it. pN_drained
// proves that every guard-owned command, write beat and read response has
// completed, including responses hidden during hold. A master must quiesce
// before using this proof for reprogramming; hold alone is insufficient.
// The guards finish or drain a transfer the master had started, so the controller never
// sees a burst stop midway. Tie an unused port's read and write low.
// The wiring follows Quartus 17.0 f2sdram::add_port (ip/altera/hps/util/
// procedures.tcl). Its Avalon ports leave wr_valid undriven; here it follows
// the write strobe, and every unused input is tied off explicitly.
// Compile-time specialization for masters that tie the corresponding inputs
// low. Defaults retain the full three-port read/write interface and boot layout.
// A disabled operation must not be submitted by its master.
module fes_hps_ddr #(
    parameter [0:0] P0_WRITE_ENABLE = 1'b1,
    parameter [0:0] P1_ENABLE = 1'b1,
    parameter [0:0] P2_ENABLE = 1'b1
) (
    input  wire         hold,

    input  wire         p0_clk,
    output wire         p0_reset,
    output wire         p0_drained,
    input  wire [27:0]  p0_address,
    input  wire [7:0]   p0_burstcount,
    output wire         p0_waitrequest,
    output wire [127:0] p0_readdata,
    output wire         p0_readdatavalid,
    input  wire         p0_read,
    input  wire [127:0] p0_writedata,
    input  wire [15:0]  p0_byteenable,
    input  wire         p0_write,

    input  wire         p1_clk,
    output wire         p1_reset,
    output wire         p1_drained,
    input  wire [28:0]  p1_address,
    input  wire [7:0]   p1_burstcount,
    output wire         p1_waitrequest,
    output wire [63:0]  p1_readdata,
    output wire         p1_readdatavalid,
    input  wire         p1_read,
    input  wire [63:0]  p1_writedata,
    input  wire [7:0]   p1_byteenable,
    input  wire         p1_write,

    input  wire         p2_clk,
    output wire         p2_reset,
    output wire         p2_drained,
    input  wire [28:0]  p2_address,
    input  wire [7:0]   p2_burstcount,
    output wire         p2_waitrequest,
    output wire [63:0]  p2_readdata,
    output wire         p2_readdatavalid,
    input  wire         p2_read,
    input  wire [63:0]  p2_writedata,
    input  wire [7:0]   p2_byteenable,
    input  wire         p2_write
);
    localparam [31:0] PORT_WIDTH = `FES_APPLICATION_HPS_DDR_CFG_PORT_WIDTH;
    localparam [31:0] CPORT_TYPE = `FES_APPLICATION_HPS_DDR_CFG_CPORT_TYPE;
    localparam [31:0] CPORT_WFIFO_MAP = `FES_APPLICATION_HPS_DDR_CFG_CPORT_WFIFO_MAP;
    localparam [31:0] CPORT_RFIFO_MAP = `FES_APPLICATION_HPS_DDR_CFG_CPORT_RFIFO_MAP;
    localparam [31:0] WFIFO_CPORT_MAP = `FES_APPLICATION_HPS_DDR_CFG_WFIFO_CPORT_MAP;
    localparam [31:0] RFIFO_CPORT_MAP = `FES_APPLICATION_HPS_DDR_CFG_RFIFO_CPORT_MAP;
    localparam [31:0] AXI_MM_SELECT = `FES_APPLICATION_HPS_DDR_CFG_AXI_MM_SELECT;

    (* async_reg = "true" *) reg [1:0] p0_hold_sync = 2'b11;
    (* async_reg = "true" *) reg [1:0] p1_hold_sync = 2'b11;
    (* async_reg = "true" *) reg [1:0] p2_hold_sync = 2'b11;
    always @(posedge p0_clk) p0_hold_sync <= {p0_hold_sync[0], hold};
    always @(posedge p1_clk) p1_hold_sync <= {p1_hold_sync[0], hold};
    always @(posedge p2_clk) p2_hold_sync <= {p2_hold_sync[0], hold};
    assign p0_reset = p0_hold_sync[1];
    assign p1_reset = p1_hold_sync[1];
    assign p2_reset = p2_hold_sync[1];

    wire [27:0]  m0_address;
    wire [7:0]   m0_burstcount;
    wire         m0_read, m0_write;
    wire [127:0] m0_writedata;
    wire [15:0]  m0_byteenable;
    wire [28:0]  m1_address, m2_address;
    wire [7:0]   m1_burstcount, m2_burstcount;
    wire         m1_read, m1_write, m2_read, m2_write;
    wire [63:0]  m1_writedata, m2_writedata;
    wire [7:0]   m1_byteenable, m2_byteenable;
    wire         cmd_ready_0, cmd_ready_1, cmd_ready_2;
    wire [79:0]  rd_data_0, rd_data_1, rd_data_2, rd_data_3;
    wire         rd_valid_1, rd_valid_2, rd_valid_3;

    fes_hps_ddr_guard #(.DATA_W(128), .ADDR_W(28)) port0 (
        .clk(p0_clk), .hold(p0_reset), .drained(p0_drained),
        .address(p0_address), .burstcount(p0_burstcount),
        .waitrequest(p0_waitrequest), .readdata(p0_readdata),
        .readdatavalid(p0_readdatavalid), .read(p0_read),
        .writedata(p0_writedata), .byteenable(p0_byteenable), .write(p0_write),
        .m_address(m0_address), .m_burstcount(m0_burstcount),
        .m_waitrequest(~cmd_ready_0),
        .m_readdata({rd_data_1[63:0], rd_data_0[63:0]}),
        .m_readdatavalid(rd_valid_1), .m_read(m0_read),
        .m_writedata(m0_writedata), .m_byteenable(m0_byteenable), .m_write(m0_write)
    );
    fes_hps_ddr_guard #(.DATA_W(64), .ADDR_W(29)) port1 (
        .clk(p1_clk), .hold(p1_reset), .drained(p1_drained),
        .address(p1_address), .burstcount(p1_burstcount),
        .waitrequest(p1_waitrequest), .readdata(p1_readdata),
        .readdatavalid(p1_readdatavalid), .read(p1_read),
        .writedata(p1_writedata), .byteenable(p1_byteenable), .write(p1_write),
        .m_address(m1_address), .m_burstcount(m1_burstcount),
        .m_waitrequest(~cmd_ready_1), .m_readdata(rd_data_2[63:0]),
        .m_readdatavalid(rd_valid_2), .m_read(m1_read),
        .m_writedata(m1_writedata), .m_byteenable(m1_byteenable), .m_write(m1_write)
    );
    fes_hps_ddr_guard #(.DATA_W(64), .ADDR_W(29)) port2 (
        .clk(p2_clk), .hold(p2_reset), .drained(p2_drained),
        .address(p2_address), .burstcount(p2_burstcount),
        .waitrequest(p2_waitrequest), .readdata(p2_readdata),
        .readdatavalid(p2_readdatavalid), .read(p2_read),
        .writedata(p2_writedata), .byteenable(p2_byteenable), .write(p2_write),
        .m_address(m2_address), .m_burstcount(m2_burstcount),
        .m_waitrequest(~cmd_ready_2), .m_readdata(rd_data_3[63:0]),
        .m_readdatavalid(rd_valid_3), .m_read(m2_read),
        .m_writedata(m2_writedata), .m_byteenable(m2_byteenable), .m_write(m2_write)
    );

    // Keep unused hard-block operations physically tied low even when generic
    // guard state registers cannot be reduced to constants by synthesis.
    wire command_write0 = P0_WRITE_ENABLE && m0_write;
    wire command_read1 = P1_ENABLE && m1_read;
    wire command_write1 = P1_ENABLE && m1_write;
    wire command_read2 = P2_ENABLE && m2_read;
    wire command_write2 = P2_ENABLE && m2_write;

    cyclonev_hps_interface_fpga2sdram f2sdram (
        .cfg_axi_mm_select(AXI_MM_SELECT[5:0]),
        .cfg_cport_rfifo_map(CPORT_RFIFO_MAP[17:0]),
        .cfg_cport_type(CPORT_TYPE[11:0]),
        .cfg_cport_wfifo_map(CPORT_WFIFO_MAP[17:0]),
        .cfg_port_width(PORT_WIDTH[11:0]),
        .cfg_rfifo_cport_map(RFIFO_CPORT_MAP[15:0]),
        .cfg_wfifo_cport_map(WFIFO_CPORT_MAP[15:0]),
        .cmd_port_clk_0(p0_clk),
        .cmd_port_clk_1(p1_clk),
        .cmd_port_clk_2(p2_clk),
        .cmd_port_clk_3(1'b0),
        .cmd_port_clk_4(1'b0),
        .cmd_port_clk_5(1'b0),
        .cmd_valid_0(m0_read | command_write0),
        .cmd_valid_1(command_read1 | command_write1),
        .cmd_valid_2(command_read2 | command_write2),
        .cmd_valid_3(1'b0),
        .cmd_valid_4(1'b0),
        .cmd_valid_5(1'b0),
        .cmd_data_0({18'd0, m0_burstcount, 4'd0, m0_address, command_write0, m0_read}),
        .cmd_data_1({18'd0, m1_burstcount, 3'd0, m1_address, command_write1, command_read1}),
        .cmd_data_2({18'd0, m2_burstcount, 3'd0, m2_address, command_write2, command_read2}),
        .cmd_data_3(60'd0),
        .cmd_data_4(60'd0),
        .cmd_data_5(60'd0),
        .cmd_ready_0(cmd_ready_0),
        .cmd_ready_1(cmd_ready_1),
        .cmd_ready_2(cmd_ready_2),
        .cmd_ready_3(),
        .cmd_ready_4(),
        .cmd_ready_5(),
        .wr_clk_0(p0_clk),
        .wr_clk_1(p0_clk),
        .wr_clk_2(p1_clk),
        .wr_clk_3(p2_clk),
        .wr_valid_0(command_write0),
        .wr_valid_1(command_write0),
        .wr_valid_2(command_write1),
        .wr_valid_3(command_write2),
        .wr_data_0({2'b00, m0_byteenable[7:0], 16'd0, m0_writedata[63:0]}),
        .wr_data_1({2'b00, m0_byteenable[15:8], 16'd0, m0_writedata[127:64]}),
        .wr_data_2({2'b00, m1_byteenable, 16'd0, m1_writedata}),
        .wr_data_3({2'b00, m2_byteenable, 16'd0, m2_writedata}),
        .rd_clk_0(p0_clk),
        .rd_clk_1(p0_clk),
        .rd_clk_2(p1_clk),
        .rd_clk_3(p2_clk),
        .rd_ready_0(1'b1),
        .rd_ready_1(1'b1),
        .rd_ready_2(1'b1),
        .rd_ready_3(1'b1),
        .rd_data_0(rd_data_0),
        .rd_data_1(rd_data_1),
        .rd_data_2(rd_data_2),
        .rd_data_3(rd_data_3),
        .rd_valid_0(),
        .rd_valid_1(rd_valid_1),
        .rd_valid_2(rd_valid_2),
        .rd_valid_3(rd_valid_3),
        .wrack_ready_0(1'b1),
        .wrack_ready_1(1'b1),
        .wrack_ready_2(1'b1),
        .wrack_ready_3(1'b1),
        .wrack_ready_4(1'b1),
        .wrack_ready_5(1'b1),
        .wrack_data_0(), .wrack_data_1(), .wrack_data_2(),
        .wrack_data_3(), .wrack_data_4(), .wrack_data_5(),
        .wrack_valid_0(), .wrack_valid_1(), .wrack_valid_2(),
        .wrack_valid_3(), .wrack_valid_4(), .wrack_valid_5(),
        .wr_ready_0(), .wr_ready_1(), .wr_ready_2(), .wr_ready_3()
    );
endmodule
