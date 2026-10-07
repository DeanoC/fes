// SPDX-License-Identifier: GPL-3.0-or-later
module computer_interaction_sim_top (
 input wire clk, input wire [31:0] gpo, output wire [31:0] gpi,
 input wire allow_mouse, output wire exec_reset, output reg [31:0] mouse_accepts,
 input wire media_write_busy, media_changed, output wire media_frozen,
 output wire media_read_req, output wire [19:0] media_read_addr,
 input wire media_read_ready, input wire [7:0] media_read_data,
 output wire [19:0] media_write_addr, output wire [15:0] media_write_data,
 output wire [1:0] media_write_enable, output wire [1:0] unit0_state,
 output wire [31:0] unit0_size,
 input wire ac_req, ac_reg, ac_write, input wire [7:0] ac_wdata,
 output wire ac_ack, ac_irq, output wire [7:0] ac_rdata
);
 wire mouse_valid, mouse_ready, ik_mouse_ready;
 wire signed [15:0] dx,dy;
 wire [1:0] buttons;
 wire [143:0] keys;
 wire [15:0] controllers;
 wire tx_valid,tx_ready,rx_valid,rx_ready;
 wire [7:0] tx_data,rx_data;
 assign mouse_ready=allow_mouse&&ik_mouse_ready;
 initial mouse_accepts=0;
 always @(posedge clk) if(mouse_valid&&mouse_ready) mouse_accepts<=mouse_accepts+1;
 fes_computer_mailbox #(.ENABLE_MOUSE(1),.ENABLE_KEYBOARD(1),
 .ENABLE_ATARI_ST_FLOPPY(1),.ENABLE_ATARI_ST_FLOPPY_WRITE(1),
 .MEDIA_AW(20),.UNIT0_MIN(4),.UNIT0_MAX(737280)) endpoint (
 .clk(clk),.gpo(gpo),.gpi(gpi),.build_id(128'h00112233445566778899aabbccddeeff),
 .exec_reset(exec_reset),.keyboard_rows(keys),.controller_buttons(controllers),
 .mouse_valid(mouse_valid),.mouse_dx(dx),.mouse_dy(dy),.mouse_buttons(buttons),.mouse_ready(mouse_ready),
 .media_write_addr(media_write_addr),.media_write_data(media_write_data),.media_write_enable(media_write_enable),.media_write_ready(1'b1),
 .media_write_busy(media_write_busy),.media_changed(media_changed),.media_frozen(media_frozen),
 .media_read_req(media_read_req),.media_read_addr(media_read_addr),.media_read_ready(media_read_ready),.media_read_data(media_read_data),
 .unit0_state(unit0_state),.unit0_size(unit0_size));
 st_ikbd #(.SYSTEM_CLOCK_HZ(500000)) ikbd (.clk(clk),.reset(exec_reset),
 .command_valid(tx_valid),.command_data(tx_data),.command_ready(tx_ready),
 .response_valid(rx_valid),.response_data(rx_data),.response_ready(rx_ready),
 .keyboard(keys),.controller_buttons(controllers),.mouse_valid(mouse_valid&&allow_mouse),
 .mouse_dx(dx),.mouse_dy(dy),.mouse_buttons(buttons),.mouse_ready(ik_mouse_ready));
 st_acia #(.SYSTEM_CLOCK_HZ(500000)) acia (.clk(clk),.reset(exec_reset),
 .bus_req(ac_req),.bus_reg(ac_reg),.bus_write(ac_write),.bus_wdata(ac_wdata),
 .bus_rdata(ac_rdata),.bus_ack(ac_ack),.irq(ac_irq),
 .tx_valid(tx_valid),.tx_data(tx_data),.tx_ready(tx_ready),
 .rx_valid(rx_valid),.rx_data(rx_data),.rx_ready(rx_ready),.rx_frame_error(1'b0),.rx_parity_error(1'b0));
endmodule
