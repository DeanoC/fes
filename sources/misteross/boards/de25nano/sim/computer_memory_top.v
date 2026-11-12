// SPDX-License-Identifier: GPL-2.0-or-later
// Identity-only fixtures: no memory backend or physical adapter is constructed.
module computer_memory_top(input wire clk, input wire [31:0] gpo, output wire [31:0] basic, windowed, allocated, disabled, standard_allocated);
fes_computer_mailbox #(.ENABLE_KEYBOARD(1),.ENABLE_ZX81_TAPE(1)) basic_endpoint ( .clk(clk), .gpo(gpo), .build_id(128'd0), .exec_reset(), .keyboard_rows(), .controller_buttons(),
 .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
 .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_ready(1'b1),
 .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(), .media_read_req(), .media_read_addr(),
 .media_read_ready(1'b0), .media_read_data(8'd0), .unit0_state(), .unit0_size(), .gpi(basic));
fes_computer_mailbox #(.ENABLE_KEYBOARD(1),.ENABLE_ZX81_TAPE(1),.ENABLE_SESSION_DISPLAY(1)) windowed_endpoint ( .clk(clk), .gpo(gpo), .build_id(128'd0), .exec_reset(), .keyboard_rows(), .controller_buttons(),
 .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
 .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_ready(1'b1),
 .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(), .media_read_req(), .media_read_addr(),
 .media_read_ready(1'b0), .media_read_data(8'd0), .unit0_state(), .unit0_size(), .gpi(windowed));
fes_computer_mailbox #(.ENABLE_KEYBOARD(1),.ENABLE_ZX81_TAPE(1),.ENABLE_SESSION_DISPLAY(1),.ENABLE_HPS_DDR_WINDOW(0)) allocated_endpoint ( .clk(clk), .gpo(gpo), .build_id(128'd0), .exec_reset(), .keyboard_rows(), .controller_buttons(),
 .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
 .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_ready(1'b1),
 .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(), .media_read_req(), .media_read_addr(),
 .media_read_ready(1'b0), .media_read_data(8'd0), .unit0_state(), .unit0_size(), .gpi(allocated));
fes_computer_mailbox #(.ENABLE_KEYBOARD(1),.ENABLE_ZX81_TAPE(1),.ENABLE_HPS_DDR_WINDOW(0)) disabled_endpoint ( .clk(clk), .gpo(gpo), .build_id(128'd0), .exec_reset(), .keyboard_rows(), .controller_buttons(),
 .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
 .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_ready(1'b1),
 .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(), .media_read_req(), .media_read_addr(),
 .media_read_ready(1'b0), .media_read_data(8'd0), .unit0_state(), .unit0_size(), .gpi(disabled));
fes_zx81_core #(.HPS_DDR_WINDOW(0)) standard_core (
 .clk_sys(clk), .pixel_clk(clk), .reset_hold(1'b1), .gpo(gpo), .gpi(standard_allocated),
 .exec_reset(), .machine_video(), .audio_sample(), .display_request(), .display_opcode(),
 .display_index(), .display_argument(), .display_response_valid(1'b0), .display_response_error(1'b0),
 .display_response_data(16'd0), .display_quiesced(1'b1),
 .bus_addr(), .bus_wdata(), .bus_mreq_n(), .bus_iorq_n(), .bus_rd_n(), .bus_wr_n(), .bus_m1_n(),
 .bus_rfsh_n(), .bus_cpu_clock(), .bus_rdata(8'd0), .bus_peek_data(8'd0), .bus_dsel(1'b0),
 .bus_romcs(1'b0), .bus_wait(1'b0), .bus_ram_present(1'b0), .peek_addr(16'd0), .peek_data(),
 .cpu_addr(), .ram_write_enable(), .raster_h(), .raster_v()
);
endmodule
