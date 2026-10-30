// SPDX-License-Identifier: GPL-2.0-or-later
// Real assembled machine. Observations below do not drive any guest state.
module bios_top (
    input wire clk, clk_pixel, cold_reset, reset_pixel, warm_reset,
    input wire firmware_valid,
    input wire [15:0] firmware_address,
    input wire [7:0] firmware_data,
    output wire firmware_ready, firmware_done, initialized, machine_reset, firmware_loaded,
    input wire code_valid,
    input wire [7:0] code,
    output wire code_ready, keyboard_clock_low, keyboard_data_low, keyboard_clear,
    output wire [1:0] fault,
    output wire halted, dma_owner, refresh_done,
    output wire [31:0] video_request,
    output wire [27:0] video_response,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_sample,
    output wire bus_done, bus_write,
    output wire [1:0] bus_kind,
    output wire [19:0] bus_address,
    output wire [7:0] bus_data,
    output wire [15:0] post_flags,
    output wire [19:0] instruction_address,
    output wire instruction_consumed,
    input wire [10:0] text_address,
    output wire [15:0] text_word
);
    wire keyboard_clock, keyboard_data;
    pcxt_xt_keyboard keyboard (.clk(clk), .reset(machine_reset),
        .code_valid(code_valid), .code(code), .code_ready(code_ready),
        .clock_low(keyboard_clock_low), .data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear), .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data));
    wire [11:0] cga_font_address, mda_font_address;
    reg [7:0] font [0:4095];
    initial $readmemh("font.hex", font);
    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_system #(.DIAGNOSTIC_PROBE(0), .SWITCHES(8'h2c)) system (
        .clk_cpu(clk), .clk_pin(clk), .hold(1'b0), .audio_valid(), .audio_sample(),
        .cga_font_data(font[cga_font_address]), .mda_font_data(font[mda_font_address]), .*);
    /* verilator lint_on PINCONNECTEMPTY */
    assign bus_done = system.machine.completed;
    assign bus_address = system.machine.cycle_address;
    assign bus_kind = system.machine.cycle_kind;
    assign bus_write = system.machine.cycle_write;
    assign bus_data = system.machine.cycle_write ? system.machine.cycle_wdata : system.machine.cycle_rdata;
    assign instruction_consumed = system.machine.cpu.cpu.u_biu_core.eu_biu_strobe == 2'b01 &&
                                  !system.machine.cpu.cpu.u_biu_core.pfq_empty &&
                                  !(system.machine.cpu.cpu.u_biu_core.eu_biu_req_caught &&
                                    system.machine.cpu.cpu.u_biu_core.eu_biu_req_code == 5'h19);
    assign instruction_address = {system.machine.cpu.cpu.u_biu_core.biu_register_cs, 4'b0} + {4'b0, system.machine.cpu.cpu.u_biu_core.pfq_addr_out};
    assign post_flags = system.machine.cpu.cpu.u_eu_core.eu_register_bp;
    assign text_word = system.machine.isa.slots[1].cga.card.card.vram[{2'b0, text_address}];
endmodule
