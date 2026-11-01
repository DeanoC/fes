// SPDX-License-Identifier: GPL-2.0-or-later
// 8088 + motherboard + both ISA graphics cards + physical SDRAM controller.
module memory_machine_top (
    input wire clk, reset,
    output wire [19:0] address,
    output wire [7:0] wdata,
    output wire write_cycle,
    output wire [1:0] fault,
    output wire guest_ce, halted,
    output wire accepted, completed,
    output wire [1:0] cycle_kind,
    output wire [7:0] cycle_rdata,
    output wire [11:0] cga_font_address, mda_font_address,
    input wire [7:0] cga_font_data, mda_font_data,
    output wire [1:0] pixel_ce, de, hs, vs, sof, eol,
    output wire [7:0] rgbi,
    input wire cold_reset,
    output wire initialized,
    input wire firmware_valid,
    output wire firmware_ready,
    input wire [15:0] firmware_address,
    input wire [7:0] firmware_data,
    output wire firmware_done,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_sample
);
    wire memory_valid, memory_ready, memory_done;
    wire [7:0] memory_rdata;
    display_machine_top machine (
        .clk(clk),
        .reset(reset),
        .fault(fault),
        .guest_ce(guest_ce),
        .halted(halted),
        .accepted(accepted),
        .completed(completed),
        .cycle_kind(cycle_kind),
        .cycle_rdata(cycle_rdata),
        .address(address),
        .wdata(wdata),
        .write_cycle(write_cycle),
        .cga_font_address(cga_font_address),
        .mda_font_address(mda_font_address),
        .cga_font_data(cga_font_data),
        .mda_font_data(mda_font_data),
        .pixel_ce(pixel_ce),
        .de(de),
        .hs(hs),
        .vs(vs),
        .sof(sof),
        .eol(eol),
        .rgbi(rgbi),
        .memory_valid(memory_valid),
        .memory_ready(memory_ready),
        .memory_done(memory_done),
        .memory_rdata(memory_rdata)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    memory_top storage (.disk_load_valid(1'b0), .disk_load_ready(),
        .disk_load_address(19'b0), .disk_load_data(8'b0), .disk_load_done(),
        .media_read_valid(1'b0), .media_read_addr(19'b0), .media_read_ready(), .media_read_data(), .*);
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
