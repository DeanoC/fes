// Independent-clock M10K SDP: 50 MHz write, gated 25 MHz read, 256x40.
module top #(
    parameter integer WIDTH = 40,
    parameter [15:0] SIGNATURE = 16'hD419
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
        .ena(gp_out[29]),
        .outclk(read_clock),
        .enaout()
    );

    (* ramstyle = "M10K" *) reg [39:0] mem [0:255];
    reg [39:0] q = 40'd0;
    integer i;

    function [19:0] initial_word;
        input integer address;
        begin
            initial_word = (address[19:0] * 20'd73) ^ (address[19:0] >> 1) ^ 20'h00A6;
        end
    endfunction

    initial begin
        for (i = 0; i < 256; i = i + 1)
            mem[i] = {~initial_word(i), initial_word(i)};
    end

    reg [7:0] waddr = 8'd0;
    reg [19:0] wdata = 20'd0;
    reg we = 1'b0;
    // kit.py's post-program SPI probe also drives GP_OUT. Require an explicit
    // command before accepting writes so it cannot corrupt initialized memory.
    reg armed = 1'b0;

    always @(posedge FPGA_CLK1_50) begin
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        waddr <= gp_out[7:0];
        wdata <= gp_out[28:9];
        we <= armed && gp_out[31];
        if (we)
            mem[waddr] <= {~wdata, wdata};
    end

    always @(posedge read_clock) begin
        if (gp_out[30])
            q <= mem[gp_out[7:0]];
    end

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    wire [63:0] windowed = {24'd0, q} >> {gp_out[28:27], 4'b0};
    reg [15:0] sampled = 16'd0;
    always @(posedge FPGA_CLK1_50)
        sampled <= windowed[15:0];

    assign gp_in = {SIGNATURE, sampled};
endmodule
