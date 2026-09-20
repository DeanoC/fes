// SPDX-License-Identifier: GPL-2.0-or-later
// Exercise the production mailbox-to-machine firmware wiring, including RAM.
module firmware_model (
    input wire clk, input wire [31:0] gpo, output wire [31:0] gpi,
    input wire [15:0] peek_addr, output wire [7:0] peek_data,
    output wire exec_reset, output wire cpu_halt_n
);
    wire [15:0] buttons, media_size, firmware_data;
    wire [23:0] keypad;
    wire media_ready;
    wire [14:0] media_addr;
    wire [7:0] media_q;
    wire [12:0] firmware_addr;
    wire [1:0] firmware_we;
    coleco_application_gp #(.ENABLE_FIRMWARE(1)) endpoint (
        .clk(clk), .gpo(gpo), .gpi(gpi), .build_id(128'd0),
        .exec_reset(exec_reset), .controller_buttons(buttons),
        .controller_keypad(keypad), .media_ready(media_ready),
        .media_size(media_size), .media_addr(media_addr), .media_q(media_q),
        .firmware_write_addr(firmware_addr), .firmware_write_data(firmware_data),
        .firmware_write_enable(firmware_we)
    );
    coleco_machine machine (
        .clk_sys(clk), .reset(exec_reset), .controller_buttons(buttons),
        .controller_keypad(keypad), .media_ready(media_ready),
        .media_size(media_size), .media_data(media_q), .media_addr(media_addr),
        .peek_addr(peek_addr), .peek_data(peek_data),
        .controller1_value(), .controller2_value(), .logical_x(), .logical_y(),
        .logical_pixel(), .logical_blank(), .vdp_status(), .cpu_addr_debug(),
        .cpu_halt_n(cpu_halt_n), .firmware_we_a(firmware_we[0]),
        .firmware_we_b(firmware_we[1]), .firmware_addr(firmware_addr),
        .firmware_data(firmware_data)
    );
endmodule
