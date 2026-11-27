// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
// PC/XT DOS machine, control, media and video for a board without linked ROM
// lanes (fes.computer 1.0 with fes.fabric.firmware-upload). The machine is
// the DE10 package's pcxt_system with its DOS profile: GLaBIOS, the 8088,
// the motherboard, CGA and MDA cards, the XT floppy adapter with sector
// writes, the HID keyboard and the speaker, with conventional RAM, the
// firmware window and the floppy image in one SDRAM rank through the DE10
// core's own pcxt_memory and CAS-3 capture pipeline, on the same 100 MHz
// clocks. This replaces fes_pcxt_top's shell: the firmware arrives through
// the mailbox front end before execution can be released (fes_pcxt_firmware),
// and the 720p word leaves for the session display.
//
// clk_sys is the machine's 100 MHz. clk_forward (+1.25 ns) clocks the
// forwarded SDRAM clock, clk_capture the DQ input registers and clk_handoff
// the CAS-3 read-word handoff, the DE10 package's four memory PLL outputs;
// the board places the capture phase for its pads, inside the same read
// window. Board wrappers own the PLL, HPS transport, the SDRAM
// pads, HDMI and audio serialization. No FPGA-family primitive belongs here.
module fes_pcxt_core #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk_sys, clk_forward, clk_capture, clk_handoff, pixel_clk,
    // cold_reset restarts SDRAM initialization; reset_hold holds the machine.
    input wire cold_reset, pixel_reset, reset_hold,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire machine_held,
    output wire [27:0] machine_video,
    output wire signed [15:0] audio_sample,
    output wire display_request,
    output wire [6:0] display_opcode,
    output wire [7:0] display_index,
    output wire [15:0] display_argument,
    input wire display_response_valid, display_response_error,
    input wire [15:0] display_response_data,
    input wire display_quiesced,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire sdram_dqml, sdram_dqmh,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_in
);
    // The upload image: the 64 KiB system ROM window, the 4 KiB CGA font,
    // and the 4 KiB MDA font.
    localparam [31:0] FIRMWARE_BYTES = 32'd73728;
    wire [31:0] mailbox_gpo, mailbox_gpi;
    wire [16:0] firmware_write_addr;
    wire [15:0] firmware_write_data;
    wire [1:0] firmware_write_enable;
    wire firmware_write_ready;
    fes_firmware_upload #(.AW(17), .BYTES(FIRMWARE_BYTES)) firmware (
        .clk(clk_sys), .gpo(gpo), .gpi(gpi),
        .mailbox_gpo(mailbox_gpo), .mailbox_gpi(mailbox_gpi), .exec_reset(exec_reset),
        .ready(), .write_addr(firmware_write_addr), .write_data(firmware_write_data),
        .write_enable(firmware_write_enable), .write_ready(firmware_write_ready)
    );

    // The DE10 shell's board reset is the PLL lock; here it is the board's
    // cold reset, or a new firmware image starting at byte 0. Both are a
    // power-on: SDRAM initialization, the BIOS warm-boot word and the floppy.
    wire firmware_restart;
    reg machine_cold = 1'b1;
    always @(posedge clk_sys) machine_cold <= cold_reset || firmware_restart;

    wire [143:0] keyboard_rows;
    wire media_frozen, media_write_busy, media_changed, snapshot_read_valid, snapshot_read_ready;
    wire [19:0] snapshot_read_address;
    wire [7:0] snapshot_read_data;
    wire media_present, disk_restart, disk_load_valid, disk_load_ready, disk_load_done, disk_loaded;
    wire [1:0] media_unit_state;
    wire [18:0] disk_load_address;
    wire [7:0] disk_load_data;
    wire [18:0] source_addr;
    wire [15:0] source_data;
    wire [1:0] source_enable;
    wire source_ready;
    wire [1:0] video_card, mono_tint;
    wire storage_request, storage_response_valid, storage_response_error, execution_accepted;
    wire [6:0] storage_opcode;
    wire [7:0] storage_index;
    wire [15:0] storage_argument, storage_response_data;
    /* verilator lint_off PINCONNECTEMPTY */
    // The DE10 package's fes_computer_mailbox_pcxt profile, with the session
    // display, fes.pcxt.machine settings, and the shell's gamepad ports.
    fes_computer_mailbox_core #(
        .ENABLE_PCXT_FLOPPY(1), .ENABLE_PCXT_FLOPPY_WRITE(1),
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1),
        .ENABLE_PERSISTENCE(1), .ENABLE_PCXT_MACHINE(1),
        .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(19),
        .ENABLE_SESSION_DISPLAY(1), .ENABLE_HPS_DDR_WINDOW(0),
        .UNIT0_MIN(`FES_COMPUTER_PCXT_FLOPPY_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_PCXT_FLOPPY_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(mailbox_gpo), .build_id(BUILD_ID), .gpi(mailbox_gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows), .controller_buttons(),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_addr(source_addr), .media_write_data(source_data),
        .media_write_enable(source_enable), .media_write_unit(),
        .media_write_ready(source_ready), .media_write_busy(media_write_busy),
        .media_changed(media_changed), .media_reset(machine_cold), .media_frozen(media_frozen),
        .media_read_req(snapshot_read_valid), .media_read_addr(snapshot_read_address),
        .media_read_ready(snapshot_read_ready), .media_read_data(snapshot_read_data),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .unit0_state(media_unit_state), .unit0_size(), .unit1_state(), .unit1_size(),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid),
        .display_response_error(display_response_error),
        .display_response_data(display_response_data),
        .display_quiesced(display_quiesced),
        .storage_request(storage_request), .storage_opcode(storage_opcode),
        .storage_index(storage_index), .storage_argument(storage_argument),
        .storage_response_valid(storage_response_valid),
        .storage_response_error(storage_response_error),
        .storage_response_data(storage_response_data),
        .execution_accepted(execution_accepted)
    );
    pcxt_machine_settings machine_settings (
        .clk(clk_sys), .exec_reset(exec_reset), .execution_accepted(execution_accepted),
        .request(storage_request), .opcode(storage_opcode), .index(storage_index),
        .argument(storage_argument), .response_valid(storage_response_valid),
        .response_error(storage_response_error), .response_data(storage_response_data),
        .video_card(video_card), .mono_tint(mono_tint)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    pcxt_media media (
        .clk(clk_sys), .cold_reset(machine_cold), .unit_state(media_unit_state),
        .source_addr(source_addr), .source_data(source_data), .source_enable(source_enable),
        .source_ready(source_ready), .disk_restart(disk_restart), .media_present(media_present),
        .disk_loaded(disk_loaded), .disk_valid(disk_load_valid), .disk_address(disk_load_address),
        .disk_data(disk_load_data), .disk_ready(disk_load_ready), .disk_done(disk_load_done));

    wire firmware_valid, firmware_ready, firmware_done, firmware_loaded;
    wire [15:0] firmware_address;
    wire [7:0] firmware_data;
    wire [11:0] cga_font_address, mda_font_address;
    wire [7:0] cga_font_data, mda_font_data;
    fes_pcxt_firmware loader (
        .clk(clk_sys), .cold_reset(machine_cold), .restart(firmware_restart),
        .write_addr(firmware_write_addr), .write_data(firmware_write_data),
        .write_enable(firmware_write_enable), .write_ready(firmware_write_ready),
        .firmware_valid(firmware_valid), .firmware_address(firmware_address), .firmware_data(firmware_data),
        .firmware_ready(firmware_ready), .firmware_done(firmware_done),
        .cga_font_address(cga_font_address), .mda_font_address(mda_font_address),
        .cga_font_data(cga_font_data), .mda_font_data(mda_font_data)
    );

    wire start_without_media = firmware_loaded && !exec_reset &&
                               media_unit_state == 2'(`FES_COMPUTER_MEDIA_STATE_EMPTY);
    wire boot_media_hold;
    pcxt_boot_media_gate #(.ENABLE(1)) media_gate (
        .clk(clk_sys), .cold_reset(machine_cold),
        .media_present(media_present), .start_without_media(start_without_media),
        .hold(boot_media_hold));

    wire machine_reset, keyboard_clock, keyboard_data;
    wire keyboard_clock_low, keyboard_data_low, keyboard_clear;
    wire code_valid, code_ready, frontend_reset;
    wire [7:0] code;
    pcxt_hid_keyboard frontend (
        .clk(clk_sys), .reset(frontend_reset), .rows(keyboard_rows),
        .code_valid(code_valid), .code(code), .code_ready(code_ready)
    );
    pcxt_xt_keyboard_bat keyboard (
        .clk(clk_sys), .reset(machine_reset),
        .code_valid(code_valid), .code(code), .code_ready(code_ready), .frontend_reset(frontend_reset),
        .clock_low(keyboard_clock_low), .data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data)
    );

    // Rate-3 consumes the falling-edge sample. As on the DE10 board, each DQ
    // pin is captured by its own register on the capture clock; the pipeline
    // retains the selected word.
    reg [15:0] dq_sample;
    always @(posedge clk_capture) dq_sample <= dq_in;
    wire speaker_valid, hercules_graphics_mode;
    wire [1:0] display_pixel_ce, display_de, display_sof, display_eol;
    wire [7:0] display_rgbi;
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_system #(.MDA(0), .SCANLINES(0), .HALF_RATE_BOARD(0), .SDRAM_CAPTURE_PIPELINE(1),
                  .EXTERNAL_CS_REGISTER(0), .FLOPPY(1), .FLOPPY_WRITE(1), .HERCULES(1), .DIAGNOSTIC_PROBE(0)) system (
        .video_card(video_card), .hercules_graphics_mode(hercules_graphics_mode),
        .display_pixel_ce(display_pixel_ce), .display_de(display_de), .display_sof(display_sof),
        .display_eol(display_eol), .display_rgbi(display_rgbi),
        .media_frozen(media_frozen), .media_write_busy(media_write_busy), .media_changed(media_changed),
        .snapshot_read_valid(snapshot_read_valid), .snapshot_read_address(snapshot_read_address),
        .snapshot_read_ready(snapshot_read_ready), .snapshot_read_data(snapshot_read_data),
        .media_present(media_present), .disk_restart(disk_restart), .disk_load_valid(disk_load_valid),
        .disk_load_address(disk_load_address),
        .disk_load_data(disk_load_data), .disk_load_ready(disk_load_ready), .disk_load_done(disk_load_done),
        .disk_loaded(disk_loaded),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear),
        .cga_font_address(cga_font_address), .mda_font_address(mda_font_address),
        .cga_font_data(cga_font_data), .mda_font_data(mda_font_data),
        .clk(clk_sys), .clk_cpu(clk_sys), .clk_pixel(pixel_clk), .clk_pin(clk_forward),
        .clk_handoff(clk_handoff), .cold_reset(machine_cold),
        .reset_pixel(pixel_reset), .warm_reset(exec_reset), .hold(boot_media_hold || reset_hold),
        .firmware_valid(firmware_valid), .firmware_ready(firmware_ready),
        .firmware_done(firmware_done), .firmware_address(firmware_address), .firmware_data(firmware_data),
        .firmware_loaded(firmware_loaded), .initialized(), .machine_reset(machine_reset), .fault(), .halted(),
        .dma_owner(), .refresh_done(), .audio_valid(speaker_valid), .audio_sample(audio_sample),
        .video_request(), .video_response(),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs),
        .sdram_nras(sdram_nras), .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe),
        .sdram_ba(sdram_ba), .sdram_a(sdram_a), .sdram_dqml(sdram_dqml), .sdram_dqmh(sdram_dqmh),
        .dq_out(dq_out), .dq_oe(dq_oe), .dq_rise(dq_sample), .dq_fall(dq_sample)
    );
    wire select_mono = (video_card == 2'd1 || video_card == 2'd2);
    wire [1:0] source_mode = !select_mono ? 2'd0 : (hercules_graphics_mode ? 2'd2 : 2'd1);
    pcxt_video_dynamic_output #(.SCANLINES(0)) video (
        .clk_sys(clk_sys), .clk_pixel(pixel_clk), .reset_sys(machine_cold), .reset_pixel(pixel_reset),
        .hold(machine_reset), .pixel_ce(display_pixel_ce[select_mono]),
        .source_de(display_de[select_mono]), .source_sof(display_sof[select_mono]),
        .source_eol(display_eol[select_mono]), .rgbi(select_mono ? display_rgbi[7:4] : display_rgbi[3:0]),
        .source_mode(source_mode), .mono_tint(mono_tint),
        .video_request(), .video_response(machine_video)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    // The DE10 audio hold: any machine hold, a cold start or the host's hold.
    reg audio_hold = 1'b1;
    always @(posedge clk_sys) audio_hold <= machine_reset || machine_cold || exec_reset;
    assign machine_held = audio_hold;
    wire unused = speaker_valid;
endmodule
