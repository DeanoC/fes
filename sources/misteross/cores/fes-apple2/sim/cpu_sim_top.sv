// SPDX-License-Identifier: GPL-2.0-or-later
// Exercise the real Apple II motherboard and its divided CPU enable. A test
// card in slot 1 supplies IRQ; all other slots are vacant.
`include "apple2_bus.vh"

module apple2_cpu_sim_top (
    input wire clk_sys,
    input wire reset,
    input wire irq,
    output wire [15:0] bus_addr,
    output wire [7:0] bus_data,
    output wire bus_write,
    output wire bus_cycle
);
    wire [`A2_BUS_RSP*8-1:0] responses;
    assign responses = {{6*`A2_BUS_RSP{1'b0}},
                        {17'b0, 1'b0, irq, 9'b0},
                        {`A2_BUS_RSP{1'b0}}};
    apple2_machine machine (
        .clk_sys(clk_sys), .reset(reset), .reset_key(1'b0),
        .key_event(1'b0), .key_code(7'd0), .buttons(3'd0),
        .paddles(32'd0), .cassette_in(1'b0), .slot_response(responses),
        .slot_request(), .slot_devsel(), .slot_iosel(),
        .video_text(), .video_mixed(), .video_page2(), .video_hires(),
        .annunciators(), .video_clk(clk_sys), .video_addr(16'd0), .video_data(),
        .speaker(), .cassette_out(), .slot_audio(),
        .debug_pc_addr(bus_addr), .cpu_cycle(bus_cycle),
        .debug_bus_data(bus_data), .debug_bus_write(bus_write)
    );
endmodule
