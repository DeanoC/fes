module top #(
    parameter integer ADDR_BITS = 8
) (
    input wire FPGA_CLK1_50,
    output wire [0:0] LED
);
    localparam integer DEPTH = 1 << ADDR_BITS;
    localparam integer WIDTH = 8;
    localparam [WIDTH-1:0] PATTERN = 8'hA5;

    reg [ADDR_BITS-1:0] addr = {ADDR_BITS{1'b0}};
    reg [WIDTH-1:0] data = {WIDTH{1'b0}};
    reg [WIDTH-1:0] stored [0:DEPTH-1];

    integer index;
    initial begin
        for (index = 0; index < DEPTH; index = index + 1) begin
            stored[index] = index[WIDTH-1:0] ^ PATTERN;
        end
    end

    always @(posedge FPGA_CLK1_50) begin
        addr <= addr + 1'b1;
        data <= stored[addr];
    end

    assign LED[0] = data[0];

endmodule
