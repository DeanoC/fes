// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"

// RAM tester utility. The host speaks fes.application 1.0 with the gamepad and
// fes.memory.hps-ddr interfaces. The picture is fixed 720p. Memory traffic is
// local to the core; the ABI has no memory opcode.
module top #(
    // The Quartus comparison stamps the same id the package manifest carries,
    // so the kit identity probe accepts the bitstream. The OSS seal overrides
    // this default from the build record.
    parameter [127:0] BUILD_ID = `ifdef QUARTUS `RAMTEST_BUILD_ID `else 128'h00000000000000000000000000000000 `endif,
    // Simulation uses a short span of the same patterns. The sealed core
    // keeps the full SDRAM addon and the whole HPS DDR window.
    // Simulation walks past 64K halfwords so the address pattern's high half
    // is nonzero. 2176 only crossed a row, and the high XOR stayed zero.
    parameter [31:0] SDRAM_WORDS = `ifdef SIM 32'd65552 `else 32'h04000000 `endif,
    // Bytes each DDR port scans. Port 0 takes the first half of the window,
    // ports 1 and 2 a quarter each, so the three cover all of it.
    parameter [31:0] DDR_WIDE_BYTES = `ifdef SIM 32'd4096 `else `FES_APPLICATION_HPS_DDR_WINDOW_BYTES / 2 `endif,
    parameter [31:0] DDR_NARROW_BYTES = `ifdef SIM 32'd4096 `else `FES_APPLICATION_HPS_DDR_WINDOW_BYTES / 4 `endif
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

    fes_application_gp #(.ENABLE_GAMEPAD(1), .ENABLE_HPS_DDR(1)) endpoint (
        .clk(pixel_clk),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .menu_request(), .menu_opcode(), .menu_index(), .menu_argument(),
        .menu_response_valid(1'b0), .menu_response_error(1'b0),
        .menu_response_data(16'd0), .menu_quiesced(1'b1),
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
    wire [25:0] sdram_addr;
    wire [15:0] sdram_wdata, sdram_rdata;
    wire sdram_pass /* verilator public_flat_rd */;
    wire sdram_fail /* verilator public_flat_rd */;
    wire sdram_stopped /* verilator public_flat_rd */;
    wire [31:0] sdram_errors /* verilator public_flat_rd */;
    wire [2:0] sdram_phase;
    wire sdram_reading;
    wire [31:0] sdram_shown;
    wire [31:0] sdram_fault;
    wire [31:0] sdram_last;
    wire [15:0] sdram_was;
    wire [15:0] sdram_expect, sdram_got;
    wire scan_start, scan_write, scan_pass, scan_fail, scan_stopped;
    wire [25:0] scan_addr;
    wire [15:0] scan_wdata;
    wire [31:0] scan_errors;
    wire byte_start, byte_write;
    wire [25:0] byte_addr;
    wire [15:0] byte_wdata;
    wire [1:0] byte_enable;
    wire byte_pass /* verilator public_flat_rd */;
    wire byte_fail /* verilator public_flat_rd */;
    wire byte_stopped, byte_timeout;
    wire [7:0] byte_completed;
    wire [15:0] byte_checks;
    wire [25:0] byte_fault_addr;
    wire [2:0] byte_fault_step;
    wire [1:0] byte_fault_be;
    wire [15:0] byte_fault_payload, byte_fault_expect, byte_fault_got;
    wire [106:0] byte_status /* verilator public_flat_rd */ = {
        byte_pass, byte_fail, byte_stopped, byte_timeout, byte_completed, byte_checks,
        byte_fault_addr, byte_fault_step, byte_fault_be,
        byte_fault_payload, byte_fault_expect, byte_fault_got
    };
    assign sdram_start = byte_pass ? scan_start : byte_start;
    assign sdram_write = byte_pass ? scan_write : byte_write;
    assign sdram_addr = byte_pass ? scan_addr : byte_addr;
    assign sdram_wdata = byte_pass ? scan_wdata : byte_wdata;
    assign sdram_pass = byte_pass && scan_pass;
    assign sdram_fail = byte_fail || scan_fail;
    assign sdram_stopped = byte_stopped || scan_stopped;
    assign sdram_errors = byte_fail ? 32'd1 : scan_errors;
    wire [15:0] dq_out, dq_rise, dq_fall;
