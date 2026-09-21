// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_simple_computer.vh"
`ifndef FES_ZX81_BUILD_ID
`define FES_ZX81_BUILD_ID 128'h00000000000000000000000000000000
`endif

// Quartus DE10-Nano shell: 52 MHz ZX81 + 74.25 MHz HDMI, FES GP mailbox.
// BUILD_ID is overridden from the canonical build-input record.
module top #(
    parameter [127:0] BUILD_ID = `FES_ZX81_BUILD_ID,
    parameter EXPANSION_SOCKET = 0
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA,
    output wire        HDMI_I2S0,
    output wire        HDMI_MCLK,
    output wire        HDMI_LRCLK,
    output wire        HDMI_SCLK
);
    wire clk_sys;
    wire pixel_clk;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire exec_reset;
    wire [39:0] keyboard;
    wire tape_ready;
    wire [14:0] tape_size;
    wire [13:0] tape_addr;
    wire [7:0] tape_data;
    wire ce_6m5;
    wire video_pixel;
    wire hblank;
    wire vblank;
    wire [7:0] red;
    wire [7:0] green;
    wire [7:0] blue;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    wire hdmi_scl_low;
    wire hdmi_sda_low;

    // HPS I2C to ADV7513 at X52/Y60. Quartus uses assign-to-Z; OSS uses
    // MISTRAL_IO open-drain pads like FES Pong.
`ifdef QUARTUS
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .out_clk(hdmi_scl_low),
        .scl(HDMI_I2C_SCL),
        .out_data(hdmi_sda_low),
        .sda(HDMI_I2C_SDA)
    );
    assign HDMI_I2C_SCL = hdmi_scl_low ? 1'b0 : 1'bz;
    assign HDMI_I2C_SDA = hdmi_sda_low ? 1'b0 : 1'bz;
`else
    wire hdmi_scl_in;
    wire hdmi_sda_in;
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
`endif

    sys_pll system_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(clk_sys)
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk)
    );

    fes_computer_gp gp_mailbox (
        .clk(clk_sys),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .exec_reset(exec_reset),
        .keyboard(keyboard),
        .media_ready(tape_ready),
        .media_size(tape_size),
        .media_byte0(),
        .media_byte1(),
        .media_byte2(),
        .media_addr(tape_addr),
        .media_q(tape_data)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    wire [13:0] ram_address;
    wire [7:0] ram_write_data;
    wire ram_write_enable;
    wire [15:0] bus_addr;
    wire [7:0] bus_wdata, bus_rdata, bus_peek_data;
    wire bus_mreq_n, bus_iorq_n, bus_rd_n, bus_wr_n, bus_m1_n, bus_rfsh_n;
    wire bus_dsel, bus_romcs, bus_wait, bus_ram_present;
    (* keep *) wire [43:0] plug_addr;
    wire [19:0] plug_rdata;
    generate if (EXPANSION_SOCKET) begin : expansion
        zx81_expansion_socket socket (
            .clock(clk_sys),
            .cpu_addr(bus_addr),
            .cpu_wdata(bus_wdata),
            .cpu_mreq_n(bus_mreq_n),
            .cpu_iorq_n(bus_iorq_n),
            .cpu_rd_n(bus_rd_n),
            .cpu_wr_n(bus_wr_n),
            .cpu_m1_n(bus_m1_n),
            .cpu_rfsh_n(bus_rfsh_n),
            .peek_address(14'b0),
            .bus_rdata(bus_rdata),
            .bus_peek_data(bus_peek_data),
            .bus_dsel(bus_dsel),
            .bus_romcs(bus_romcs),
            .bus_wait(bus_wait),
            .bus_ram_present(bus_ram_present),
            .plug_addr(plug_addr),
            .plug_rdata_in(20'b0),
            .plug_rdata(plug_rdata)
        );
    end else begin : fixed_memory
        assign bus_rdata = 8'b0;
        assign bus_peek_data = 8'b0;
        assign bus_dsel = 1'b0;
        assign bus_romcs = 1'b0;
        assign bus_wait = 1'b0;
        assign bus_ram_present = 1'b0;
        assign plug_addr = 44'b0;
        assign plug_rdata = 20'b0;
    end endgenerate
    zx81_machine #(.EXTERNAL_RAM(EXPANSION_SOCKET)) machine (
        .clk_sys(clk_sys),
        .reset(exec_reset),
        .keyboard(keyboard),
        .tape_ready(tape_ready),
        .tape_size(tape_size),
        .tape_data(tape_data),
        .tape_addr_out(tape_addr),
        .ce_6m5(ce_6m5),
        .video_pixel(video_pixel),
        .hblank(hblank),
        .vblank(vblank),
        .hsync_out(),
        .vsync_out(),
        .halt_n(),
        .cpu_addr(),
        .peek_addr(16'h0000),
        .peek_data(),
        .ram_address(ram_address), .ram_write_data(ram_write_data),
        .ram_write_enable(ram_write_enable),
        .external_ram_data(8'b0), .external_peek_data(8'b0),
        .bus_addr(bus_addr), .bus_wdata(bus_wdata),
        .bus_mreq_n(bus_mreq_n), .bus_iorq_n(bus_iorq_n),
        .bus_rd_n(bus_rd_n), .bus_wr_n(bus_wr_n),
        .bus_m1_n(bus_m1_n), .bus_rfsh_n(bus_rfsh_n),
        .bus_rdata(bus_rdata), .bus_peek_data(bus_peek_data),
        .bus_dsel(bus_dsel), .bus_romcs(bus_romcs),
        .bus_wait(bus_wait), .bus_ram_present(bus_ram_present)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    wire signed [15:0] psg_sample = bus_ram_present ? 16'sd0
        : $signed({1'b0, bus_peek_data, 7'b0});
    zx81_hdmi_i2s hdmi_i2s (
        .pixel_clk(pixel_clk),
        .sample(psg_sample),
        .mclk(HDMI_MCLK),
        .sclk(HDMI_SCLK),
        .lrclk(HDMI_LRCLK),
        .i2s(HDMI_I2S0)
    );

    zx81_video_720p video (
        .clk_sys(clk_sys),
        .ce_6m5(ce_6m5),
        .zx_pixel(video_pixel),
        .hblank(hblank),
        .vblank(vblank),
        .pixel_clk(pixel_clk),
        .red(red),
        .green(green),
        .blue(blue),
        .de(HDMI_TX_DE),
        .hsync(HDMI_TX_HS),
        .vsync(HDMI_TX_VS),
        .frame_tick(),
        .src_x_max(),
        .src_y_max()
    );

    assign HDMI_TX_D = {red, green, blue};
    assign HDMI_TX_CLK = pixel_clk;
endmodule
