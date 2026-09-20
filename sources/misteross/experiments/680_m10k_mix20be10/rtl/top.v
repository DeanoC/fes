// Mixed-width M10K SDP: 512x20 byte-masked writes, 1024x10 reads.
module top #(
    parameter [15:0] SIGNATURE = 16'hD425
) (
    input wire FPGA_CLK1_50
);
    function automatic [10239:0] packed_init;
        integer address;
        begin
            packed_init = 10240'b0;
            for (address = 0; address < 1024; address = address + 1)
                packed_init[address * 10 +: 10] = (address[9:0] * 10'd73) ^ (address[9:0] >> 1) ^ 10'h0A6;
        end
    endfunction

    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire pll_clock;
    wire read_clock;
    wire locked;
    wire [9:0] q;

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

    reg armed = 1'b0;
    reg we = 1'b0;
    reg [8:0] waddr = 9'd0;
    reg [19:0] wdata = 20'd0;
    reg [1:0] wbe = 2'b00;

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= {gp_out[8:6], 2'b00, gp_out[3:0]};
        wdata <= gp_out[28:9];
        wbe <= gp_out[5:4];
        we <= armed && gp_out[31];
    end

    (* keep *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1),
        .INIT(packed_init())
    ) mem (
        .CLK1(FPGA_CLK1_50),
        .CLK2(read_clock),
        .A1ADDR(waddr),
        .A1DATA(wdata),
        .A1EN(we),
        .A1BE(wbe),
        .B1ADDR(gp_out[9:0]),
        .B1DATA(q),
        .B1EN(gp_out[30])
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    reg [15:0] sampled = 16'd0;
    always @(posedge FPGA_CLK1_50)
        sampled <= {6'b0, q};

    assign gp_in = {SIGNATURE, sampled};
endmodule
