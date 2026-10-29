module constant_probe (
 input wire clk,
 (* preserve, dont_merge, noprune *) output reg [19:0] bus_response
);
 always @(posedge clk) bus_response <= 20'd0;
endmodule
