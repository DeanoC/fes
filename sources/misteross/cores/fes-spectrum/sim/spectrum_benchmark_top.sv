// SPDX-License-Identifier: GPL-2.0-or-later
// Registered expansion fixture; no external CPU or Sinclair firmware model.
`include "spectrum_bus.vh"
module spectrum_benchmark_top #(parameter bit FAST_CPU = 1'b0) (
    input wire clk_sys, reset, hold_wait, nmi,
    output wire [31:0] request,
    output wire peripheral_tick, frame_int_n, retired, illegal, halted,
    output wire [15:0] pc,
    output wire [7:0] sig8000, sig8001, sig8002, sig8003, sig8004,
    output reg [15:0] card_writes, ram_writes,
    output wire [7:0] ram8100,
    output wire [2:0] border
);
    wire [31:0] plug;
    wire [27:0] card_response, response1, response2, response3, response4;
    spectrum_machine #(.FAST_CPU(FAST_CPU), .ROM_FILE("build/diagnostics/fes-spectrum/benchmark.hex")) machine (
        .clk_sys(clk_sys), .reset(reset), .matrix(40'hffffffffff), .kempston(5'd0),
        .unit_state(2'd0), .unit_size(32'd0), .media_write_addr(16'd0),
        .media_write_data(16'd0), .media_write_enable(2'd0),
        .bus_request(request), .response1(response1), .response2(response2),
        .response3(response3), .response4(response4), .border(border),
        .flash_on(), .speaker(), .ear(), .cpu_cycle(peripheral_tick), .slot_audio(),
        .cpu_retired(retired), .cpu_illegal(illegal), .cpu_halted(halted),
        .cpu_pc(pc), .frame_int_n(frame_int_n),
        .video_clk(clk_sys), .video_addr(16'h4000), .video_data(),
        .sig8000(sig8000), .sig8001(sig8001), .sig8002(sig8002), .sig8003(sig8003), .sig8004(sig8004)
    );
    spectrum_slot_socket1 slot1 (.clock(clk_sys), .request(request), .response(response1),
                                .plug_request(plug), .plug_response(card_response));
    spectrum_slot_socket2 slot2 (.clock(clk_sys), .request(request), .response(response2),
                                .plug_request(), .plug_response(28'd0));
    spectrum_slot_socket3 slot3 (.clock(clk_sys), .request(request), .response(response3),
                                .plug_request(), .plug_response(28'd0));
    spectrum_slot_socket4 slot4 (.clock(clk_sys), .request(request), .response(response4),
                                .plug_request(), .plug_response(28'd0));
    wire io_select = plug[`SP_BUS_IORQ] && plug[7:0] == 8'he1;
    wire ram_select = plug[`SP_BUS_MREQ] && plug[`SP_BUS_A] == 16'h8100;
    wire rom_select = plug[`SP_BUS_MREQ] && plug[`SP_BUS_A] == 16'h2000;
    wire selected = io_select || ram_select;
    reg wait_armed = 1'b0;
    reg [7:0] scratch = 8'd0;
    always @(posedge clk_sys) begin
        if (plug[`SP_BUS_RESET]) begin
            card_writes <= 16'd0;
            scratch <= 8'd0;
            wait_armed <= 1'b0;
        end else begin
            if (!(plug[`SP_BUS_RD] || plug[`SP_BUS_WR])) wait_armed <= 1'b0;
            if (plug[`SP_BUS_STROBE] && selected) wait_armed <= 1'b1;
            if (plug[`SP_BUS_STROBE] && io_select && plug[`SP_BUS_WR]) begin
                card_writes <= card_writes + 16'd1;
                scratch <= plug[`SP_BUS_D];
            end
        end
    end
    assign card_response[`SP_BUS_RDATA] = rom_select ? 8'hc9 : scratch;
    assign card_response[`SP_BUS_DRIVE] = (io_select || rom_select) && plug[`SP_BUS_RD];
    assign card_response[`SP_BUS_ROMCS] = rom_select;
    assign card_response[`SP_BUS_NMI] = nmi;
    assign card_response[`SP_BUS_WAIT] = selected && wait_armed && hold_wait;
    assign card_response[`SP_BUS_AUDIO] = 16'd0;
    // Simulation observation only; this wrapper is excluded from production.
    assign ram8100 = machine.ram.mem[16'h4100];
    always @(posedge clk_sys) begin
        if (reset) ram_writes <= 16'd0;
        else if (machine.ram.cpu_we && machine.cpu_a == 16'h8100)
            ram_writes <= ram_writes + 16'd1;
    end
    initial card_writes = 16'd0;
endmodule
