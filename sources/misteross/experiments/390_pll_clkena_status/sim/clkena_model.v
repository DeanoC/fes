// Simulation only: falling-edge clock enable, power-up low, disabled low.
module cyclonev_clkena #(
    parameter clock_type = "global clock",
    parameter ena_register_mode = "falling edge",
    parameter ena_register_power_up = "low",
    parameter disable_mode = "low",
    parameter test_syn = "high"
) (
    input wire inclk,
    input wire ena,
    output wire outclk,
    output wire enaout
);
    reg enable = 1'b0;
    always @(negedge inclk)
        enable <= ena;
    assign outclk = enable ? inclk : 1'b0;
    assign enaout = enable;
endmodule
