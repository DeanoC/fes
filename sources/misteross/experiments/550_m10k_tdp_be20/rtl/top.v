// Independent-clock byte-masked true dual-port 512-by-20 table on HPS GP.
// GPO addr[8:0], masks A[11:10]/B[13:12], clock-B gate[14], seed[25:16],
// select B[26], window[27], enable A/B[28/29], write A/B[30/31].
module top #(
    parameter [15:0] SIGNATURE = 16'hD41F
) (
    input wire FPGA_CLK1_50
);
    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire pll_clock;
    wire read_clock;
    wire locked;

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
        .ena(gp_out[14]),
        .outclk(read_clock),
        .enaout()
    );

    /* verilator lint_off MULTIDRIVEN */
    (* ram_style = "m10k_tdp_byte" *) reg [19:0] mem [0:511];
    reg [19:0] q_a = 20'd0;
    reg [19:0] q_b = 20'd0;
    integer i;

    initial begin
        for (i = 0; i < 512; i = i + 1)
            mem[i] = (i[19:0] * 20'd73) ^ (i[19:0] >> 1) ^ 20'h00A6;
    end

    reg armed = 1'b0;
    reg [8:0] addr_a = 9'd0;
    reg [8:0] addr_b = 9'd0;
    reg [9:0] seed_a = 10'd0;
    reg [9:0] seed_b = 10'd0;

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        if (!gp_out[26]) begin
            addr_a <= gp_out[8:0];
            seed_a <= gp_out[25:16];
        end else begin
            addr_b <= gp_out[8:0];
            seed_b <= gp_out[25:16];
        end
    end

    wire [19:0] data_a = {10'd0, seed_a} ^ 20'h93a00;
    wire [19:0] data_b = {10'd0, seed_b} ^ 20'h2bc00;
    wire we_a = armed && gp_out[30];
    wire we_b = armed && gp_out[31];

    always @(posedge FPGA_CLK1_50)
        if (gp_out[28]) begin
            if (we_a) begin
                if (gp_out[10])
                    mem[addr_a][9:0] <= data_a[9:0];
                if (gp_out[11])
                    mem[addr_a][19:10] <= data_a[19:10];
            end else
                q_a <= mem[addr_a];
        end

    always @(posedge read_clock)
        if (gp_out[29]) begin
            if (we_b) begin
                if (gp_out[12])
                    mem[addr_b][9:0] <= data_b[9:0];
                if (gp_out[13])
                    mem[addr_b][19:10] <= data_b[19:10];
            end else
                q_b <= mem[addr_b];
        end
    /* verilator lint_on MULTIDRIVEN */

    wire [19:0] selected_q = gp_out[26] ? q_b : q_a;
    wire [63:0] windowed = {44'd0, selected_q} >> {gp_out[27], 4'b0};
    reg [15:0] sampled = 16'd0;
    always @(posedge FPGA_CLK1_50)
        sampled <= windowed[15:0];

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
