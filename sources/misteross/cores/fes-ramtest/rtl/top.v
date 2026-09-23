// SPDX-License-Identifier: GPL-2.0-or-later
// RAM tester utility. The host speaks fes.application 1.0. The picture is
// fixed 720p. Memory traffic is local to the core; the ABI has no memory opcode.
module top #(
    parameter [127:0] BUILD_ID = 128'h00000000000000000000000000000000,
    // Simulation uses a short span of the same patterns. The sealed core
    // keeps the full SDRAM addon and the HPS window.
    parameter [31:0] SDRAM_WORDS = `ifdef SIM 32'd2176 `else 32'h04000000 `endif,
    parameter [31:0] HPS_WORDS = `ifdef SIM 32'd64 `else 32'h00040000 `endif,
    parameter [31:0] HPS_BASE = `ifdef SIM 32'd0 `else 32'h01000000 `endif
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA,
    output wire        SDRAM_CLK,
    output wire        SDRAM_CKE,
    output wire        SDRAM_nCS,
    output wire        SDRAM_nRAS,
    output wire        SDRAM_nCAS,
    output wire        SDRAM_nWE,
    output wire        SDRAM_DQML,
    output wire        SDRAM_DQMH,
    output wire [1:0]  SDRAM_BA,
    output wire [12:0] SDRAM_A,
    inout  wire [15:0] SDRAM_DQ
);
    wire hdmi_scl_in;
    wire hdmi_sda_in;
    wire hdmi_scl_low;
    wire hdmi_sda_low;

    MISTRAL_IO hdmi_scl_pad (
        .I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL)
    );
    MISTRAL_IO hdmi_sda_pad (
        .I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA)
    );
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire mailbox_reset;
    wire pixel_clk;
    wire [7:0] app_buttons;
    wire [7:0] play_red, play_green, play_blue;
    wire [7:0] video_red, video_green, video_blue;
    wire [9:0] playfield_x, playfield_y;
    wire playfield_active;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    fes_application_gp #(.ENABLE_GAMEPAD(1)) endpoint (
        .clk(pixel_clk),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .exec_reset(mailbox_reset),
        .buttons(app_buttons), .controller_buttons(), .controller_keypad(),
        .media_ready(), .media_size(),
        .media_byte0(), .media_byte1(), .media_byte2(),
        .media_write_addr(), .media_write_data(), .media_write_enable(),
        .firmware_write_addr(), .firmware_write_data(), .firmware_write_enable(),
        .firmware_ready()
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk)
    );

    wire sdram_start, sdram_write, sdram_done;
    wire hps_start, hps_write, hps_done;
    wire [25:0] sdram_addr;
    wire [31:0] hps_addr;
    wire [15:0] sdram_wdata, sdram_rdata;
    wire [15:0] hps_wdata, hps_rdata;
    wire sdram_pass /* verilator public_flat_rd */;
    wire sdram_fail /* verilator public_flat_rd */;
    wire hps_pass /* verilator public_flat_rd */;
    wire hps_fail /* verilator public_flat_rd */;
    wire sdram_stopped /* verilator public_flat_rd */;
    wire hps_stopped /* verilator public_flat_rd */;
    wire [31:0] sdram_errors /* verilator public_flat_rd */;
    wire [31:0] hps_errors /* verilator public_flat_rd */;
    wire [2:0] sdram_phase, hps_phase;
    wire sdram_reading, hps_reading;
    wire [31:0] sdram_shown, hps_shown;
    wire [31:0] sdram_fault, hps_fault;
    wire [31:0] sdram_last, hps_last;
    wire [15:0] sdram_was, hps_was;
    wire [15:0] sdram_expect, hps_expect, sdram_got, hps_got;
    wire [15:0] dq_out, dq_in;
    wire dq_oe;
    reg [1:0] reset_sync = 2'b00;
    reg [1:0] button_sync = 2'b00;

    always @(posedge FPGA_CLK1_50) begin
        reset_sync <= {reset_sync[0], mailbox_reset};
        button_sync <= {button_sync[0], |app_buttons};
    end

    mem_channel #(.ADDR_W(26), .WORDS(SDRAM_WORDS), .BASE(32'd0)) sdram_test (
        .clk(FPGA_CLK1_50), .reset(reset_sync[1]), .stop(button_sync[1]),
        .start(sdram_start), .write(sdram_write), .addr(sdram_addr), .wdata(sdram_wdata),
        .done(sdram_done), .rdata(sdram_rdata),
        .busy(), .pass(sdram_pass), .fail(sdram_fail), .stopped(sdram_stopped),
        .phase(sdram_phase), .reading(sdram_reading), .shown_addr(sdram_shown),
        .fault_addr(sdram_fault), .last_addr(sdram_last), .fault_got(sdram_was),
        .errors(sdram_errors), .shown_expect(sdram_expect), .shown_got(sdram_got)
    );
    mem_channel #(.ADDR_W(32), .WORDS(HPS_WORDS), .BASE(HPS_BASE)) hps_test (
        .clk(FPGA_CLK1_50), .reset(reset_sync[1]), .stop(button_sync[1]),
        .start(hps_start), .write(hps_write), .addr(hps_addr), .wdata(hps_wdata),
        .done(hps_done), .rdata(hps_rdata),
        .busy(), .pass(hps_pass), .fail(hps_fail), .stopped(hps_stopped),
        .phase(hps_phase), .reading(hps_reading), .shown_addr(hps_shown),
        .fault_addr(hps_fault), .last_addr(hps_last), .fault_got(hps_was),
        .errors(hps_errors), .shown_expect(hps_expect), .shown_got(hps_got)
    );

    sdram_addon_port sdram (
        .clk(FPGA_CLK1_50),
        .start(sdram_start), .write(sdram_write), .addr(sdram_addr), .wdata(sdram_wdata),
        .done(sdram_done), .rdata(sdram_rdata),
        .sdram_clk(SDRAM_CLK), .sdram_cke(SDRAM_CKE),
        .sdram_ncs(SDRAM_nCS), .sdram_nras(SDRAM_nRAS),
        .sdram_ncas(SDRAM_nCAS), .sdram_nwe(SDRAM_nWE),
        .sdram_dqml(SDRAM_DQML), .sdram_dqmh(SDRAM_DQMH),
        .sdram_ba(SDRAM_BA), .sdram_a(SDRAM_A),
        .dq_out(dq_out), .dq_oe(dq_oe), .dq_in(dq_in)
    );

    genvar dq_bit;
    generate
        for (dq_bit = 0; dq_bit < 16; dq_bit = dq_bit + 1) begin : dq_buf
            altiobuf_bidir #(
                .number_of_channels(1),
                .enable_bus_hold("OFF")
            ) pad (
                .dataio(SDRAM_DQ[dq_bit]),
                .oe(dq_oe),
                .datain(dq_out[dq_bit]),
                .dataout(dq_in[dq_bit])
            );
        end
    endgenerate

    hps_ddr_port hps_ddr (
        .clk(FPGA_CLK1_50),
        .start(hps_start), .write(hps_write), .addr(hps_addr), .wdata(hps_wdata),
        .done(hps_done), .rdata(hps_rdata)
    );

    ram_display display (
        .pixel_clk(pixel_clk),
        .x(playfield_x), .y(playfield_y), .active(playfield_active),
        .sdram_phase(sdram_phase), .sdram_reading(sdram_reading), .sdram_addr(sdram_shown),
        .sdram_fault(sdram_fault), .sdram_last(sdram_last), .sdram_was(sdram_was),
        .sdram_errors(sdram_errors), .sdram_expect(sdram_expect), .sdram_got(sdram_got),
        .sdram_pass(sdram_pass), .sdram_fail(sdram_fail), .sdram_stopped(sdram_stopped),
        .hps_phase(hps_phase), .hps_reading(hps_reading), .hps_addr(hps_shown),
        .hps_fault(hps_fault), .hps_last(hps_last), .hps_was(hps_was),
        .hps_errors(hps_errors), .hps_expect(hps_expect), .hps_got(hps_got),
        .hps_pass(hps_pass), .hps_fail(hps_fail), .hps_stopped(hps_stopped),
        .red(play_red), .green(play_green), .blue(play_blue)
    );

    fes_video_720p video (
        .pixel_clk(pixel_clk),
        .game_red(play_red), .game_green(play_green), .game_blue(play_blue),
        .playfield_x(playfield_x), .playfield_y(playfield_y),
        .playfield_active(playfield_active),
        .red(video_red), .green(video_green), .blue(video_blue),
        .de(HDMI_TX_DE), .hsync(HDMI_TX_HS), .vsync(HDMI_TX_VS),
        .frame_tick()
    );

    assign HDMI_TX_CLK = pixel_clk;
    assign HDMI_TX_D = {video_red, video_green, video_blue};
endmodule
