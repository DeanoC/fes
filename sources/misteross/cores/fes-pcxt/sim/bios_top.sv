// SPDX-License-Identifier: GPL-2.0-or-later
// Real assembled machine. Observations below do not drive any guest state.
module bios_top #(parameter bit WITH_FLOPPY = 0, WITH_RUNTIME_MEDIA = 0, WAIT_FOR_MEDIA = 0) (
    input wire clk, clk_pixel, cold_reset, reset_pixel, warm_reset, hold,
    input wire firmware_valid,
    input wire [15:0] firmware_address,
    input wire [7:0] firmware_data,
    output wire firmware_ready, firmware_done, initialized, machine_reset, firmware_loaded,
    input wire media_present,
    input wire disk_restart,
    input wire disk_load_valid,
    input wire [18:0] disk_load_address,
    input wire [7:0] disk_load_data,
    output wire disk_load_ready, disk_load_done, disk_loaded,
    input wire [31:0] media_gpo,
    output wire [31:0] media_gpi,
    output wire [1:0] media_state,
    output wire media_read_ready,
    output wire [7:0] media_read_data,
    output wire media_read_valid,
    output wire [18:0] media_read_addr,
    input wire code_valid,
    input wire [7:0] key_code,
    output wire code_ready, keyboard_clock_low, keyboard_data_low, keyboard_clear,
    output wire [1:0] fault,
    output wire halted, dma_owner, refresh_done, dma2_done, fdc_irq,
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
        .code_valid(code_valid), .code(key_code), .code_ready(code_ready),
        .clock_low(keyboard_clock_low), .data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear), .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data));
    wire [11:0] cga_font_address, mda_font_address;
    reg [7:0] font [0:4095];
    initial $readmemh("font.hex", font);
    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;
    wire active_media_present, active_disk_restart, active_disk_valid, active_disk_ready, active_disk_done;
    wire [18:0] active_disk_address;
    wire [7:0] active_disk_data;
    wire runtime_hold;
    generate if (WITH_RUNTIME_MEDIA) begin: runtime_media
        wire [18:0] source_addr;
        wire [15:0] source_data;
        wire [1:0] source_enable;
        wire source_ready;
        wire [31:0] unused_unit_size;
        fes_computer_mailbox_pcxt gp_mailbox (
            .clk(clk), .gpo(media_gpo), .build_id(128'd0), .gpi(media_gpi),
            .exec_reset(runtime_hold), .media_reset(cold_reset),
            .media_write_addr(source_addr), .media_write_data(source_data), .media_write_enable(source_enable),
            .media_write_ready(source_ready), .media_write_busy(1'b0), .unit0_state(media_state), .unit0_size(unused_unit_size));
        pcxt_media adapter (
            .clk(clk), .cold_reset(cold_reset), .unit_state(media_state),
            .source_addr(source_addr), .source_data(source_data), .source_enable(source_enable),
            .source_ready(source_ready), .disk_restart(active_disk_restart), .media_present(active_media_present),
            .disk_loaded(disk_loaded), .disk_valid(active_disk_valid), .disk_address(active_disk_address),
            .disk_data(active_disk_data), .disk_ready(active_disk_ready), .disk_done(active_disk_done));
        assign disk_load_ready = 1'b0;
        assign disk_load_done = 1'b0;
        wire unused_direct = &{1'b0, media_present, disk_restart, disk_load_valid, disk_load_address, disk_load_data};
    end else begin: direct_media
        assign runtime_hold = 1'b0;
        assign media_gpi = 0;
        assign media_state = 0;
        assign active_media_present = media_present;
        assign active_disk_restart = disk_restart;
        assign active_disk_valid = disk_load_valid;
        assign active_disk_address = disk_load_address;
        assign active_disk_data = disk_load_data;
        assign disk_load_ready = active_disk_ready;
        assign disk_load_done = active_disk_done;
        wire unused_gpo = &{1'b0,media_gpo};
    end endgenerate
    wire start_without_media = firmware_loaded && !hold && !runtime_hold && media_state == 2'd1;
    wire boot_media_hold;
    pcxt_boot_media_gate #(.ENABLE(WAIT_FOR_MEDIA)) media_gate (
        .clk(clk), .cold_reset(cold_reset), .media_present(active_media_present),
        .start_without_media(start_without_media), .hold(boot_media_hold));
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_system #(.DIAGNOSTIC_PROBE(0), .FLOPPY(WITH_FLOPPY),
                  .SWITCHES(WITH_FLOPPY ? 8'h2d : 8'h2c)) system (
        .clk_cpu(clk), .clk_pin(clk), .hold(hold | runtime_hold | boot_media_hold),
        .media_present(active_media_present), .disk_restart(active_disk_restart),
        .disk_load_valid(active_disk_valid), .disk_load_ready(active_disk_ready), .disk_load_done(active_disk_done),
        .disk_load_address(active_disk_address), .disk_load_data(active_disk_data),
        .audio_valid(), .audio_sample(),
        .cga_font_data(font[cga_font_address]), .mda_font_data(font[mda_font_address]), .*);
    /* verilator lint_on PINCONNECTEMPTY */
    assign media_read_valid = system.media_read_valid;
    assign media_read_addr = system.media_read_addr;
    assign media_read_ready = system.media_read_ready;
    assign media_read_data = system.media_read_data;
    assign dma2_done = system.machine.isa_done && system.machine.aen &&
                       system.machine.kind == 2'd3 && system.machine.dma_channel == 2'd2;
    assign fdc_irq = system.machine.irq[6];
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
