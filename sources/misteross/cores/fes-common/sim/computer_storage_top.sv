// SPDX-License-Identifier: GPL-2.0-or-later
// fes_computer_mailbox_core with fes_computer_storage behind it, as a 1.1
// shell joins them, for computer_storage_tb's replay of the 1.1 golden
// exchanges. The bench plays the machine's side of the storage ports.
`include "fes_computer.vh"
module computer_storage_top #(
    parameter bit ENABLE_KEYBOARD = 0,
    parameter bit ENABLE_MOUSE = 0,
    parameter bit ENABLE_AUDIO = 0,
    parameter bit ENABLE_SESSION_DISPLAY = 0,
    parameter bit ENABLE_APPLE2_FLOPPY = 0,
    parameter bit ENABLE_ATA_DISK = 0,
    parameter bit ENABLE_PERSISTENCE = 0,
    parameter bit ENABLE_PC386_MACHINE = 0,
    parameter bit ENABLE_WALL_CLOCK = 0
) (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output wire         exec_reset,
    output wire [2:0]   disk_state,
    output wire [31:0]  disk_sectors,
    output wire [15:0]  disk_cylinders,
    output wire [4:0]   disk_heads,
    output wire [5:0]   disk_spt,
    output wire [31:0]  disk_crc,
    input  wire         disk_verify_done,
    input  wire         disk_verify_match,
    output wire         disk_frozen,
    input  wire         disk_fenced,
    input  wire         disk_busy,
    input  wire         disk_written,
    output wire         disk_clear,
    input  wire         disk_clearing,
    output wire [6:0]   disk_dirty_index,
    input  wire [15:0]  disk_dirty_word,
    output wire [127:0] machine_settings,
    input  wire         cmos_write,
    input  wire [6:0]   cmos_byte_index,
    input  wire [7:0]   cmos_byte,
    input  wire         cmos_load,
    input  wire [5:0]   cmos_load_pair,
    output wire [15:0]  cmos_load_word,
    output wire [2:0]   cmos_clock_index,
    input  wire [15:0]  cmos_clock_pair,
    output wire         wall_apply,
    output wire [15:0]  wall_year,
    output wire [7:0]   wall_month,
    output wire [7:0]   wall_day,
    output wire [7:0]   wall_hour,
    output wire [7:0]   wall_minute,
    output wire [7:0]   wall_second,
    output wire [7:0]   wall_weekday
);
    wire storage_request, storage_response_valid, storage_response_error, execution_accepted;
    wire [6:0] storage_opcode;
    wire [7:0] storage_index;
    wire [15:0] storage_argument, storage_response_data;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(ENABLE_KEYBOARD), .ENABLE_MOUSE(ENABLE_MOUSE), .ENABLE_AUDIO(ENABLE_AUDIO),
        .ENABLE_SESSION_DISPLAY(ENABLE_SESSION_DISPLAY), .ENABLE_APPLE2_FLOPPY(ENABLE_APPLE2_FLOPPY),
        .ENABLE_ATA_DISK(ENABLE_ATA_DISK), .ENABLE_PERSISTENCE(ENABLE_PERSISTENCE),
        .ENABLE_PC386_MACHINE(ENABLE_PC386_MACHINE), .ENABLE_WALL_CLOCK(ENABLE_WALL_CLOCK)
    ) gp_mailbox (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi), .exec_reset(exec_reset),
        .keyboard_rows(), .controller_buttons(),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b1),
        .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_unit(),
        .media_write_ready(1'b1), .media_write_busy(1'b0), .media_changed(1'b0), .media_reset(1'b0), .media_frozen(),
        .media_read_req(), .media_read_addr(), .media_read_ready(1'b0), .media_read_data(8'd0),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .unit0_state(), .unit0_size(), .unit1_state(), .unit1_size(),
        .display_request(), .display_opcode(), .display_index(), .display_argument(),
        .display_response_valid(1'b0), .display_response_error(1'b0), .display_response_data(16'd0),
        .display_quiesced(1'b1),
        .storage_request(storage_request), .storage_opcode(storage_opcode), .storage_index(storage_index),
        .storage_argument(storage_argument), .storage_response_valid(storage_response_valid),
        .storage_response_error(storage_response_error), .storage_response_data(storage_response_data),
        .execution_accepted(execution_accepted)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    fes_computer_storage #(
        .ENABLE_ATA_DISK(ENABLE_ATA_DISK), .ENABLE_DATA(ENABLE_PERSISTENCE && ENABLE_PC386_MACHINE),
        .ENABLE_WALL_CLOCK(ENABLE_WALL_CLOCK)
    ) storage (
        .clk(clk), .request(storage_request), .opcode(storage_opcode), .index(storage_index),
        .argument(storage_argument), .response_valid(storage_response_valid),
        .response_error(storage_response_error), .response_data(storage_response_data),
        .exec_reset(exec_reset), .execution_accepted(execution_accepted),
        .disk_state(disk_state), .disk_sectors(disk_sectors), .disk_cylinders(disk_cylinders),
        .disk_heads(disk_heads), .disk_spt(disk_spt), .disk_crc(disk_crc),
        .disk_verify_done(disk_verify_done), .disk_verify_match(disk_verify_match),
        .disk_frozen(disk_frozen), .disk_fenced(disk_fenced), .disk_busy(disk_busy),
        .disk_written(disk_written), .disk_clear(disk_clear), .disk_clearing(disk_clearing),
        .disk_dirty_index(disk_dirty_index), .disk_dirty_word(disk_dirty_word),
        .machine_settings(machine_settings), .cmos_write(cmos_write), .cmos_byte_index(cmos_byte_index),
        .cmos_byte(cmos_byte), .cmos_load(cmos_load), .cmos_load_pair(cmos_load_pair),
        .cmos_load_word(cmos_load_word), .cmos_clock_index(cmos_clock_index), .cmos_clock_pair(cmos_clock_pair),
        .wall_apply(wall_apply), .wall_year(wall_year), .wall_month(wall_month), .wall_day(wall_day),
        .wall_hour(wall_hour), .wall_minute(wall_minute), .wall_second(wall_second),
        .wall_weekday(wall_weekday)
    );
endmodule
