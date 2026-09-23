// SPDX-License-Identifier: GPL-2.0-or-later
// RAM tester utility. The host speaks fes.application 1.0. The picture is
// fixed 720p. Memory traffic is local to the core; the ABI has no memory opcode.
module top #(
    parameter [127:0] BUILD_ID = 128'h00000000000000000000000000000000
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
    wire [7:0] play_red, play_green, play_blue;
    wire [7:0] video_red, video_green, video_blue;
    wire [9:0] playfield_x, playfield_y;
    wire playfield_active;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    fes_application_gp endpoint (
        .clk(pixel_clk),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .exec_reset(mailbox_reset),
        .buttons(), .controller_buttons(), .controller_keypad(),
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
    wire [15:0] sdram_addr, sdram_wdata, sdram_rdata;
    wire [15:0] hps_addr, hps_wdata, hps_rdata;
    wire sdram_busy;
    wire sdram_pass /* verilator public_flat_rd */;
    wire sdram_fail /* verilator public_flat_rd */;
    wire hps_busy;
    wire hps_pass /* verilator public_flat_rd */;
    wire hps_fail /* verilator public_flat_rd */;
    wire [15:0] sdram_result_addr, hps_result_addr;
    wire [15:0] dq_out, dq_in;
    wire dq_oe;
    reg [1:0] reset_sync = 2'b00;

    always @(posedge FPGA_CLK1_50)
        reset_sync <= {reset_sync[0], mailbox_reset};

    mem_channel sdram_test (
        .clk(FPGA_CLK1_50), .reset(reset_sync[1]),
        .start(sdram_start), .write(sdram_write), .addr(sdram_addr), .wdata(sdram_wdata),
        .done(sdram_done), .rdata(sdram_rdata),
        .busy(sdram_busy), .pass(sdram_pass), .fail(sdram_fail), .result_addr(sdram_result_addr)
    );
    mem_channel hps_test (
        .clk(FPGA_CLK1_50), .reset(reset_sync[1]),
        .start(hps_start), .write(hps_write), .addr(hps_addr), .wdata(hps_wdata),
        .done(hps_done), .rdata(hps_rdata),
        .busy(hps_busy), .pass(hps_pass), .fail(hps_fail), .result_addr(hps_result_addr)
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
        .sdram_pass(sdram_pass), .sdram_fail(sdram_fail),
        .hps_pass(hps_pass), .hps_fail(hps_fail),
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

    /* verilator lint_off UNUSEDSIGNAL */
    wire unused_status = |{sdram_busy, hps_busy, sdram_result_addr, hps_result_addr};
    /* verilator lint_on UNUSEDSIGNAL */
endmodule