`ifndef RAM_OSS_HIGH_SPEED
`ifndef RAM_100_ONLY
    reg [15:0] dq_rise_q, dq_fall_q;
`endif
`endif
`ifdef RAM_OSS_HIGH_SPEED
`ifndef RAM_100_ONLY
    // The shifted 130 MHz capture edge is too close to the next controller
    // edge for a fabric-to-fabric transfer. Retiming on the following falling
    // edge gives the controller a full half cycle to receive the word.
    reg [15:0] dq_oss_hold;
`endif
`endif
    wire dq_oe;
    reg [1:0] stop_sync = 2'b00;
    reg [1:0] mem_reset_sync = 2'b00;
    // OSS and Quartus builds select either 100 or 130 MHz directly from the
    // PLL for a full-span hardware diagnostic. The behavioral simulation
    // uses the reference clock without a memory PLL.
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

    // Per-pattern counts stay on screen after the next rate re-inits the chip.
    // The copy waits one cycle so a mismatch on the final word is included.
    wire [191:0] sdram_patterns;
    wire [191:0] pat50, pat75, pat100;
    wire [2:0] pat_ok;
    always @(posedge mem_clk) begin
        stop_sync <= {stop_sync[0], stop_level};
        mem_reset_sync <= {mem_reset_sync[0], mailbox_reset | rate_reset};
    end
    // Follow the pattern scan. The combined status also rises when the
    // byte-lane preflight fails, before any pattern count exists.
    pattern_latch sdram_pattern_latch (
        .clk(mem_clk), .reset(mem_reset_sync[1]), .rate(rate),
        .pass(scan_pass), .fail(scan_fail), .counts(sdram_patterns),
        .pat50(pat50), .pat75(pat75), .pat100(pat100), .pat_ok(pat_ok)
    );

    sdram_byte_lane byte_test (
        .clk(mem_clk), .reset(mem_reset_sync[1]), .stop(stop_sync[1]),
        .start(byte_start), .write(byte_write), .addr(byte_addr), .wdata(byte_wdata),
        .byte_enable(byte_enable), .done(sdram_done && !byte_pass), .rdata(sdram_rdata),
        .pass(byte_pass), .fail(byte_fail), .stopped(byte_stopped), .timed_out(byte_timeout),
        .completed(byte_completed), .checks(byte_checks), .fault_addr(byte_fault_addr),
        .fault_step(byte_fault_step), .fault_be(byte_fault_be), .fault_payload(byte_fault_payload),
        .fault_expect(byte_fault_expect), .fault_got(byte_fault_got)
    );

    mem_channel #(.ADDR_W(26), .WORDS(SDRAM_WORDS), .BASE(32'd0)) sdram_test (
        .clk(mem_clk), .reset(mem_reset_sync[1] || !byte_pass), .stop(stop_sync[1]),
        .start(scan_start), .write(scan_write), .addr(scan_addr), .wdata(scan_wdata),
        .done(sdram_done), .rdata(sdram_rdata),
        .busy(), .pass(scan_pass), .fail(scan_fail), .stopped(scan_stopped),
        .phase(sdram_phase), .reading(sdram_reading), .shown_addr(sdram_shown),
        .fault_addr(sdram_fault), .last_addr(sdram_last), .fault_got(sdram_was),
        .errors(scan_errors), .pattern_errors(sdram_patterns),
        .shown_expect(sdram_expect), .shown_got(sdram_got)
    );

`ifdef RAM_OSS_HIGH_SPEED
`define RAM_SDRAM_IO_REGISTERS
`endif
`ifdef RAM_SDRAM_IO_REGISTERS
    // Native builds put the command and address registers in the pads
    // (constraints.qsf FAST_OUTPUT_REGISTER); see DeanoC/nextpnr#135. The
    // simulation also defines RAM_SDRAM_IO_REGISTERS to cover this timing.
    localparam SDRAM_IO_OUTPUT_REGISTERS = 1;
