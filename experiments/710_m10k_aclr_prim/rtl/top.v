// Independent-clock 512x20 M10K. Yosys now exposes ACLR0/ACLR1 on the
// primitive, so ACLR1 is instantiated from gp_out[5] rather than patched in.
module top #(
    parameter [15:0] SIGNATURE = 16'hD427
) (
    input wire FPGA_CLK1_50
);
    function automatic [10239:0] packed_init;
        integer address;
        begin
            packed_init = 10240'b0;
            for (address = 0; address < 512; address = address + 1)
                packed_init[address * 20 +: 20] = (address[19:0] * 20'd73) ^ (address[19:0] >> 1) ^ 20'h00A6;
        end
    endfunction

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire pll_clock;
    wire read_clock;
    wire locked;
    wire [19:0] q;
    wire [8:0] table_addr;

    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(1),
        .output_clock_frequency0("25.0 MHz"),
        .phase_shift0("0 ps"),
        .duty_cycle0(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) pll (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk(pll_clock),
        .locked(locked)
    );

    cyclonev_clkena #(
        .clock_type("global clock"),
        .ena_register_mode("falling edge"),
        .ena_register_power_up("high"),
        .disable_mode("low"),
        .test_syn("high")
    ) gate (
        .inclk(pll_clock),
        .ena(gp_out[29]),
        .outclk(read_clock),
        .enaout()
    );

    // kit.py's post-program SPI probe also drives GP_OUT. Require an explicit
    // command before accepting writes so it cannot corrupt initialized memory.
    // gp_out[5] is ACLR1; it is forced out of the table address.
    reg armed = 1'b0;
    reg we = 1'b0;
    reg [8:0] waddr = 9'd0;
    reg [19:0] wdata = 20'd0;
    assign table_addr = {gp_out[8:6], 2'b00, gp_out[3:0]};

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= table_addr;
        wdata <= gp_out[28:9];
        we <= armed && gp_out[31];
    end

    (* keep *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1),
        .INIT(packed_init())
    ) mem (
        .CLK1(FPGA_CLK1_50),
        .CLK2(read_clock),
        .A1ADDR(waddr),
        .A1DATA(wdata),
        .A1EN(we),
        .A1BE(2'b11),
        .B1ADDR(table_addr),
        .B1DATA(q),
        .B1EN(gp_out[30]),
        .ACLR0(1'b0),
        .ACLR1(gp_out[5])
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    wire [63:0] windowed = {44'd0, q} >> {gp_out[28:27], 4'b0};
    reg [15:0] sampled = 16'd0;
    always @(posedge FPGA_CLK1_50)
        sampled <= windowed[15:0];

    assign gp_in = {SIGNATURE, sampled};
endmodule
