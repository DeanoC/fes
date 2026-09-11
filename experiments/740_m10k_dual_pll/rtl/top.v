// Two PLLs plus one independent-clock 512x20 M10K. Default router2 retries
// with router1 when a constrained clock has less than 10% margin.
module top #(
    parameter [15:0] SIGNATURE = 16'hD42A
) (
    input wire FPGA_CLK1_50
);
    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire pll_clock;
    wire pixel_clock;
    wire read_clock;
    wire locked_read;
    wire locked_pixel;

    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(1),
        .output_clock_frequency0("25.0 MHz"),
        .phase_shift0("0 ps"),
        .duty_cycle0(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("false")
    ) pll_read (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk(pll_clock),
        .locked(locked_read)
    );

    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(1),
        .output_clock_frequency0("74.25 MHz"),
        .phase_shift0("0 ps"),
        .duty_cycle0(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("true")
    ) pll_pixel (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk(pixel_clock),
        .locked(locked_pixel)
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

    (* keep *)
    reg [7:0] pixel_beat = 8'd0;
    always @(posedge pixel_clock)
        pixel_beat <= pixel_beat + 1'b1;

    (* ramstyle = "M10K" *) reg [19:0] mem [0:511];
    reg [19:0] q = 20'd0;
    integer i;

    initial begin
        for (i = 0; i < 512; i = i + 1)
            mem[i] = (i[19:0] * 20'd73) ^ (i[19:0] >> 1) ^ 20'h00A6;
    end

    reg [8:0] waddr = 9'd0;
    reg [19:0] wdata = 20'd0;
    reg we = 1'b0;
    reg armed = 1'b0;

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= gp_out[8:0];
        wdata <= gp_out[28:9];
        we <= armed && gp_out[31];
        if (we)
            mem[waddr] <= wdata;
    end

    always @(posedge read_clock) begin
        if (gp_out[30])
            q <= mem[gp_out[8:0]];
    end

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