`else
    localparam SDRAM_IO_OUTPUT_REGISTERS = 0;
`endif
    sdram_addon_port #(.BYTE_MASK_ENABLED(1),
                       .IO_OUTPUT_REGISTERS(SDRAM_IO_OUTPUT_REGISTERS)) sdram (
        .clk(mem_clk),
        .clk_pin(sdram_pin_clk),
        .rate(rate),
        .reset(mem_reset_sync[1]),
        .start(sdram_start), .write(sdram_write), .addr(sdram_addr), .wdata(sdram_wdata),
        .write_byte_enable(byte_pass ? 2'b11 : byte_enable), .initialized(),
        .done(sdram_done), .rdata(sdram_rdata),
        .sdram_clk(SDRAM_CLK), .sdram_cke(SDRAM_CKE),
        .sdram_ncs(SDRAM_nCS), .sdram_nras(SDRAM_nRAS),
        .sdram_ncas(SDRAM_nCAS), .sdram_nwe(SDRAM_nWE),
        .sdram_dqml(SDRAM_DQML), .sdram_dqmh(SDRAM_DQMH),
        .sdram_ba(SDRAM_BA), .sdram_a(SDRAM_A),
        .dq_out(dq_out), .dq_oe(dq_oe),
`ifndef RAM_OSS_HIGH_SPEED
`ifndef RAM_100_ONLY
        .dq_rise(dq_rise_q), .dq_fall(dq_fall_q)
`else
        .dq_rise(dq_rise), .dq_fall(dq_fall)
`endif
`else
`ifndef RAM_100_ONLY
        .dq_rise(dq_oss_hold), .dq_fall(dq_oss_hold)
`else
        .dq_rise(dq_rise), .dq_fall(dq_fall)
`endif
`endif
    );

`ifdef RAM_OSS_HIGH_SPEED
`ifndef RAM_100_ONLY
    always @(negedge mem_clk)
        dq_oss_hold <= dq_rise;
`endif
`endif

`ifndef RAM_OSS_HIGH_SPEED
`ifndef RAM_100_ONLY
    // A fabric register next to the input DDIO cells removes the long
    // 130 MHz route from the pins through rate selection into rdata.
    always @(posedge mem_clk) begin
        dq_rise_q <= dq_rise;
        dq_fall_q <= dq_fall;
    end
`endif
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
`ifdef RAM_OSS_HIGH_SPEED
            // The OSS packer cannot attach DDR input capture to a
            // bidirectional pad. Shift the PLL by the former falling-edge
            // offset and sample the pad on the rising fabric clock instead.
            reg dq_sample;
            always @(posedge cap_clk)
                dq_sample <= dq_pin;
            assign dq_rise[dq_bit] = dq_sample;
            assign dq_fall[dq_bit] = dq_sample;
`else
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
`endif
        end
    endgenerate

    // HPS DDR: the three fes.memory.hps-ddr ports scan the window together
    // on the memory clock. Execution hold and a memory PLL change hold them.
    wire ddr0_reset, ddr1_reset, ddr2_reset;
    wire [27:0] ddr0_address;
    wire [28:0] ddr1_address, ddr2_address;
    wire [7:0] ddr0_burst, ddr1_burst, ddr2_burst;
    wire ddr0_wait, ddr1_wait, ddr2_wait;
    wire [127:0] ddr0_rdata, ddr0_wdata;
    wire [63:0] ddr1_rdata, ddr1_wdata, ddr2_rdata, ddr2_wdata;
    wire ddr0_rvalid, ddr1_rvalid, ddr2_rvalid;
    wire ddr0_read, ddr1_read, ddr2_read;
    wire ddr0_write, ddr1_write, ddr2_write;
    wire [15:0] ddr0_be;
    wire [7:0] ddr1_be, ddr2_be;
    wire [297:0] ddr0_status, ddr1_status, ddr2_status;
    wire ddr0_done /* verilator public_flat_rd */;
    wire ddr1_done /* verilator public_flat_rd */;
    wire ddr2_done /* verilator public_flat_rd */;
    wire ddr0_nack /* verilator public_flat_rd */;
    wire ddr1_nack /* verilator public_flat_rd */;
    wire ddr2_nack /* verilator public_flat_rd */;
    wire ddr0_stopped /* verilator public_flat_rd */;
    wire ddr1_stopped /* verilator public_flat_rd */;
    wire ddr2_stopped /* verilator public_flat_rd */;
    wire [31:0] ddr0_errors /* verilator public_flat_rd */;
    wire [31:0] ddr1_errors /* verilator public_flat_rd */;
    wire [31:0] ddr2_errors /* verilator public_flat_rd */;
`ifdef RAM_130_ONLY
    localparam integer DDR_MHZ = 130;
`elsif RAM_RATE_SWEEP
    localparam integer DDR_MHZ = 100;
`else
    localparam integer DDR_MHZ = 50;
`endif
    localparam [31:0] DDR_BASE = `FES_APPLICATION_HPS_DDR_WINDOW_BASE;
    localparam [31:0] DDR1_BASE = DDR_BASE + (`FES_APPLICATION_HPS_DDR_WINDOW_BYTES / 2);
    localparam [31:0] DDR2_BASE = DDR1_BASE + (`FES_APPLICATION_HPS_DDR_WINDOW_BYTES / 4);

    fes_hps_ddr hps_ddr (
        .hold(mailbox_reset | rate_reset),
        .p0_drained(), .p0_clk(mem_clk), .p0_reset(ddr0_reset),
        .p0_address(ddr0_address), .p0_burstcount(ddr0_burst),
        .p0_waitrequest(ddr0_wait), .p0_readdata(ddr0_rdata),
        .p0_readdatavalid(ddr0_rvalid), .p0_read(ddr0_read),
        .p0_writedata(ddr0_wdata), .p0_byteenable(ddr0_be), .p0_write(ddr0_write),
        .p1_drained(), .p1_clk(mem_clk), .p1_reset(ddr1_reset),
        .p1_address(ddr1_address), .p1_burstcount(ddr1_burst),
        .p1_waitrequest(ddr1_wait), .p1_readdata(ddr1_rdata),
        .p1_readdatavalid(ddr1_rvalid), .p1_read(ddr1_read),
        .p1_writedata(ddr1_wdata), .p1_byteenable(ddr1_be), .p1_write(ddr1_write),
        .p2_drained(), .p2_clk(mem_clk), .p2_reset(ddr2_reset),
        .p2_address(ddr2_address), .p2_burstcount(ddr2_burst),
        .p2_waitrequest(ddr2_wait), .p2_readdata(ddr2_rdata),
        .p2_readdatavalid(ddr2_rvalid), .p2_read(ddr2_read),
        .p2_writedata(ddr2_wdata), .p2_byteenable(ddr2_be), .p2_write(ddr2_write)
    );

    wire [191:0] ddr_cycles;
    wire [95:0] ddr_rate_digits /* verilator public_flat_rd */;
    ddr_channel #(.DATA_W(128), .ADDR_W(28), .BASE(DDR_BASE),
                  .BYTES(DDR_WIDE_BYTES)) ddr0_test (
        .clk(mem_clk), .reset(ddr0_reset), .stop(stop_sync[1]),
        .address(ddr0_address), .burstcount(ddr0_burst), .waitrequest(ddr0_wait),
        .readdata(ddr0_rdata), .readdatavalid(ddr0_rvalid), .read(ddr0_read),
        .writedata(ddr0_wdata), .byteenable(ddr0_be), .write(ddr0_write),
        .phase(ddr0_status[297:295]), .reading(ddr0_status[294]),
        .shown_addr(ddr0_status[293:262]), .errors(ddr0_errors),
        .fault_addr(ddr0_status[229:198]), .last_addr(ddr0_status[197:166]),
        .fault_phase(ddr0_status[165:163]), .bad(ddr0_status[162:35]),
        .done(ddr0_done), .nack(ddr0_nack), .stopped(ddr0_stopped),
        .write_cycles(ddr_cycles[31:0]), .read_cycles(ddr_cycles[63:32])
    );
    ddr_channel #(.DATA_W(64), .ADDR_W(29), .BASE(DDR1_BASE),
                  .BYTES(DDR_NARROW_BYTES)) ddr1_test (
        .clk(mem_clk), .reset(ddr1_reset), .stop(stop_sync[1]),
        .address(ddr1_address), .burstcount(ddr1_burst), .waitrequest(ddr1_wait),
        .readdata(ddr1_rdata), .readdatavalid(ddr1_rvalid), .read(ddr1_read),
        .writedata(ddr1_wdata), .byteenable(ddr1_be), .write(ddr1_write),
        .phase(ddr1_status[297:295]), .reading(ddr1_status[294]),
        .shown_addr(ddr1_status[293:262]), .errors(ddr1_errors),
        .fault_addr(ddr1_status[229:198]), .last_addr(ddr1_status[197:166]),
        .fault_phase(ddr1_status[165:163]), .bad(ddr1_status[98:35]),
        .done(ddr1_done), .nack(ddr1_nack), .stopped(ddr1_stopped),
        .write_cycles(ddr_cycles[95:64]), .read_cycles(ddr_cycles[127:96])
    );
    ddr_channel #(.DATA_W(64), .ADDR_W(29), .BASE(DDR2_BASE),
                  .BYTES(DDR_NARROW_BYTES)) ddr2_test (
        .clk(mem_clk), .reset(ddr2_reset), .stop(stop_sync[1]),
        .address(ddr2_address), .burstcount(ddr2_burst), .waitrequest(ddr2_wait),
        .readdata(ddr2_rdata), .readdatavalid(ddr2_rvalid), .read(ddr2_read),
        .writedata(ddr2_wdata), .byteenable(ddr2_be), .write(ddr2_write),
        .phase(ddr2_status[297:295]), .reading(ddr2_status[294]),
        .shown_addr(ddr2_status[293:262]), .errors(ddr2_errors),
        .fault_addr(ddr2_status[229:198]), .last_addr(ddr2_status[197:166]),
        .fault_phase(ddr2_status[165:163]), .bad(ddr2_status[98:35]),
        .done(ddr2_done), .nack(ddr2_nack), .stopped(ddr2_stopped),
        .write_cycles(ddr_cycles[159:128]), .read_cycles(ddr_cycles[191:160])
    );
    ddr_rates #(.WIDE_NUMERATOR({8'd0, DDR_WIDE_BYTES} * DDR_MHZ),
                .NARROW_NUMERATOR({8'd0, DDR_NARROW_BYTES} * DDR_MHZ)) ddr_speed (
        .clk(mem_clk), .cycles(ddr_cycles), .digits(ddr_rate_digits)
    );
    assign ddr0_status[31:0] = {ddr_rate_digits[15:0], ddr_rate_digits[31:16]};
    assign ddr1_status[31:0] = {ddr_rate_digits[47:32], ddr_rate_digits[63:48]};
    assign ddr2_status[31:0] = {ddr_rate_digits[79:64], ddr_rate_digits[95:80]};
    assign ddr0_status[261:230] = ddr0_errors;
    assign ddr1_status[261:230] = ddr1_errors;
    assign ddr2_status[261:230] = ddr2_errors;
    assign ddr0_status[34:32] = {ddr0_done, ddr0_nack, ddr0_stopped};
    assign ddr1_status[34:32] = {ddr1_done, ddr1_nack, ddr1_stopped};
    assign ddr2_status[34:32] = {ddr2_done, ddr2_nack, ddr2_stopped};
    assign ddr1_status[162:99] = 64'd0;
    assign ddr2_status[162:99] = 64'd0;

    ram_display display (
        .byte_status(byte_status),
        .pixel_clk(pixel_clk),
        .x(playfield_x), .y(playfield_y), .active(playfield_active),
        .sdram_phase(sdram_phase), .sdram_reading(sdram_reading), .sdram_addr(sdram_shown),
        .sdram_fault(sdram_fault), .sdram_last(sdram_last), .sdram_was(sdram_was),
        .sdram_errors(sdram_errors), .sdram_expect(sdram_expect), .sdram_got(sdram_got),
        .sdram_mhz(sdram_mhz),
        .sdram_pass(sdram_pass), .sdram_fail(sdram_fail), .sdram_stopped(sdram_stopped),
        .pat50(pat50), .pat75(pat75), .pat100(pat100), .pat_ok(pat_ok),
        .ddr0(ddr0_status), .ddr1(ddr1_status), .ddr2(ddr2_status),
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
