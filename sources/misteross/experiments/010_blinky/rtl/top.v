module top #(
    parameter integer COUNTER_BITS = 25
) (
    input wire FPGA_CLK1_50,
    output wire [0:0] LED
);

    reg [COUNTER_BITS-1:0] counter = {COUNTER_BITS{1'b0}};

    always @(posedge FPGA_CLK1_50) begin
        counter <= counter + 1'b1;
    end

    assign LED[0] = counter[COUNTER_BITS-1];

endmodule
