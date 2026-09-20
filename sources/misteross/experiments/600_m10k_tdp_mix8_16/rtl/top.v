// Independent-clock mixed-width true dual-port M10K table on HPS GP.
// GPO addr[9:0], clock-B gate[14], seed[25:16], select B[26],
// window[27], enable A/B[28/29], write A/B[30/31].
module top #(
    parameter [15:0] SIGNATURE = 16'hD424
) (
    input wire FPGA_CLK1_50
);
    localparam integer UNIT = 8;
    localparam integer ALANES = 1;
    localparam integer BLANES = 2;
    localparam integer AA = 10 - $clog2(ALANES);
    localparam integer BA = 10 - $clog2(BLANES);

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
    /* verilator lint_off WIDTHTRUNC */
    (* ram_style = "m10k_tdp_mixed" *) reg [UNIT-1:0] mem [0:1023];
    reg [UNIT*ALANES-1:0] q_a = {(UNIT * ALANES) {1'b0}};
    reg [UNIT*BLANES-1:0] q_b = {(UNIT * BLANES) {1'b0}};
    integer i;

    initial begin
        for (i = 0; i < 1024; i = i + 1)
            mem[i] = ((i[9:0] * 10'd73) ^ (i[9:0] >> 1) ^ 10'h0A6);
    end

    reg armed = 1'b0;
    reg [AA-1:0] addr_a = {AA{1'b0}};
    reg [BA-1:0] addr_b = {BA{1'b0}};
    reg [UNIT-1:0] seed_a = {UNIT{1'b0}};
    reg [UNIT-1:0] seed_b = {UNIT{1'b0}};

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        if (!gp_out[26]) begin
            addr_a <= gp_out[AA-1:0];
            seed_a <= gp_out[16+:UNIT];
        end else begin
            addr_b <= gp_out[BA-1:0];
            seed_b <= gp_out[16+:UNIT];
        end
    end

    wire we_a = armed && gp_out[30];
    wire we_b = armed && gp_out[31];
    wire [UNIT*ALANES-1:0] data_a;
    wire [UNIT*BLANES-1:0] data_b;
    genvar lane;
    generate
        for (lane = 0; lane < ALANES; lane = lane + 1) begin
            assign data_a[lane*UNIT+:UNIT] = seed_a ^ (lane * 'h93);
        end
        for (lane = 0; lane < BLANES; lane = lane + 1) begin
            assign data_b[lane*UNIT+:UNIT] = seed_b ^ ((lane + 2) * 'h93);
        end
        for (lane = 0; lane < ALANES; lane = lane + 1) begin: a_lane
            if (ALANES == 1) begin
                always @(posedge FPGA_CLK1_50)
                    if (gp_out[28]) begin
                        if (we_a) begin
                            mem[addr_a] <= data_a;
                            q_a <= data_a;
                        end else
                            q_a <= mem[addr_a];
                    end
            end else begin
                localparam [$clog2(ALANES)-1:0] INDEX = lane;
                always @(posedge FPGA_CLK1_50)
                    if (gp_out[28]) begin
                        if (we_a) begin
                            mem[{addr_a, INDEX}] <= data_a[lane*UNIT+:UNIT];
                            q_a[lane*UNIT+:UNIT] <= data_a[lane*UNIT+:UNIT];
                        end else
                            q_a[lane*UNIT+:UNIT] <= mem[{addr_a, INDEX}];
                    end
            end
        end
        for (lane = 0; lane < BLANES; lane = lane + 1) begin: b_lane
            if (BLANES == 1) begin
                always @(posedge read_clock)
                    if (gp_out[29]) begin
                        if (we_b) begin
                            mem[addr_b] <= data_b;
                            q_b <= data_b;
                        end else
                            q_b <= mem[addr_b];
                    end
            end else begin
                localparam [$clog2(BLANES)-1:0] INDEX = lane;
                always @(posedge read_clock)
                    if (gp_out[29]) begin
                        if (we_b) begin
                            mem[{addr_b, INDEX}] <= data_b[lane*UNIT+:UNIT];
                            q_b[lane*UNIT+:UNIT] <= data_b[lane*UNIT+:UNIT];
                        end else
                            q_b[lane*UNIT+:UNIT] <= mem[{addr_b, INDEX}];
                    end
            end
        end
    endgenerate
    /* verilator lint_on WIDTHTRUNC */
    /* verilator lint_on MULTIDRIVEN */

    wire [19:0] q_a_ext;
    wire [19:0] q_b_ext;
    generate
        if (UNIT * ALANES == 20)
            assign q_a_ext = q_a;
        else
            assign q_a_ext = {{(20 - UNIT * ALANES){1'b0}}, q_a};
        if (UNIT * BLANES == 20)
            assign q_b_ext = q_b;
        else
            assign q_b_ext = {{(20 - UNIT * BLANES){1'b0}}, q_b};
    endgenerate
    wire [19:0] selected_q = gp_out[26] ? q_b_ext : q_a_ext;
    wire [31:0] windowed = {12'd0, selected_q} >> {gp_out[27], 4'b0};
    reg [15:0] sampled = 16'd0;
    always @(posedge FPGA_CLK1_50)
        sampled <= windowed[15:0];

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
