// Mixed-width M10K SDP: 1024x10 write, 256x40 read. 50 MHz write, gated 25 MHz read.
module top #(
    parameter [15:0] SIGNATURE = 16'hD41C
) (
    input wire FPGA_CLK1_50
);
    localparam integer UNIT = 10;
    localparam integer WLANES = 1;
    localparam integer RLANES = 4;
    localparam integer WA = 10;
    localparam integer RA = 8;

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
        .ena(gp_out[29]),
        .outclk(read_clock),
        .enaout()
    );

    (* ram_style = "m10k_mixed" *) reg [UNIT-1:0] mem [0:1023];
    integer i;

    initial begin
        for (i = 0; i < 1024; i = i + 1)
            mem[i] = (i[9:0] * 10'd73) ^ (i[9:0] >> 1) ^ 10'h0A6;
    end

    reg armed = 1'b0;
    reg we = 1'b0;
    reg [WA-1:0] wa = {WA{1'b0}};
    reg [UNIT-1:0] seed = {UNIT{1'b0}};
    reg [UNIT*RLANES-1:0] q = {(UNIT * RLANES) {1'b0}};

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        we <= armed && gp_out[31];
        wa <= gp_out[WA-1:0];
        seed <= gp_out[16+:UNIT];
    end

    wire [RA-1:0] ra = gp_out[RA-1:0];
    genvar lane;
    generate
        for (lane = 0; lane < WLANES; lane = lane + 1) begin: write_lane
            always @(posedge FPGA_CLK1_50)
                if (we)
                    mem[wa] <= seed;
        end
        for (lane = 0; lane < RLANES; lane = lane + 1) begin: read_lane
            localparam [$clog2(RLANES)-1:0] INDEX = lane;
            always @(posedge read_clock)
                if (gp_out[30])
                    q[lane*UNIT+:UNIT] <= mem[{ra, INDEX}];
        end
    endgenerate

    wire [63:0] windowed = {24'd0, q} >> {gp_out[27:26], 4'b0};
    reg [15:0] sampled = 16'd0;
    always @(posedge FPGA_CLK1_50)
        sampled <= windowed[15:0];

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
