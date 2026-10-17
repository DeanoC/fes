// SPDX-License-Identifier: GPL-2.0-or-later
// Lockstep of fes_computer_mailbox and the Atari ST direct mailbox. The
// profiles share one host request so a protocol edit has to land in both
// endpoints. input_ replays the keyboard fixture, media_ the floppy fixture,
// st_ the parameters the Atari ST shell actually sets, and snap_ a small
// writable disk that can finish a snapshot read.
module mailbox_direct_compare_top (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    input  wire         mouse_ready,
    input  wire         media_write_ready,
    input  wire         media_write_busy,
    input  wire         media_changed,
    input  wire         media_read_ready,
    input  wire [7:0]   media_read_data,
    output wire [3:0]   diverge,
    output wire [31:0]  gpi_input,
    output wire [31:0]  gpi_media,
    output wire [31:0]  gpi_st,
    output wire         exec_reset_input,
    output wire [143:0] keyboard_rows_input,
    output wire [15:0]  controller_input,
    output wire [1:0]   unit0_state_media,
    output wire [31:0]  unit0_size_media,
    output wire [17:0]  media_write_addr_media,
    output wire [15:0]  media_write_data_media,
    output wire [1:0]   media_write_enable_media,
    output wire         exec_reset_st,
    output wire [143:0] keyboard_rows_st,
    output wire [15:0]  controller_st,
    output wire [1:0]   unit0_state_st,
    output wire [31:0]  unit0_size_st,
    output wire [19:0]  media_write_addr_st,
    output wire [15:0]  media_write_data_st,
    output wire [1:0]   media_write_enable_st,
    output wire         mouse_valid_st,
    output wire signed [15:0] mouse_dx_st,
    output wire signed [15:0] mouse_dy_st,
    output wire [31:0]  gpi_snap,
    output wire [1:0]   unit0_state_snap,
    output wire [31:0]  unit0_size_snap,
    output wire         media_frozen_snap,
    output wire [19:0]  media_write_addr_snap,
    output wire [15:0]  media_write_data_snap,
    output wire [1:0]   media_write_enable_snap,
    output wire         media_read_req_snap,
    output wire [19:0]  media_read_addr_snap
);
    wire [31:0] gpi_input_d, gpi_media_d, gpi_st_d;
    wire exec_reset_input_d, exec_reset_media, exec_reset_media_d, exec_reset_st_d;
    wire [143:0] keyboard_input_d, keyboard_media, keyboard_media_d, keyboard_st_d;
    wire [15:0] controller_input_d, controller_media, controller_media_d, controller_st_d;
    wire [1:0] unit0_state_input, unit0_state_input_d, unit0_state_media_d, unit0_state_st_d;
    wire [31:0] unit0_size_input, unit0_size_input_d, unit0_size_media_d, unit0_size_st_d;
    wire [17:0] media_addr_input, media_addr_input_d, media_addr_media_d;
    wire [19:0] media_addr_st_d;
    wire [15:0] media_data_input, media_data_input_d, media_data_media_d, media_data_st_d;
    wire [1:0] media_enable_input, media_enable_input_d, media_enable_media_d, media_enable_st_d;
    wire mouse_valid_input, mouse_valid_input_d, mouse_valid_media, mouse_valid_media_d, mouse_valid_st_d;
    wire signed [15:0] mouse_dx_input, mouse_dx_input_d, mouse_dx_media, mouse_dx_media_d, mouse_dx_st_d;
    wire signed [15:0] mouse_dy_input, mouse_dy_input_d, mouse_dy_media, mouse_dy_media_d, mouse_dy_st_d;
    wire [1:0] mouse_buttons_input, mouse_buttons_input_d, mouse_buttons_media, mouse_buttons_media_d;
    wire [1:0] mouse_buttons_st, mouse_buttons_st_d;
    wire media_frozen_input, media_frozen_input_d, media_frozen_media, media_frozen_media_d;
    wire media_frozen_st, media_frozen_st_d;
    wire media_read_req_input, media_read_req_input_d, media_read_req_media, media_read_req_media_d;
    wire media_read_req_st, media_read_req_st_d;
    wire [19:0] media_read_addr_input, media_read_addr_input_d, media_read_addr_media, media_read_addr_media_d;
    wire [19:0] media_read_addr_st, media_read_addr_st_d;

    fes_computer_mailbox #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1)
    ) input_shared (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_input),
        .exec_reset(exec_reset_input), .keyboard_rows(keyboard_rows_input),
        .controller_buttons(controller_input),
        .mouse_valid(mouse_valid_input), .mouse_dx(mouse_dx_input), .mouse_dy(mouse_dy_input),
        .mouse_buttons(mouse_buttons_input), .mouse_ready(mouse_ready),
        .media_write_addr(media_addr_input), .media_write_data(media_data_input),
        .media_write_enable(media_enable_input), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_input),
        .media_read_req(media_read_req_input), .media_read_addr(media_read_addr_input),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_input), .unit0_size(unit0_size_input)
    );
    fes_computer_mailbox_direct #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1)
    ) input_direct (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_input_d),
        .exec_reset(exec_reset_input_d), .keyboard_rows(keyboard_input_d),
        .controller_buttons(controller_input_d),
        .mouse_valid(mouse_valid_input_d), .mouse_dx(mouse_dx_input_d), .mouse_dy(mouse_dy_input_d),
        .mouse_buttons(mouse_buttons_input_d), .mouse_ready(mouse_ready),
        .media_write_addr(media_addr_input_d), .media_write_data(media_data_input_d),
        .media_write_enable(media_enable_input_d), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_input_d),
        .media_read_req(media_read_req_input_d), .media_read_addr(media_read_addr_input_d),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_input_d), .unit0_size(unit0_size_input_d)
    );

    fes_computer_mailbox #(
        .ENABLE_APPLE2_FLOPPY(1), .UNIT0_MIN(1), .UNIT0_MAX(1030)
    ) media_shared (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_media),
        .exec_reset(exec_reset_media), .keyboard_rows(keyboard_media),
        .controller_buttons(controller_media),
        .mouse_valid(mouse_valid_media), .mouse_dx(mouse_dx_media), .mouse_dy(mouse_dy_media),
        .mouse_buttons(mouse_buttons_media), .mouse_ready(mouse_ready),
        .media_write_addr(media_write_addr_media), .media_write_data(media_write_data_media),
        .media_write_enable(media_write_enable_media), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_media),
        .media_read_req(media_read_req_media), .media_read_addr(media_read_addr_media),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_media), .unit0_size(unit0_size_media)
    );
    fes_computer_mailbox_direct #(
        .ENABLE_APPLE2_FLOPPY(1), .UNIT0_MIN(1), .UNIT0_MAX(1030)
    ) media_direct (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_media_d),
        .exec_reset(exec_reset_media_d), .keyboard_rows(keyboard_media_d),
        .controller_buttons(controller_media_d),
        .mouse_valid(mouse_valid_media_d), .mouse_dx(mouse_dx_media_d), .mouse_dy(mouse_dy_media_d),
        .mouse_buttons(mouse_buttons_media_d), .mouse_ready(mouse_ready),
        .media_write_addr(media_addr_media_d), .media_write_data(media_data_media_d),
        .media_write_enable(media_enable_media_d), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_media_d),
        .media_read_req(media_read_req_media_d), .media_read_addr(media_read_addr_media_d),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_media_d), .unit0_size(unit0_size_media_d)
    );

    fes_computer_mailbox #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1), .ENABLE_MOUSE(1),
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_WRITE(1),
        .ENABLE_ATARI_ST_FLOPPY_GEOMETRY(1), .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(20),
        .UNIT0_MIN(368640), .UNIT0_MAX(839680)
    ) st_shared (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_st),
        .exec_reset(exec_reset_st), .keyboard_rows(keyboard_rows_st),
        .controller_buttons(controller_st),
        .mouse_valid(mouse_valid_st), .mouse_dx(mouse_dx_st), .mouse_dy(mouse_dy_st),
        .mouse_buttons(mouse_buttons_st), .mouse_ready(mouse_ready),
        .media_write_addr(media_write_addr_st), .media_write_data(media_write_data_st),
        .media_write_enable(media_write_enable_st), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_st),
        .media_read_req(media_read_req_st), .media_read_addr(media_read_addr_st),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_st), .unit0_size(unit0_size_st)
    );
    fes_computer_mailbox_direct #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1), .ENABLE_MOUSE(1),
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_WRITE(1),
        .ENABLE_ATARI_ST_FLOPPY_GEOMETRY(1), .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(20),
        .UNIT0_MIN(368640), .UNIT0_MAX(839680)
    ) st_direct (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_st_d),
        .exec_reset(exec_reset_st_d), .keyboard_rows(keyboard_st_d),
        .controller_buttons(controller_st_d),
        .mouse_valid(mouse_valid_st_d), .mouse_dx(mouse_dx_st_d), .mouse_dy(mouse_dy_st_d),
        .mouse_buttons(mouse_buttons_st_d), .mouse_ready(mouse_ready),
        .media_write_addr(media_addr_st_d), .media_write_data(media_data_st_d),
        .media_write_enable(media_enable_st_d), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_st_d),
        .media_read_req(media_read_req_st_d), .media_read_addr(media_read_addr_st_d),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_st_d), .unit0_size(unit0_size_st_d)
    );

    // Small writable disk so a completed snapshot can be compared without
    // uploading a geometry-sized image. The shell-sized st_ pair above still
    // covers the geometry the Atari ST core elaborates.
    wire [31:0] gpi_snap_d;
    wire exec_reset_snap, exec_reset_snap_d;
    wire [143:0] keyboard_snap, keyboard_snap_d;
    wire [15:0] controller_snap, controller_snap_d;
    wire [1:0] unit0_state_snap_d;
    wire [31:0] unit0_size_snap_d;
    wire [19:0] media_addr_snap_d;
    wire [15:0] media_data_snap_d;
    wire [1:0] media_enable_snap_d;
    wire mouse_valid_snap, mouse_valid_snap_d;
    wire signed [15:0] mouse_dx_snap, mouse_dx_snap_d, mouse_dy_snap, mouse_dy_snap_d;
    wire [1:0] mouse_buttons_snap, mouse_buttons_snap_d;
    wire media_frozen_snap_d;
    wire media_read_req_snap_d;
    wire [19:0] media_read_addr_snap_d;
    fes_computer_mailbox #(
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_WRITE(1),
        .MEDIA_AW(20), .UNIT0_MIN(4), .UNIT0_MAX(16)
    ) snap_shared (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_snap),
        .exec_reset(exec_reset_snap), .keyboard_rows(keyboard_snap),
        .controller_buttons(controller_snap),
        .mouse_valid(mouse_valid_snap), .mouse_dx(mouse_dx_snap), .mouse_dy(mouse_dy_snap),
        .mouse_buttons(mouse_buttons_snap), .mouse_ready(mouse_ready),
        .media_write_addr(media_write_addr_snap), .media_write_data(media_write_data_snap),
        .media_write_enable(media_write_enable_snap), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_snap),
        .media_read_req(media_read_req_snap), .media_read_addr(media_read_addr_snap),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_snap), .unit0_size(unit0_size_snap)
    );
    fes_computer_mailbox_direct #(
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_WRITE(1),
        .MEDIA_AW(20), .UNIT0_MIN(4), .UNIT0_MAX(16)
    ) snap_direct (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi_snap_d),
        .exec_reset(exec_reset_snap_d), .keyboard_rows(keyboard_snap_d),
        .controller_buttons(controller_snap_d),
        .mouse_valid(mouse_valid_snap_d), .mouse_dx(mouse_dx_snap_d), .mouse_dy(mouse_dy_snap_d),
        .mouse_buttons(mouse_buttons_snap_d), .mouse_ready(mouse_ready),
        .media_write_addr(media_addr_snap_d), .media_write_data(media_data_snap_d),
        .media_write_enable(media_enable_snap_d), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen_snap_d),
        .media_read_req(media_read_req_snap_d), .media_read_addr(media_read_addr_snap_d),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state_snap_d), .unit0_size(unit0_size_snap_d)
    );

    wire input_diff = gpi_input !== gpi_input_d || exec_reset_input !== exec_reset_input_d
        || keyboard_rows_input !== keyboard_input_d || controller_input !== controller_input_d
        || unit0_state_input !== unit0_state_input_d || unit0_size_input !== unit0_size_input_d
        || media_addr_input !== media_addr_input_d || media_data_input !== media_data_input_d
        || media_enable_input !== media_enable_input_d
        || mouse_valid_input !== mouse_valid_input_d || mouse_dx_input !== mouse_dx_input_d
        || mouse_dy_input !== mouse_dy_input_d || mouse_buttons_input !== mouse_buttons_input_d
        || media_frozen_input !== media_frozen_input_d
        || media_read_req_input !== media_read_req_input_d
        || media_read_addr_input !== media_read_addr_input_d;
    wire media_diff = gpi_media !== gpi_media_d || exec_reset_media !== exec_reset_media_d
        || keyboard_media !== keyboard_media_d || controller_media !== controller_media_d
        || unit0_state_media !== unit0_state_media_d || unit0_size_media !== unit0_size_media_d
        || media_write_addr_media !== media_addr_media_d
        || media_write_data_media !== media_data_media_d
        || media_write_enable_media !== media_enable_media_d
        || mouse_valid_media !== mouse_valid_media_d || mouse_dx_media !== mouse_dx_media_d
        || mouse_dy_media !== mouse_dy_media_d || mouse_buttons_media !== mouse_buttons_media_d
        || media_frozen_media !== media_frozen_media_d
        || media_read_req_media !== media_read_req_media_d
        || media_read_addr_media !== media_read_addr_media_d;
    wire st_diff = gpi_st !== gpi_st_d || exec_reset_st !== exec_reset_st_d
        || keyboard_rows_st !== keyboard_st_d || controller_st !== controller_st_d
        || unit0_state_st !== unit0_state_st_d || unit0_size_st !== unit0_size_st_d
        || media_write_addr_st !== media_addr_st_d || media_write_data_st !== media_data_st_d
        || media_write_enable_st !== media_enable_st_d
        || mouse_valid_st !== mouse_valid_st_d || mouse_dx_st !== mouse_dx_st_d
        || mouse_dy_st !== mouse_dy_st_d || mouse_buttons_st !== mouse_buttons_st_d
        || media_frozen_st !== media_frozen_st_d
        || media_read_req_st !== media_read_req_st_d
        || media_read_addr_st !== media_read_addr_st_d;
    wire snap_diff = gpi_snap !== gpi_snap_d || exec_reset_snap !== exec_reset_snap_d
        || keyboard_snap !== keyboard_snap_d || controller_snap !== controller_snap_d
        || unit0_state_snap !== unit0_state_snap_d || unit0_size_snap !== unit0_size_snap_d
        || media_write_addr_snap !== media_addr_snap_d
        || media_write_data_snap !== media_data_snap_d
        || media_write_enable_snap !== media_enable_snap_d
        || mouse_valid_snap !== mouse_valid_snap_d || mouse_dx_snap !== mouse_dx_snap_d
        || mouse_dy_snap !== mouse_dy_snap_d || mouse_buttons_snap !== mouse_buttons_snap_d
        || media_frozen_snap !== media_frozen_snap_d
        || media_read_req_snap !== media_read_req_snap_d
        || media_read_addr_snap !== media_read_addr_snap_d;
    assign diverge = {snap_diff, st_diff, media_diff, input_diff};
endmodule
