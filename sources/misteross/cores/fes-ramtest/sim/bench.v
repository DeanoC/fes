// SPDX-License-Identifier: GPL-2.0-or-later
// Connects the utility core to a behavioral SDRAM chip. HPS DDR uses the
// cyclonev_hps_interface_fpga2sdram stand-in compiled beside this bench;
// the test reads its violation flag, open bursts and one 64-bit lane.
// latency_check recounts every latency read from the memory handshakes and
// compares its counts with the ones the core reports.
module bench (
    input wire FPGA_CLK1_50,
    input wire [2:0] mask_fault,
    output wire [106:0] byte_status,
    output wire [11:0] byte_coverage,
    output wire [1:0] byte_high_rows,
    output wire [31:0] masked_writes, no_writes, refresh_masked_writes,

    output wire HDMI_TX_DE, HDMI_TX_VS,
    output wire [23:0] HDMI_TX_D,
    input wire [11:0] peek_slot,
    output wire [63:0] peek_data,
    output wire ddr_violation,
    output wire ddr_open,
    output wire ddr_finishing,
    output wire ddr_draining_reads,
    output wire [3:0] lat_mismatch,
    output wire [127:0] lat_checked
);
    assign byte_status = dut.byte_status;
    assign byte_coverage = {chip.banks_seen, chip.low_columns_seen, chip.rows_seen, chip.high_columns_seen};
    assign byte_high_rows = chip.high_rows_seen;
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

    // The memory clock is the reference clock in simulation.
    latency_check #(.BEATS(2)) lat0 (
        .clk(FPGA_CLK1_50), .arm(dut.lat_start[0]), .taken(dut.ddr0_read & ~dut.ddr0_wait), .beat(dut.ddr0_rvalid),
        .line_ok(dut.ddr0_burst == 8'd2 && dut.ddr0_address[0] == 1'b0),
        .sample(dut.lat_sample[0]), .first(dut.lat_first[13:0]), .last(dut.lat_last[13:0]),
        .mismatch(lat_mismatch[0]), .checked(lat_checked[31:0])
    );
    latency_check #(.BEATS(4)) lat1 (
        .clk(FPGA_CLK1_50), .arm(dut.lat_start[1]), .taken(dut.ddr1_read & ~dut.ddr1_wait), .beat(dut.ddr1_rvalid),
        .line_ok(dut.ddr1_burst == 8'd4 && dut.ddr1_address[1:0] == 2'd0),
        .sample(dut.lat_sample[1]), .first(dut.lat_first[27:14]), .last(dut.lat_last[27:14]),
        .mismatch(lat_mismatch[1]), .checked(lat_checked[63:32])
    );
    latency_check #(.BEATS(4)) lat2 (
        .clk(FPGA_CLK1_50), .arm(dut.lat_start[2]), .taken(dut.ddr2_read & ~dut.ddr2_wait), .beat(dut.ddr2_rvalid),
        .line_ok(dut.ddr2_burst == 8'd4 && dut.ddr2_address[1:0] == 2'd0),
        .sample(dut.lat_sample[2]), .first(dut.lat_first[41:28]), .last(dut.lat_last[41:28]),
        .mismatch(lat_mismatch[2]), .checked(lat_checked[95:64])
    );
    // SDRAM: the controller samples start on each edge it is high; the
    // sixteen words of a line are consecutive reads from a line boundary.
    reg [3:0] sdram_word = 4'd0;
    reg sdram_order_ok = 1'b1;
    always @(posedge FPGA_CLK1_50) begin
        if (dut.lat_start[3])
            sdram_word <= 4'd0;
        else if (dut.sdram_done && dut.sdram_test.state == 3'd7) begin
            if (dut.sdram_write || dut.sdram_addr[3:0] != sdram_word)
                sdram_order_ok <= 1'b0;
            sdram_word <= sdram_word + 4'd1;
        end
    end
    latency_check #(.BEATS(16)) lat3 (
        .clk(FPGA_CLK1_50), .arm(dut.lat_start[3]), .taken(dut.sdram_start), .beat(dut.sdram_done), .line_ok(sdram_order_ok),
        .sample(dut.lat_sample[3]), .first(dut.lat_first[55:42]), .last(dut.lat_last[55:42]),
        .mismatch(lat_mismatch[3]), .checked(lat_checked[127:96])
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
        // MiSTer addon wiring: DQM shares the two high row-address pins.
        .dqml(sdram_a[11]),
        .dqmh(sdram_a[12]),
        .dq(sdram_dq)
    );
endmodule

// Counts one latency read after arm: from the edge the first request is
// taken to the edges of its first and last data, then checks the reported
// counts on the following sample. mismatch is sticky.
module latency_check #(
    parameter integer BEATS = 2
) (
    input wire clk,
    input wire arm,
    input wire taken,
    input wire beat,
    input wire line_ok,
    input wire sample,
    input wire [13:0] first,
    input wire [13:0] last,
    output reg mismatch = 1'b0,
    output reg [31:0] checked = 32'd0
);
    reg armed = 1'b0;
    reg running = 1'b0;
    reg [13:0] count = 14'd0;
    reg [13:0] first_at = 14'd0;
    reg [13:0] last_at = 14'd0;
    reg [7:0] beats = 8'd0;
    reg complete = 1'b0;
    always @(posedge clk) begin
        if (arm) begin
            armed <= 1'b1;
            running <= 1'b0;
            complete <= 1'b0;
            beats <= 8'd0;
        end else if (armed && !running && taken) begin
            running <= 1'b1;
            count <= 14'd1;
            if (!line_ok)
                mismatch <= 1'b1;
        end else if (running) begin
            count <= count + 14'd1;
            if (beat) begin
                beats <= beats + 8'd1;
                if (beats == 8'd0)
                    first_at <= count;
                if (beats == BEATS[7:0] - 8'd1) begin
                    last_at <= count;
                    running <= 1'b0;
                    armed <= 1'b0;
                    complete <= 1'b1;
                end
            end
        end
        if (sample) begin
            if (!complete || first != first_at || last != last_at || first > last)
                mismatch <= 1'b1;
            complete <= 1'b0;
            checked <= checked + 32'd1;
        end
    end
endmodule
