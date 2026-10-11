// SPDX-License-Identifier: GPL-3.0-or-later
module st_input_audio_sim_top (
    input wire clk, reset,
    input wire ac_req, ac_reg, ac_write,
    input wire [7:0] ac_wdata,
    output wire [7:0] ac_rdata,
    output wire ac_ack, ac_irq,
    input wire raw_mode, raw_rx_valid, raw_rx_frame, raw_rx_parity, raw_tx_ready,
    input wire [7:0] raw_rx_data,
    output wire raw_rx_ready, raw_tx_valid,
    output wire [7:0] raw_tx_data,
    input wire direct_ikbd, ik_command_valid, ik_response_ready,
    input wire [7:0] ik_command_data,
    output wire ik_command_ready, ik_response_valid,
    output wire [7:0] ik_response_data,
    input wire [143:0] keyboard,
    input wire [15:0] controller_buttons,
    input wire mouse_valid,
    input wire signed [15:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    output wire mouse_ready,
    input wire ym_req, ym_reg, ym_write,
    input wire [7:0] ym_wdata,
    output wire [7:0] ym_rdata,
    output wire ym_ack, sample_valid,
    output wire signed [15:0] pcm_signed,
    output wire [7:0] port_a,
    // Direct chip enables isolate Yamaha tone/envelope equations from the
    // wrapper's sample cadence, while exercising the same shared engine.
    input wire engine_ce, engine_address_write, engine_data_write,
    input wire [7:0] engine_data,
    output wire [7:0] engine_pcm, engine_read,
    output wire [14:0] engine_levels, sampled_levels, sampled_result_levels
);
    st_acia #(.SYSTEM_CLOCK_HZ(2_000_000)) acia (
        .clk(clk), .reset(reset), .bus_req(ac_req), .bus_reg(ac_reg),
        .bus_write(ac_write), .bus_wdata(ac_wdata), .bus_rdata(ac_rdata),
        .bus_ack(ac_ack), .irq(ac_irq),
        .tx_valid(raw_tx_valid), .tx_data(raw_tx_data),
        .tx_ready(raw_mode ? raw_tx_ready : ik_command_ready),
        .rx_valid(raw_mode ? raw_rx_valid : (!direct_ikbd && ik_response_valid)),
        .rx_data(raw_mode ? raw_rx_data : ik_response_data),
        .rx_frame_error(raw_mode && raw_rx_frame),
        .rx_parity_error(raw_mode && raw_rx_parity), .rx_ready(raw_rx_ready)
    );
    st_ikbd #(.SYSTEM_CLOCK_HZ(2_000_000)) ikbd (
        .clk(clk), .reset(reset),
        .command_valid(direct_ikbd ? ik_command_valid : (!raw_mode && raw_tx_valid)),
        .command_data(direct_ikbd ? ik_command_data : raw_tx_data),
        .command_ready(ik_command_ready), .response_valid(ik_response_valid),
        .response_data(ik_response_data),
        .response_ready(direct_ikbd ? ik_response_ready : (!raw_mode && raw_rx_ready)),
        .keyboard(keyboard), .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready)
    );
    // Scaled clocks retain 128 fabric edges per /8 counter tick, above the
    // exhaustive 42-edge mixer bound (production has about 208 edges).
    st_ym2149 #(.SYSTEM_CLOCK_HZ(2_000_000), .CHIP_CLOCK_HZ(125_000)) ym (
        .clk(clk), .reset(reset), .bus_req(ym_req), .bus_reg(ym_reg),
        .bus_write(ym_write), .bus_wdata(ym_wdata), .bus_rdata(ym_rdata),
        .bus_ack(ym_ack), .sample_valid(sample_valid), .pcm_signed(pcm_signed),
        .port_a(port_a)
    );
    assign sampled_levels = ym.ym_levels;
    assign sampled_result_levels = ym.mixer.result_levels;
    zonx_ay #(.YM2149(1)) engine (
        .clk(clk), .reset_n(!reset), .chip_ce(engine_ce),
        .address_write(engine_address_write), .data_write(engine_data_write),
        .data(engine_data), .pcm(engine_pcm), .ym_levels(engine_levels), .read_data(engine_read)
    );
endmodule
