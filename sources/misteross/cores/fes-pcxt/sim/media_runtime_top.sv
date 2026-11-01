// SPDX-License-Identifier: GPL-2.0-or-later
// The actual PC/XT mailbox, writer, ordered loader and SDRAM controller.
// CPU and FDC request pins let the fixture exercise contention and removal.
module media_runtime_top (
    input wire clk, cold_reset,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset, initialized, disk_loaded, media_present,
    output wire [1:0] unit_state,
    input wire memory_valid,
    output wire memory_ready, memory_done,
    input wire [19:0] address,
    output wire [7:0] memory_rdata,
    input wire media_read_valid,
    input wire [18:0] media_read_addr,
    output wire media_read_ready,
    output wire [7:0] media_read_data,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_sample
);
    wire [18:0] source_addr, disk_address;
    wire [15:0] source_data;
    wire [1:0] source_enable;
    wire [31:0] unused_unit_size;
    wire source_ready, disk_restart, disk_valid, disk_ready, disk_done, storage_valid, storage_ready, storage_done;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_computer_mailbox_pcxt gp_mailbox (
        .clk(clk), .gpo(gpo), .build_id(128'd0), .gpi(gpi), .exec_reset(exec_reset),
        .media_reset(cold_reset), .media_write_addr(source_addr), .media_write_data(source_data),
        .media_write_enable(source_enable), .media_write_ready(source_ready), .media_write_busy(1'b0),
        .unit0_state(unit_state), .unit0_size(unused_unit_size), .keyboard_rows());
    /* verilator lint_on PINCONNECTEMPTY */
    pcxt_media media (
        .clk(clk), .cold_reset(cold_reset), .unit_state(unit_state), .source_addr(source_addr),
        .source_data(source_data), .source_enable(source_enable), .source_ready(source_ready),
        .disk_restart(disk_restart), .media_present(media_present), .disk_loaded(disk_loaded),
        .disk_valid(disk_valid), .disk_address(disk_address), .disk_data(disk_data),
        .disk_ready(disk_ready), .disk_done(disk_done));
    wire [7:0] disk_data;
    pcxt_disk_loader loader (
        .clk(clk), .cold_reset(cold_reset), .restart(disk_restart),
        .load_valid(disk_valid), .load_ready(disk_ready), .load_done(disk_done),
        .load_address(disk_address), .loaded(disk_loaded),
        .storage_valid(storage_valid), .storage_ready(storage_ready), .storage_done(storage_done));
    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_memory memory (
        .clk(clk), .clk_pin(clk), .cold_reset(cold_reset), .reset(exec_reset), .initialized(initialized),
        .memory_valid(memory_valid), .memory_ready(memory_ready), .address(address), .wdata(8'd0),
        .write_cycle(1'b0), .memory_done(memory_done), .memory_rdata(memory_rdata),
        .firmware_valid(1'b0), .firmware_ready(), .firmware_address(16'd0), .firmware_data(8'd0), .firmware_done(),
        .disk_load_valid(storage_valid), .disk_load_ready(storage_ready), .disk_load_address(disk_address),
        .disk_load_data(disk_data), .disk_load_done(storage_done),
        .media_read_valid(media_read_valid), .media_read_addr(media_read_addr),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs), .sdram_nras(sdram_nras),
        .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe), .sdram_ba(sdram_ba), .sdram_a(sdram_a),
        .sdram_dqml(sdram_dqml), .sdram_dqmh(sdram_dqmh), .dq_out(dq_out), .dq_oe(dq_oe),
        .dq_rise(dq_rise), .dq_fall(dq_fall));
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
