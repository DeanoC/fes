// Simulation only: two-stage falling-edge clock enable, power-up low.
module cyclonev_clkena #(
    parameter clock_type = "global clock",
    parameter ena_register_mode = "double register",
    parameter ena_register_power_up = "low",
    parameter disable_mode = "low",
    parameter test_syn = "high"
) (
    input wire inclk,
    input wire ena,
    output wire outclk,
    output wire enaout
);
    reg enable_meta = 1'b0;
    reg enable = 1'b0;
    always @(negedge inclk) begin
        enable_meta <= ena;
        enable <= enable_meta;
    end
    assign outclk = enable ? inclk : 1'b0;
    assign enaout = enable;
endmodule
