// SPDX-License-Identifier: GPL-2.0-or-later
// Simulation only: standard core/control/display with the existing PR shell and
// actual RAM personas. Selection and memory primitives are behavioral. The
// reset observation below is not a production Qsys/PR boundary implementation.
`include "zx81_bus_pack.vh"
module fes_de25_standard_expansion_harness (
    input wire clk_sys, pixel_clk, reset_n, persona_b,
    input wire [4:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output wire [31:0] readdata,
    output wire readdatavalid, writeresponsevalid, waitrequest,
    output wire [1:0] response,
    input wire [5:0] pr_address,
    input wire pr_read, pr_write,
    input wire [31:0] pr_writedata,
    output wire [31:0] pr_readdata,
    output wire pr_readdatavalid, pr_writeresponsevalid, pr_waitrequest,
    output wire [1:0] pr_response,
    input wire [15:0] peek_address,
    output wire [7:0] peek_data,
    output wire frozen, running, machine_held, exec_reset, unexpected_dma,
    output reg [45:0] bus_request = 0,
    output reg [19:0] static_response = 0,
    output reg [31:0] pack_writes = 0
);
    localparam [127:0] BUILD_ID = 128'h00112233445566778899aabbccddeeff;
    wire [15:0] bus_address;
    wire [7:0] bus_data;
    wire mreq_n, iorq_n, rd_n, wr_n, m1_n, rfsh_n, cpu_clock;
    wire request, slot_reset, a_valid, b_valid;
    wire [7:0] diagnostic_address;
    wire [31:0] a_data, b_data;
    wire [19:0] a_bus, b_bus;
    // Observe the actual reset, including transport failure and PR freeze.
    // No internal machine/control state is driven or forced by this harness.
    assign machine_held = computer.core.machine_reset;
    always @(posedge clk_sys) begin
        static_response <= persona_b ? b_bus : a_bus;
        bus_request <= {!machine_held, cpu_clock, peek_address[13:0],
            !machine_held ? {rfsh_n, m1_n, wr_n, rd_n, iorq_n, mreq_n,
                             bus_data, bus_address} : {6'b111111, 24'd0}};
        // Count the real RAM write enable, including reset-pipeline tails.
        if (b.core.write_enable) pack_writes <= pack_writes + 1;
    end
    fes_de25_zx81_standard #(.BUILD_ID(BUILD_ID), .EXPANSION_SOCKET(1),
        .FIRMWARE_INIT("standard-expansion-rom.hex")) computer (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk),
        .reset_n(reset_n), .pixel_reset_n(reset_n), .video_ready(1'b1),
        .clock_fault(1'b0), .expansion_running(running),
        .computer_address(address), .computer_read(read), .computer_write(write),
        .computer_byteenable(byteenable), .computer_writedata(writedata),
        .computer_readdata(readdata), .computer_readdatavalid(readdatavalid),
        .computer_writeresponsevalid(writeresponsevalid), .computer_waitrequest(waitrequest),
        .computer_response(response),
        .scanout_address(7'd0), .scanout_read(1'b0), .scanout_write(1'b0),
        .scanout_byteenable(4'hf), .scanout_writedata(32'd0), .scanout_readdata(),
        .scanout_readdatavalid(), .scanout_writeresponsevalid(), .scanout_waitrequest(),
        .scanout_response(), .avm_address(), .avm_burstcount(),
        .avm_read(unexpected_dma), .avm_byteenable(), .avm_waitrequest(1'b1),
        .avm_readdata(128'd0), .avm_readdatavalid(1'b0), .avm_response(2'd0),
        .video(), .video_rgb(), .video_de(), .video_hs(), .video_vs(),
        .audio_sample(), .exec_reset(exec_reset), .bus_addr(bus_address),
        .bus_wdata(bus_data), .bus_mreq_n(mreq_n), .bus_iorq_n(iorq_n),
        .bus_rd_n(rd_n), .bus_wr_n(wr_n), .bus_m1_n(m1_n), .bus_rfsh_n(rfsh_n),
        .bus_cpu_clock(cpu_clock), .bus_rdata(static_response[`ZX81_BUS_DRD]),
        .bus_peek_data(static_response[`ZX81_BUS_PEEK_D]),
        .bus_dsel(static_response[`ZX81_BUS_DSEL]),
        .bus_romcs(static_response[`ZX81_BUS_ROMCS]),
        .bus_wait(static_response[`ZX81_BUS_WAIT]),
        .bus_ram_present(static_response[`ZX81_BUS_RAM_PRESENT]),
        .peek_addr(peek_address), .peek_data(peek_data), .cpu_addr(),
        .ram_write_enable(), .ui_active(), .display_faulted(), .display_quiesced()
    );
    fes_link_shell #(.BUILD_ID(BUILD_ID)) shell (
        .clk(clk_sys), .reset_n(reset_n), .address(pr_address),
        .read(pr_read), .write(pr_write), .byteenable(4'hf), .writedata(pr_writedata),
        .readdata(pr_readdata), .readdatavalid(pr_readdatavalid),
        .writeresponsevalid(pr_writeresponsevalid), .response(pr_response),
        .waitrequest(pr_waitrequest), .req(request), .slot_address(diagnostic_address),
        .slot_reset(slot_reset), .rsp_valid(persona_b ? b_valid : a_valid),
        .rsp_data(persona_b ? b_data : a_data), .frozen(frozen), .running(running),
        .isolated_data(), .static_count(), .retired(), .last_data()
    );
    fes_link_persona_a a (
        .clk(clk_sys), .reset(slot_reset || persona_b), .req(request),
        .address(diagnostic_address), .bus_request(bus_request),
        .bus_response(a_bus), .rsp_valid(a_valid), .rsp_data(a_data)
    );
    fes_link_persona_b b (
        .clk(clk_sys), .reset(slot_reset || !persona_b), .req(request),
        .address(diagnostic_address), .bus_request(bus_request),
        .bus_response(b_bus), .rsp_valid(b_valid), .rsp_data(b_data)
    );
endmodule
