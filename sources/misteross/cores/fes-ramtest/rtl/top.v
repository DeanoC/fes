// SPDX-License-Identifier: GPL-2.0-or-later
// RAM tester utility. The host speaks fes.application 1.0. The picture is
// fixed 720p. Memory traffic is local to the core; the ABI has no memory opcode.
module top #(
    // The Quartus comparison stamps the same id the package manifest carries,
    // so the kit identity probe accepts the bitstream. The OSS seal overrides
    // this default from the build record.
    parameter [127:0] BUILD_ID = `ifdef QUARTUS `RAMTEST_BUILD_ID `else 128'h00000000000000000000000000000000 `endif,
    // Simulation uses a short span of the same patterns. The sealed core
    // keeps the full SDRAM addon and the HPS window.
    // Simulation walks past 64K halfwords so the address pattern's high half
    // is nonzero. 2176 only crossed a row, and the high XOR stayed zero.
    parameter [31:0] SDRAM_WORDS = `ifdef SIM 32'd65552 `else 32'h04000000 `endif,
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
    wire hdmi_scl_low;
    wire hdmi_sda_low;

    // Quartus drives the open-drain I2C pins from the hard block. The OSS
    // lane keeps explicit pads and the fixed Cyclone V I2C site.
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
    wire [15:0] dq_out, dq_rise, dq_fall;
`ifndef RAM_100_ONLY
    reg [15:0] dq_rise_q, dq_fall_q;
`endif
    wire dq_oe;
    reg [1:0] hps_reset_sync = 2'b00;
    reg [1:0] hps_stop_sync = 2'b00;
    reg [1:0] stop_sync = 2'b00;
    reg [1:0] mem_reset_sync = 2'b00;
    // The OSS placer accepts one PLL output per clock buffer and has no clock
    // mux, so the sealed bitstream stays on the 50 MHz pin. Quartus builds
    // select either 100 or 130 MHz for a full-span hardware diagnostic.
`ifdef RAM_RATE_SWEEP
    wire clk130, clk100, clk_cap, ram_locked, mem_clk, cap_clk;
    ram_pll ram_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(clk130),
        .outclk_1(clk100),
        .outclk_2(clk_cap),
        .locked(ram_locked)
    );
`ifdef RAM_130_ONLY
    // The 130 MHz diagnostic uses direct PLL outputs. Routing both through
    // the sweep's clock muxes exceeds the board's available clock regions.
    assign mem_clk = clk130;
    assign cap_clk = clk_cap;
    wire sdram_pin_clk = mem_clk;
    wire [1:0] rate = 2'd1;
    wire rate_reset = ~ram_locked;
    wire stop_level = |app_buttons;
    wire [7:0] sdram_mhz = 8'd130;
