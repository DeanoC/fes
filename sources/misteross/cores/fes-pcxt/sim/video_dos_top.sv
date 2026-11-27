// SPDX-License-Identifier: GPL-2.0-or-later
// Full 8088 PC/XT machine with runtime-selectable CGA, MDA, and Hercules video.
// Observations below do not drive any guest state.
module video_dos_top (
    input wire clk, clk_pixel, cold_reset, reset_pixel, warm_reset, hold,
    input wire [1:0] video_card,
    input wire [1:0] mono_tint,
    input wire firmware_valid,
    input wire [15:0] firmware_address,
    input wire [7:0] firmware_data,
    output wire firmware_ready, firmware_done, initialized, machine_reset, firmware_loaded,
    input wire [31:0] media_gpo,
    output wire [31:0] media_gpi,
    output wire [1:0] media_state,
    output wire disk_loaded,
    output wire media_read_ready,
    output wire [7:0] media_read_data,
    output wire media_read_valid,
    output wire [18:0] media_read_addr,
    output wire keyboard_clock_low, keyboard_data_low, keyboard_clear,
    output wire [1:0] fault,
    output wire halted, dma_owner, refresh_done, dma2_done, fdc_irq,
    output wire hercules_graphics_mode,
    output wire [31:0] video_request,
    output wire [27:0] video_response,
    output wire audio_valid,
    output wire signed [15:0] audio_sample,
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
    input wire [10:0] cga_text_address, mda_text_address,
    output wire [15:0] cga_text_word, mda_text_word
);
    wire keyboard_clock, keyboard_data;
    wire [143:0] keyboard_rows;
    wire valid_key, ready_key, frontend_reset;
    wire [7:0] key_code;
    pcxt_hid_keyboard frontend (
        .clk(clk), .reset(frontend_reset), .rows(keyboard_rows),
        .code_valid(valid_key), .code(key_code), .code_ready(ready_key)
    );
    pcxt_xt_keyboard_bat keyboard (
        .clk(clk), .reset(machine_reset),
        .code_valid(valid_key), .code(key_code), .code_ready(ready_key), .frontend_reset(frontend_reset),
        .clock_low(keyboard_clock_low), .data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear), .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data)
    );

    wire [11:0] cga_font_address, mda_font_address;
    reg [7:0] cga_font [0:4095];
    reg [7:0] mda_font [0:4095];
    initial begin
        $readmemh("cga_font.hex", cga_font);
        $readmemh("mda_font.hex", mda_font);
    end

    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;

    wire media_present, disk_restart, disk_load_valid, disk_load_ready, disk_load_done;
    wire [18:0] disk_load_address;
    wire [7:0] disk_load_data;
    wire runtime_hold;
    wire [18:0] source_addr;
    wire [15:0] source_data;
    wire [1:0] source_enable;
    wire source_ready;
    wire [31:0] unused_unit_size;
    wire media_frozen, media_write_busy, media_changed, snapshot_read_valid, snapshot_read_ready;
    wire [19:0] snapshot_read_address;
    wire [7:0] snapshot_read_data;

    /* verilator lint_off PINCONNECTEMPTY */
    fes_computer_mailbox_pcxt #(.ENABLE_KEYBOARD(1), .ENABLE_WRITE(0)) gp_mailbox (
        .clk(clk), .gpo(media_gpo), .build_id(128'd0), .gpi(media_gpi),
        .exec_reset(runtime_hold), .media_reset(cold_reset),
        .keyboard_rows(keyboard_rows),
        .media_write_addr(source_addr), .media_write_data(source_data), .media_write_enable(source_enable),
        .media_write_ready(source_ready), .media_changed(media_changed), .media_frozen(media_frozen),
        .media_read_req(snapshot_read_valid), .media_read_addr(snapshot_read_address),
        .media_read_ready(snapshot_read_ready), .media_read_data(snapshot_read_data),
        .media_write_busy(media_write_busy), .unit0_state(media_state), .unit0_size(unused_unit_size)
    );
    pcxt_media adapter (
        .clk(clk), .cold_reset(cold_reset), .unit_state(media_state),
        .source_addr(source_addr), .source_data(source_data), .source_enable(source_enable),
        .source_ready(source_ready), .disk_restart(disk_restart), .media_present(media_present),
        .disk_loaded(disk_loaded), .disk_valid(disk_load_valid), .disk_address(disk_load_address),
        .disk_data(disk_load_data), .disk_ready(disk_load_ready), .disk_done(disk_load_done)
    );
    assign snapshot_read_valid = 1'b0;
    assign snapshot_read_address = 20'b0;

    wire start_without_media = firmware_loaded && !hold && !runtime_hold && media_state == 2'd1;
    wire boot_media_hold;
    pcxt_boot_media_gate #(.ENABLE(1)) media_gate (
        .clk(clk), .cold_reset(cold_reset), .media_present(media_present),
        .start_without_media(start_without_media), .hold(boot_media_hold)
    );

    wire [1:0] display_pixel_ce, display_de, display_sof, display_eol;
    wire [7:0] display_rgbi;
    wire [31:0] unused_fixed_req;
    wire [27:0] unused_fixed_rsp;

    pcxt_system #(.DIAGNOSTIC_PROBE(0), .FLOPPY(1), .FLOPPY_WRITE(0), .HERCULES(1),
                  .SWITCHES(8'h2d)) system (
        .video_card(video_card), .hercules_graphics_mode(hercules_graphics_mode),
        .display_pixel_ce(display_pixel_ce), .display_de(display_de),
        .display_sof(display_sof), .display_eol(display_eol), .display_rgbi(display_rgbi),
        .media_frozen(media_frozen), .media_write_busy(media_write_busy), .media_changed(media_changed),
        .snapshot_read_valid(snapshot_read_valid), .snapshot_read_address(snapshot_read_address),
        .snapshot_read_ready(snapshot_read_ready), .snapshot_read_data(snapshot_read_data),
        .clk(clk), .clk_cpu(clk), .clk_pixel(clk_pixel), .clk_pin(clk), .clk_handoff(clk),
        .cold_reset(cold_reset), .reset_pixel(reset_pixel), .warm_reset(warm_reset),
        .hold(hold | runtime_hold | boot_media_hold),
        .media_present(media_present), .disk_restart(disk_restart),
        .disk_load_valid(disk_load_valid), .disk_load_ready(disk_load_ready), .disk_load_done(disk_load_done),
        .disk_load_address(disk_load_address), .disk_load_data(disk_load_data), .disk_loaded(disk_loaded),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear),
        .cga_font_address(cga_font_address), .mda_font_address(mda_font_address),
        .cga_font_data(cga_font[cga_font_address]), .mda_font_data(mda_font[mda_font_address]),
        .firmware_valid(firmware_valid), .firmware_ready(firmware_ready), .firmware_done(firmware_done),
        .firmware_address(firmware_address), .firmware_data(firmware_data), .firmware_loaded(firmware_loaded),
        .initialized(initialized), .machine_reset(machine_reset), .fault(fault), .halted(halted),
        .dma_owner(dma_owner), .refresh_done(refresh_done),
        .video_request(unused_fixed_req), .video_response(unused_fixed_rsp),
        .audio_valid(audio_valid), .audio_sample(audio_sample),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs),
        .sdram_nras(sdram_nras), .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe),
        .sdram_ba(sdram_ba), .sdram_a(sdram_a), .sdram_dqml(sdram_dqml), .sdram_dqmh(sdram_dqmh),
        .dq_out(dq_out), .dq_oe(dq_oe), .dq_rise(dq_rise), .dq_fall(dq_fall)
    );

    wire select_mono = (video_card == 2'd1 || video_card == 2'd2);
    wire [1:0] source_mode = !select_mono ? 2'd0 : (hercules_graphics_mode ? 2'd2 : 2'd1);
    pcxt_video_dynamic_output #(.SCANLINES(0)) dynamic_video (
        .clk_sys(clk), .clk_pixel(clk_pixel), .reset_sys(cold_reset), .reset_pixel(reset_pixel),
        .hold(machine_reset),
        .pixel_ce(display_pixel_ce[select_mono]),
        .source_de(display_de[select_mono]),
        .source_sof(display_sof[select_mono]),
        .source_eol(display_eol[select_mono]),
        .rgbi(select_mono ? display_rgbi[7:4] : display_rgbi[3:0]),
        .source_mode(source_mode),
        .mono_tint(mono_tint),
        .video_request(video_request), .video_response(video_response)
    );
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
    assign instruction_address = {system.machine.cpu.cpu.u_biu_core.biu_register_cs, 4'b0} +
                                 {4'b0, system.machine.cpu.cpu.u_biu_core.pfq_addr_out};
    assign post_flags = system.machine.cpu.cpu.u_eu_core.eu_register_bp;
    assign cga_text_word = system.machine.isa.slots[1].cga.card.card.vram[{2'b0, cga_text_address}];
    assign mda_text_word = system.machine.isa.slots[2].hercules.card.vram[{4'b0, mda_text_address}];
    wire unused_fixed_video = ^{unused_fixed_req, unused_fixed_rsp};
endmodule
