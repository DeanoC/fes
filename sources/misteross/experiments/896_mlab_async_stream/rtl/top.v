// One 256x40 flow-through MLAB array sampled every 74.25 MHz pixel cycle.
// The write phase and read phase do not overlap, so read-during-write mode
// cannot explain a reported mismatch.
module top(input wire FPGA_CLK1_50);
    wire pixel_clock;
    wire locked;
    altera_pll #(
        .reference_clock_frequency("50.0 MHz"),
        .number_of_clocks(1),
        .output_clock_frequency0("74.25 MHz"),
        .phase_shift0("0 ps"),
        .duty_cycle0(50),
        .operation_mode("direct"),
        .fractional_vco_multiplier("true")
    ) pll (
        .refclk(FPGA_CLK1_50), .rst(1'b0),
        .outclk(pixel_clock), .locked(locked)
    );
    wire [31:0] gp_out;
    wire scan_clock;
    cyclonev_clkena #(
        .clock_type("global clock"),
        .ena_register_mode("falling edge"),
        .ena_register_power_up("high"),
        .disable_mode("low"),
        .test_syn("high")
    ) gate (
        .inclk(pixel_clock), .ena(phase != 5 || gp_out[31]),
        .outclk(scan_clock), .enaout()
    );

    function [39:0] pattern(input [7:0] address);
        begin
            pattern = {8'ha5 ^ address, 8'h3c + address,
                       address ^ 8'h5a, 8'hc3 - address, address};
        end
    endfunction

    (* ramstyle = "MLAB" *) reg [39:0] memory [0:255];
    reg [2:0] phase = 0;
    reg [7:0] fill_address = 0;
    reg [7:0] scan_address = 0;
    reg [39:0] expected = 0;
    reg [15:0] samples = 0;
    reg [13:0] errors = 0;
    reg [15:0] slow_samples = 0;
    reg [13:0] slow_errors = 0;
    reg hold_address = 0;
    reg [7:0] first_address = 0;
    reg [39:0] first_expected = 0;
    reg [39:0] first_observed = 0;
    wire [7:0] read_address = phase == 5 ? gp_out[15:8] : scan_address + 8'd1;
    wire [39:0] observed = memory[read_address];

    always @(posedge scan_clock) begin
        if (locked) begin
            case (phase)
                0: begin
                    memory[fill_address] <= pattern(fill_address);
                    fill_address <= fill_address + 8'd1;
                    if (fill_address == 8'hff)
                        phase <= 1;
                end
                1: begin
                    expected <= pattern(8'd1);
                    phase <= 2;
                end
                2: begin
                    if (observed != expected) begin
                        if (errors == 0) begin
                            first_address <= read_address;
                            first_expected <= expected;
                            first_observed <= observed;
                        end
                        if (errors != 14'h3fff)
                            errors <= errors + 14'd1;
                    end
                    scan_address <= scan_address + 8'd1;
                    expected <= pattern(scan_address + 8'd2);
                    samples <= samples + 16'd1;
                    if (samples == 16'hffff)
                        phase <= 3;
                end
                3: begin
                    scan_address <= 0;
                    expected <= pattern(8'd1);
                    phase <= 4;
                end
                4: begin
                    hold_address <= !hold_address;
                    if (hold_address) begin
                        if (observed != expected && slow_errors != 14'h3fff)
                            slow_errors <= slow_errors + 14'd1;
                        scan_address <= scan_address + 8'd1;
                        expected <= pattern(scan_address + 8'd2);
                        slow_samples <= slow_samples + 16'd1;
                        if (slow_samples == 16'hffff)
                            phase <= 5;
                    end
                end
                default: begin end
            endcase
        end
    end

    reg [15:0] page;
    always @* begin
        case (gp_out[2:0])
            3'd0: page = {phase == 5, locked,
                          gp_out[3] ? slow_errors : errors};
            3'd1: page = gp_out[3] ? observed[15:0] : {8'd0, first_address};
            3'd2: page = gp_out[3] ? observed[31:16] : first_expected[15:0];
            3'd3: page = gp_out[3] ? {8'd0, observed[39:32]} : first_observed[15:0];
            3'd4: page = first_expected[31:16];
            3'd5: page = first_observed[31:16];
            3'd6: page = {8'd0, first_expected[39:32]};
            default: page = {8'd0, first_observed[39:32]};
        endcase
    end
    wire [31:0] gp_in = {16'hd895, page};
    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in), .gp_out(gp_out)
    );
endmodule