`elsif RAM_100_ONLY
    assign mem_clk = clk100;
    assign cap_clk = clk_cap;
    wire sdram_pin_clk = mem_clk;
    wire [1:0] rate = 2'd2;
    wire rate_reset = ~ram_locked;
    wire stop_level = |app_buttons;
    wire [7:0] sdram_mhz = 8'd100;
`else
    reg [1:0] rate = 2'd1;
    reg [15:0] rate_hold = 16'd0;
    reg [1:0] up_sync = 2'b00;
    reg [1:0] down_sync = 2'b00;
    // Eight seconds on the 50 MHz reference, so a capture can read the count
    // before the next rate re-inits the chip.
    reg [28:0] dwell = 29'd0;
    reg armed = 1'b1;
    wire stop_level = |app_buttons[7:2];
    wire scan_done = sdram_pass | sdram_fail;
    wire [7:0] sdram_mhz = rate == 2'd0 ? 8'd50 : rate == 2'd1 ? 8'd130 : 8'd100;
    // Clock-select inputs 0 and 1 are pin clocks. PLL clocks belong on 2 and 3.
    wire [1:0] clkselect = rate == 2'd0 ? 2'b00 : rate == 2'd1 ? 2'b10 : 2'b11;
    always @(posedge FPGA_CLK1_50) begin
        up_sync <= {up_sync[0], app_buttons[0]};
        down_sync <= {down_sync[0], app_buttons[1]};
        if (up_sync == 2'b01 && rate != 2'd2) begin
            rate <= rate + 2'd1;
            rate_hold <= 16'hFFFF;
            dwell <= 29'd0;
            armed <= 1'b0;
        end else if (down_sync == 2'b01 && rate != 2'd0) begin
            rate <= rate - 2'd1;
            rate_hold <= 16'hFFFF;
            dwell <= 29'd0;
            armed <= 1'b0;
        end else if (rate_hold != 16'd0) begin
            rate_hold <= rate_hold - 16'd1;
        end else if (!scan_done) begin
            armed <= 1'b1;
            dwell <= 29'd0;
        end else if (armed && rate != 2'd2 && !stop_level && (rate != 2'd0 || sdram_pass)) begin
            if (dwell == 29'd400000000) begin
                rate <= rate + 2'd1;
                rate_hold <= 16'hFFFF;
                dwell <= 29'd0;
                armed <= 1'b0;
            end else begin
                dwell <= dwell + 29'd1;
            end
        end
    end
    altclkctrl #(
        .clock_type("Global Clock"),
        .number_of_clocks(4),
        .width_clkselect(2),
        .ena_register_mode("falling edge"),
        .intended_device_family("Cyclone V"),
        .use_glitch_free_switch_over_implementation("ON")
    ) mem_clock (
        .inclk({clk100, clk130, 1'b0, FPGA_CLK1_50}),
        .clkselect(clkselect),
        .ena(1'b1),
        .outclk(mem_clk)
    );
    // The controller and the SDRAM pin share one phase-0 clock. At 100 MHz
    // the read capture clock leads it by one VCO step.
    wire sdram_pin_clk = mem_clk;
    wire [1:0] capselect = rate == 2'd0 ? 2'b00 : rate == 2'd1 ? 2'b11 : 2'b10;
    altclkctrl #(
        .clock_type("Global Clock"),
        .number_of_clocks(4),
        .width_clkselect(2),
        .ena_register_mode("falling edge"),
        .intended_device_family("Cyclone V"),
        .use_glitch_free_switch_over_implementation("ON")
    ) cap_clock (
        .inclk({clk_cap, clk100, 1'b0, FPGA_CLK1_50}),
        .clkselect(capselect),
        .ena(1'b1),
        .outclk(cap_clk)
    );
    wire rate_reset = ~ram_locked | (rate_hold != 16'd0);
`endif
`else
    wire mem_clk = FPGA_CLK1_50;
    wire sdram_pin_clk = FPGA_CLK1_50;
    wire cap_clk = FPGA_CLK1_50;
    wire [1:0] rate = 2'd0;
    wire rate_reset = 1'b0;
    wire stop_level = |app_buttons;
    wire [7:0] sdram_mhz = 8'd50;
`endif

    always @(posedge FPGA_CLK1_50) begin
        hps_reset_sync <= {hps_reset_sync[0], mailbox_reset};
        hps_stop_sync <= {hps_stop_sync[0], stop_level};
    end

    // Per-pattern counts stay on screen after the next rate re-inits the chip.
    wire [191:0] sdram_patterns;
    reg [191:0] pat50 = 192'd0;
    reg [191:0] pat75 = 192'd0;
    reg [191:0] pat100 = 192'd0;
    reg [2:0] pat_ok = 3'd0;
    reg pat_seen = 1'b0;
    always @(posedge mem_clk) begin
        stop_sync <= {stop_sync[0], stop_level};
        mem_reset_sync <= {mem_reset_sync[0], mailbox_reset | rate_reset};
        if (mem_reset_sync[1])
            pat_seen <= 1'b0;
        else if ((sdram_pass || sdram_fail) && !pat_seen) begin
            pat_seen <= 1'b1;
            case (rate)
                2'd0: begin
                    pat50 <= sdram_patterns;
                    pat_ok[0] <= 1'b1;
                end
                2'd1: begin
                    pat75 <= sdram_patterns;
                    pat_ok[1] <= 1'b1;
                end
                default: begin
                    pat100 <= sdram_patterns;
                    pat_ok[2] <= 1'b1;
                end
            endcase
        end
    end

    mem_channel #(.ADDR_W(26), .WORDS(SDRAM_WORDS), .BASE(32'd0)) sdram_test (
        .clk(mem_clk), .reset(mem_reset_sync[1]), .stop(stop_sync[1]),
        .start(sdram_start), .write(sdram_write), .addr(sdram_addr), .wdata(sdram_wdata),
        .done(sdram_done), .rdata(sdram_rdata),
        .busy(), .pass(sdram_pass), .fail(sdram_fail), .stopped(sdram_stopped),
        .phase(sdram_phase), .reading(sdram_reading), .shown_addr(sdram_shown),
        .fault_addr(sdram_fault), .last_addr(sdram_last), .fault_got(sdram_was),
        .errors(sdram_errors), .pattern_errors(sdram_patterns),
        .shown_expect(sdram_expect), .shown_got(sdram_got)
    );
    mem_channel #(.ADDR_W(32), .WORDS(HPS_WORDS), .BASE(HPS_BASE)) hps_test (
        .clk(FPGA_CLK1_50), .reset(hps_reset_sync[1]), .stop(hps_stop_sync[1]),
        .start(hps_start), .write(hps_write), .addr(hps_addr), .wdata(hps_wdata),
        .done(hps_done), .rdata(hps_rdata),
        .busy(), .pass(hps_pass), .fail(hps_fail), .stopped(hps_stopped),
        .phase(hps_phase), .reading(hps_reading), .shown_addr(hps_shown),
        .fault_addr(hps_fault), .last_addr(hps_last), .fault_got(hps_was),
        .errors(hps_errors), .pattern_errors(),
        .shown_expect(hps_expect), .shown_got(hps_got)
    );

    sdram_addon_port sdram (
        .clk(mem_clk),
        .clk_pin(sdram_pin_clk),
        .rate(rate),
        .reset(mem_reset_sync[1]),
        .start(sdram_start), .write(sdram_write), .addr(sdram_addr), .wdata(sdram_wdata),
        .done(sdram_done), .rdata(sdram_rdata),
        .sdram_clk(SDRAM_CLK), .sdram_cke(SDRAM_CKE),
        .sdram_ncs(SDRAM_nCS), .sdram_nras(SDRAM_nRAS),
        .sdram_ncas(SDRAM_nCAS), .sdram_nwe(SDRAM_nWE),
        .sdram_dqml(SDRAM_DQML), .sdram_dqmh(SDRAM_DQMH),
        .sdram_ba(SDRAM_BA), .sdram_a(SDRAM_A),
        .dq_out(dq_out), .dq_oe(dq_oe),
`ifndef RAM_100_ONLY
        .dq_rise(dq_rise_q), .dq_fall(dq_fall_q)
`else
        .dq_rise(dq_rise), .dq_fall(dq_fall)
`endif
    );

`ifndef RAM_100_ONLY
    // A fabric register next to the input DDIO cells removes the long
    // 130 MHz route from the pins through rate selection into rdata.
    always @(posedge mem_clk) begin
        dq_rise_q <= dq_rise;
        dq_fall_q <= dq_fall;
    end
`endif

    genvar dq_bit;
    generate
        for (dq_bit = 0; dq_bit < 16; dq_bit = dq_bit + 1) begin : dq_buf
            wire dq_pin;
            altiobuf_bidir #(
                .number_of_channels(1),
`ifdef QUARTUS
                .enable_bus_hold("FALSE")
`else
                .enable_bus_hold("OFF")
`endif
            ) pad (
                .dataio(SDRAM_DQ[dq_bit]),
                .oe(dq_oe),
                .datain(dq_out[dq_bit]),
                .dataout(dq_pin)
            );
            // Both edges are taken in the IO cell, on the capture clock.
            altddio_in #(
                .width(1),
                .intended_device_family("Cyclone V"),
                .power_up_high("OFF"),
                .invert_input_clocks("OFF")
            ) dq_capture (
                .datain(dq_pin),
                .inclock(cap_clk),
                .inclocken(1'b1),
                .aset(1'b0),
                .aclr(1'b0),
                .sset(1'b0),
                .sclr(1'b0),
                .dataout_h(dq_rise[dq_bit]),
                .dataout_l(dq_fall[dq_bit])
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
        .sdram_mhz(sdram_mhz),
        .sdram_pass(sdram_pass), .sdram_fail(sdram_fail), .sdram_stopped(sdram_stopped),
        .pat50(pat50), .pat75(pat75), .pat100(pat100), .pat_ok(pat_ok),
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
